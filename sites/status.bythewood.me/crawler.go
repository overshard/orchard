package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// The SEO spider.
//
// Breadth-first from the property URL, staying on the host, up to PageCap
// pages at Concurrency at a time, bounded by CrawlDeadline. Everything it
// collects is handed to the checks in crawler_checks.go, which turn it into
// the flat insight list the dashboard groups and renders.

// Page is one crawled URL plus, when it was HTML, everything parsed out of it.
type Page struct {
	URL          string
	RequestedURL string
	Status       int
	ContentType  string
	ElapsedMS    int64
	Bytes        int
	Headers      map[string]string
	RedirectHops int
	Err          string
	IsHTML       bool
	HTML         *ParsedHTML
}

// CrawlResult is everything one crawl learned, before any of it is judged.
type CrawlResult struct {
	StartURL           string
	Host               string
	Pages              []*Page
	ExternalLinkStatus map[string]int
	SitemapURLs        []string
	Robots             RobotsCtx
	// Compression is the server's Content-Encoding for the start URL, or ""
	// when it answered uncompressed.
	Compression string
}

// normalizeURL is the key pages are deduplicated by.
//
// The fragment goes because "/a" and "/a#top" are one page, and the trailing
// slash goes because "/a" and "/a/" almost always are too. That second one is
// a deliberate over-reach: a server *can* serve different content at those two
// paths, and if one did, this would crawl only whichever it saw first. No site
// here does, and the alternative is crawling most sites twice.
func normalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	s := u.String()
	if trimmed := strings.TrimSuffix(s, "/"); trimmed != "" {
		return trimmed
	}
	return s
}

// RunSEOSpider crawls a site and returns the insights.
//
// progress is called with the running page count so the dashboard's progress
// bar moves during a crawl that can legitimately take nine minutes.
func RunSEOSpider(ctx context.Context, startURL string, progress func(pages int)) ([]Insight, error) {
	started := time.Now()
	log.Printf("[crawler] starting %s", startURL)

	result, err := crawl(ctx, startURL, progress)
	if err != nil {
		return nil, err
	}
	insights := runChecks(result)

	log.Printf("[crawler] done %s: %d pages, %d insights, %.1fs",
		startURL, len(result.Pages), len(insights), time.Since(started).Seconds())
	return insights, nil
}

