package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/temoto/robotstxt"
)

// Fetching, for the SEO crawler.
//
// Unlike checker.go this uses a pooled http.Client. The checker measures what a
// cold visitor pays and must not reuse connections; the crawler makes up to
// five hundred requests to one host, where reuse is both faster and more
// polite.

const (
	// PageCap bounds one crawl, as the fence against a calendar or a faceted
	// search generating URLs forever.
	PageCap = 500
	// Concurrency is low on purpose. This is pointed at somebody's site,
	// sometimes a small one on shared hosting, and four parallel requests is
	// the difference between an audit and a load test.
	Concurrency = 4
	// ExternalLinkCap bounds the outbound HEAD probes. A 500-page site can
	// easily surface thousands of distinct external links, and at four-way
	// concurrency with an eight-second timeout an unbounded list runs for
	// hours.
	ExternalLinkCap = 500
	// CrawlDeadline is nine minutes, against the scheduler's fifteen-minute
	// watchdog. The gap means a crawl that overruns ends itself and records
	// what it found rather than being killed and recorded as an
	// interruption.
	CrawlDeadline = 540 * time.Second

	// MaxBodyBytes caps one HTML body. Real pages are well under this;
	// anything larger is a mislabeled download, and buffering it whole times
	// four-way concurrency could exhaust the container.
	MaxBodyBytes = 5 * 1024 * 1024

	crawlerRequestTimeout = 15 * time.Second
	externalLinkTimeout   = 8 * time.Second

	// A real user agent with a contact URL, unlike the checker's Chrome
	// impersonation. The checker has to look like a visitor to measure what
	// one experiences; the crawler is a robot and should say so, so an
	// operator reading their logs knows who to contact.
	crawlerUserAgent = "status (+" + baseURL + ")"
)

// FetchResult is one fetched URL.
type FetchResult struct {
	URL          string
	RequestedURL string
	Status       int
	Headers      map[string]string
	Body         []byte
	ContentType  string
	ElapsedMS    int64
	RedirectHops int
	Err          string
}

