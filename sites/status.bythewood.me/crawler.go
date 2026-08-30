package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

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
	// Compression is the start URL's Content-Encoding, or "" if it answered
	// uncompressed.
	Compression string
}

// normalizeURL is the key pages are deduplicated by. Dropping the trailing
// slash can merge two paths a server really does serve differently, but
// keeping it crawls most sites twice.
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

// RunSEOSpider crawls a site and returns the insights. progress, if non-nil,
// is called with the running page count after each batch.
func RunSEOSpider(ctx context.Context, startURL string, progress func(pages int)) ([]Insight, error) {
	started := time.Now()
	slog.Info(fmt.Sprintf("starting %s", startURL), slog.String("component", "crawler"))

	result, err := crawl(ctx, startURL, progress)
	if err != nil {
		return nil, err
	}
	insights := runChecks(result)

	slog.Info(fmt.Sprintf("done %s: %d pages, %d insights, %.1fs",
		startURL, len(result.Pages), len(insights), time.Since(started).Seconds()), slog.String("component", "crawler"))
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
	// The sitemap is a second entry point and not just a checklist, since it
	// surfaces pages nothing links to.
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
			// A redirect can land two queued URLs on the same page, so dedupe
			// on the final URL, which the queue cannot know yet.
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
					slog.Error(fmt.Sprintf("parse failed for %s: %v", r.URL, err), slog.String("component", "crawler"))
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
		slog.Info(fmt.Sprintf("hit deadline for %s after %d pages", startURL, len(pages)), slog.String("component", "crawler"))
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

// probeExternalLinks HEADs every distinct off-site link. Sorting before the
// cap keeps the probed subset the same from one run to the next.
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
		slog.Info(fmt.Sprintf("capping external link probes at %d of %d for %s",
			ExternalLinkCap, len(urls), startURL), slog.String("component", "crawler"))
		urls = urls[:ExternalLinkCap]
	}

	var mu sync.Mutex
	for i := 0; i < len(urls); i += Concurrency {
		// Without this a slow tail of probes runs past the crawl budget and the
		// watchdog records a finished crawl as an interruption.
		if ctx.Err() != nil {
			slog.Info(fmt.Sprintf("hit deadline during external link probes for %s", startURL), slog.String("component", "crawler"))
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