func crawl(ctx context.Context, startURL string, progress func(pages int)) (*CrawlResult, error) {
	start, err := parseHTTPURL(startURL)
	if err != nil {
		return nil, err
	}
	host := start.Hostname()
	origin := start.Scheme + "://" + start.Host

	ctx, cancel := context.WithTimeout(ctx, CrawlDeadline)
	defer cancel()

	client := newCrawlClient()

	compression := probeCompression(ctx, client, startURL)
	robots, robotsCtx := loadRobots(ctx, client, origin)
	sitemapURLs := loadSitemap(ctx, client, origin, robotsCtx.Raw)

	var (
		seen    = map[string]bool{}
		fetched = map[string]bool{}
		queue   []string
		pages   []*Page
	)

	enqueue := func(raw string) {
		key := normalizeURL(raw)
		if seen[key] {
			return
		}
		seen[key] = true
		queue = append(queue, raw)
	}

	enqueue(startURL)
	// The sitemap is a second entry point, not just a checklist: it surfaces
	// pages nothing links to, which are exactly the ones worth auditing.
	for i, u := range sitemapURLs {
		if i >= PageCap {
			break
		}
		if sameSite(u, host) {
			enqueue(u)
		}
	}

	hitDeadline := false
	for len(queue) > 0 && len(pages) < PageCap {
		if ctx.Err() != nil {
			hitDeadline = true
			break
		}

		// Take a batch, skipping anything robots.txt disallows.
		var batch []string
		for len(queue) > 0 && len(batch) < Concurrency && len(pages)+len(batch) < PageCap {
			next := queue[0]
			queue = queue[1:]
			if !robots.Allowed(next) {
				continue
			}
			batch = append(batch, next)
		}
		if len(batch) == 0 {
			break
		}

		results := make([]FetchResult, len(batch))
		var wg sync.WaitGroup
		for i, u := range batch {
			wg.Add(1)
			go func(i int, u string) {
				defer wg.Done()
				results[i] = fetchPage(ctx, client, u)
			}(i, u)
		}
		wg.Wait()

		for _, r := range results {
			// A redirect can land two queued URLs on the same page. The
			// deduplication is on the *final* URL, which the request-time
			// check on the queue cannot do because it does not know it yet.
			finalKey := normalizeURL(r.URL)
			if fetched[finalKey] {
				seen[finalKey] = true
				continue
			}
			fetched[finalKey] = true
			seen[finalKey] = true

			page := &Page{
				URL:          r.URL,
				RequestedURL: r.RequestedURL,
				Status:       r.Status,
				ContentType:  r.ContentType,
				ElapsedMS:    r.ElapsedMS,
				Bytes:        len(r.Body),
				Headers:      r.Headers,
				RedirectHops: r.RedirectHops,
				Err:          r.Err,
				IsHTML:       r.Status == 200 && strings.Contains(r.ContentType, "text/html"),
			}

			if page.IsHTML {
				parsed, err := parseHTML(r.Body, r.URL)
				if err != nil {
					log.Printf("[crawler] parse failed for %s: %v", r.URL, err)
					page.IsHTML = false
				} else {
					page.HTML = parsed
					for _, link := range parsed.Links {
						if sameSite(link.URL, host) {
							enqueue(link.URL)
						}
					}
				}
			}

			pages = append(pages, page)
		}
		if progress != nil {
			progress(len(pages))
		}
	}

	if hitDeadline || ctx.Err() != nil {
		log.Printf("[crawler] hit deadline for %s after %d pages", startURL, len(pages))
	}

	return &CrawlResult{
		StartURL:           startURL,
		Host:               host,
		Pages:              pages,
		ExternalLinkStatus: probeExternalLinks(ctx, client, pages, host, startURL),
		SitemapURLs:        sitemapURLs,
		Robots:             robotsCtx,
		Compression:        compression,
	}, nil
}

// probeExternalLinks HEADs every distinct off-site link, capped and sorted.
//
// Sorted before truncating so that which links get probed is deterministic
// across runs of the same site. An unsorted set would report a different
// arbitrary subset every week, and a finding that appears and disappears on
// its own is worse than one that is consistently absent.
func probeExternalLinks(ctx context.Context, client *http.Client, pages []*Page, host, startURL string) map[string]int {
	external := map[string]bool{}
	for _, p := range pages {
		if !p.IsHTML || p.HTML == nil {
			continue
		}
		for _, link := range p.HTML.Links {
			if sameSite(link.URL, host) || isCrawlerHostile(link.URL) {
				continue
			}
			external[link.URL] = true
		}
	}

	status := map[string]int{}
	if len(external) == 0 || ctx.Err() != nil {
		return status
	}

	urls := make([]string, 0, len(external))
	for u := range external {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	if len(urls) > ExternalLinkCap {
		log.Printf("[crawler] capping external link probes at %d of %d for %s",
			ExternalLinkCap, len(urls), startURL)
		urls = urls[:ExternalLinkCap]
	}

	var mu sync.Mutex
	for i := 0; i < len(urls); i += Concurrency {
		// The page loop respects the deadline and so must this: a slow tail of
		// external probes would otherwise keep the crawl "running" long past
		// its budget, until the scheduler's wedge reset killed it and recorded
		// a successful crawl as an interruption.
		if ctx.Err() != nil {
			log.Printf("[crawler] hit deadline during external link probes for %s", startURL)
			break
		}

		end := min(i+Concurrency, len(urls))
		var wg sync.WaitGroup
		for _, u := range urls[i:end] {
			wg.Add(1)
			go func(u string) {
				defer wg.Done()
				code := headStatus(ctx, client, u)
				mu.Lock()
				status[u] = code
				mu.Unlock()
			}(u)
		}
		wg.Wait()
	}
	return status
}