func newCrawlClient() *http.Client {
	return &http.Client{
		Timeout: crawlerRequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// fetchPage retrieves one URL. It never returns an error: a failed fetch is a
// finding, recorded as status 0 with the error text, and the checks report it
// as an unreachable link rather than aborting the crawl.
func fetchPage(ctx context.Context, client *http.Client, rawURL string) FetchResult {
	started := time.Now()
	result := FetchResult{URL: rawURL, RequestedURL: rawURL, Headers: map[string]string{}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		result.Err = err.Error()
		result.ElapsedMS = time.Since(started).Milliseconds()
		return result
	}
	req.Header.Set("User-Agent", crawlerUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		result.Err = err.Error()
		result.ElapsedMS = time.Since(started).Milliseconds()
		return result
	}
	defer resp.Body.Close()

	result.URL = canonicalURL(resp.Request.URL)
	result.Status = resp.StatusCode
	// Go follows redirects inside Do and hands back the final response, with
	// the chain reachable only through resp.Request. Counting the hops is what
	// the redirect-chain check needs.
	for r := resp.Request; r != nil; r = r.Response.Request {
		if r.Response == nil {
			break
		}
		result.RedirectHops++
	}

	for k, v := range resp.Header {
		if len(v) > 0 {
			result.Headers[strings.ToLower(k)] = v[0]
		}
	}
	result.ContentType = strings.ToLower(result.Headers["content-type"])

	// Only HTML bodies are read. Everything else is fetched for its status
	// code and headers alone, so a linked 200MB video costs one round trip
	// rather than 200MB of memory.
	if resp.StatusCode == http.StatusOK && strings.Contains(result.ContentType, "text/html") {
		body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
		if err != nil && len(body) == 0 {
			result.Err = err.Error()
		}
		if len(body) == MaxBodyBytes {
			slog.Info(fmt.Sprintf("body cap %d hit for %s", MaxBodyBytes, rawURL), slog.String("component", "crawler"))
		}
		result.Body = body
	} else {
		// Drained so the connection returns to the pool instead of being
		// dropped and re-handshaked for the next page.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	}

	result.ElapsedMS = time.Since(started).Milliseconds()
	return result
}

// canonicalURL renders a fetched URL with an explicit "/" path.
//
// An absolute HTTP URL with an empty path means "/", and every other source of
// URLs in a crawl spells it that way: a sitemap lists "https://example.com/",
// and so does a link to the site root. url.String() preserves the empty path it
// was given, so a property registered as "https://example.com" keys a page that
// matches neither, and the root page gets reported "not listed in sitemap" when
// the sitemap lists it.
func canonicalURL(u *url.URL) string {
	if u.Path == "" {
		clone := *u
		clone.Path = "/"
		return clone.String()
	}
	return u.String()
}

// headStatus probes an external link. A HEAD that comes back 403, 405 or 501
// usually means the server refuses the method rather than that the link is
// broken, so those retry as a GET. Otherwise every link to a site with a strict
// WAF is reported dead.
func headStatus(ctx context.Context, client *http.Client, rawURL string) int {
	ctx, cancel := context.WithTimeout(ctx, externalLinkTimeout)
	defer cancel()

	try := func(method string) int {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
		if err != nil {
			return 0
		}
		req.Header.Set("User-Agent", crawlerUserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		return resp.StatusCode
	}

	status := try(http.MethodHead)
	switch status {
	case http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return try(http.MethodGet)
	}
	return status
}

// probeCompression reports the server's Content-Encoding for a URL, or "" when
// the response came back uncompressed.
//
// Setting Accept-Encoding by hand is what makes this work. Go's transport
// otherwise adds `Accept-Encoding: gzip` itself, transparently decodes the
// response and then *removes* Content-Encoding from the headers, so a normal
// fetch can never tell whether the server compressed anything.
func probeCompression(ctx context.Context, client *http.Client, rawURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", crawlerUserAgent)
	req.Header.Set("Accept-Encoding", "gzip, br, zstd, deflate")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	if enc == "" || enc == "identity" {
		return ""
	}
	return enc
}

// Robots wraps a parsed robots.txt. A missing or unparseable file allows
// everything: this crawls sites its operator owns, and a typo in a robots.txt
// should not produce an empty audit that looks like a healthy one.
type Robots struct {
	group *robotstxt.Group
}

func (r *Robots) Allowed(rawURL string) bool {
	if r == nil || r.group == nil {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return r.group.Test(path)
}

// RobotsCtx is what the checks see: whether the file exists, its text, and
// whether it points at a sitemap.
type RobotsCtx struct {
	URL               string
	Exists            bool
	Raw               string
	ReferencesSitemap bool
}

func loadRobots(ctx context.Context, client *http.Client, origin string) (*Robots, RobotsCtx) {
	robotsURL := strings.TrimSuffix(origin, "/") + "/robots.txt"
	out := RobotsCtx{URL: robotsURL}

	result := fetchPageAllowingText(ctx, client, robotsURL)
	if result.Status != http.StatusOK || len(result.Body) == 0 {
		return &Robots{}, out
	}

	out.Exists = true
	out.Raw = string(result.Body)
	for _, line := range strings.Split(out.Raw, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "sitemap:") {
			out.ReferencesSitemap = true
			break
		}
	}

	data, err := robotstxt.FromBytes(result.Body)
	if err != nil {
		slog.Info(fmt.Sprintf("robots.txt at %s did not parse, allowing everything: %v", robotsURL, err), slog.String("component", "crawler"))
		return &Robots{}, out
	}
	return &Robots{group: data.FindGroup("*")}, out
}

// fetchPageAllowingText is fetchPage for the two non-HTML documents the crawler
// needs the body of: robots.txt and sitemap.xml. fetchPage discards every
// non-HTML body, which is right for pages and wrong for these two.
func fetchPageAllowingText(ctx context.Context, client *http.Client, rawURL string) FetchResult {
	started := time.Now()
	result := FetchResult{URL: rawURL, RequestedURL: rawURL, Headers: map[string]string{}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		result.Err = err.Error()
		return result
	}
	req.Header.Set("User-Agent", crawlerUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		result.Err = err.Error()
		result.ElapsedMS = time.Since(started).Milliseconds()
		return result
	}
	defer resp.Body.Close()

	result.URL = canonicalURL(resp.Request.URL)
	result.Status = resp.StatusCode
	result.Body, _ = io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result
}

var locPattern = regexp.MustCompile(`(?is)<loc>\s*([^<]+?)\s*</loc>`)

// loadSitemap collects the page URLs a site advertises.
//
// Sitemaps nest: an index lists other sitemaps, which list pages. The loop
// follows that, capped at twenty documents, which is enough for a real site and
// bounded against an index that points at itself.
func loadSitemap(ctx context.Context, client *http.Client, origin, robotsText string) []string {
	var queue []string
	for _, line := range strings.Split(robotsText, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 8 || !strings.EqualFold(trimmed[:8], "sitemap:") {
			continue
		}
		// Sliced off the original rather than the lowercased copy, because a
		// URL path is case sensitive and lowercasing it produces a 404.
		if target := strings.TrimSpace(trimmed[8:]); target != "" {
			queue = append(queue, target)
		}
	}
	if len(queue) == 0 {
		queue = append(queue, strings.TrimSuffix(origin, "/")+"/sitemap.xml")
	}

	seen := map[string]bool{}
	var urls []string

	for len(queue) > 0 && len(seen) < 20 {
		current := queue[0]
		queue = queue[1:]
		if seen[current] {
			continue
		}
		seen[current] = true

		result := fetchPageAllowingText(ctx, client, current)
		if result.Status != http.StatusOK {
			continue
		}
		for _, m := range locPattern.FindAllStringSubmatch(string(result.Body), -1) {
			loc := strings.TrimSpace(m[1])
			if loc == "" {
				continue
			}
			lower := strings.ToLower(loc)
			if strings.HasSuffix(lower, ".xml") || strings.Contains(lower, "sitemap") {
				queue = append(queue, loc)
			} else {
				urls = append(urls, loc)
			}
		}
	}
	return urls
}

// sameSite decides whether a URL belongs to the site being crawled. The www
// variant counts as the same site in both directions: a site linking between
// its apex and its www host is one site with a redirect, and treating them as
// separate would report every such link as external.
func sameSite(rawURL, host string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	target := strings.ToLower(u.Hostname())
	if target == "" {
		return false
	}
	host = strings.ToLower(host)
	return target == host ||
		target == "www."+host ||
		host == "www."+target
}

// crawlerHostileHosts return 403 or 404 to anything that does not look like a
// browser, whether or not the link works, so probing them produces only false
// positives and they are skipped from the external HEAD probe.
//
// The list is kept short and specific: it suppresses real findings, so a host
// has to earn its place on it.
var crawlerHostileHosts = map[string]bool{
	"linkedin.com":     true,
	"www.linkedin.com": true,
}

func isCrawlerHostile(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return crawlerHostileHosts[host] || strings.HasSuffix(host, ".linkedin.com")
}
