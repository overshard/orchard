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

const (
	// PageCap is the fence against a calendar or a faceted search that
	// generates URLs forever.
	PageCap = 500
	// Concurrency stays low because this is pointed at somebody else's site,
	// sometimes a small one on shared hosting.
	Concurrency = 4
	// ExternalLinkCap bounds the outbound HEAD probes, since a large site can
	// surface thousands of distinct external links.
	ExternalLinkCap = 500
	// CrawlDeadline sits under the scheduler's fifteen-minute watchdog, so an
	// overrunning crawl ends itself and records what it found instead of being
	// killed.
	CrawlDeadline = 540 * time.Second

	// MaxBodyBytes caps one HTML body, because buffering a mislabeled download
	// four times over could exhaust the container.
	MaxBodyBytes = 5 * 1024 * 1024

	crawlerRequestTimeout = 15 * time.Second
	externalLinkTimeout   = 8 * time.Second

	// A real user agent with a contact URL, unlike the checker's Chrome
	// impersonation, so an operator reading their logs knows who to reach.
	crawlerUserAgent = "status (+" + baseURL + ")"
)

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

// fetchPage retrieves one URL and never fails. A failed fetch comes back as
// status 0 with the error text, which reads as an unreachable link.
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
	// Go follows redirects inside Do and hands back only the final response,
	// with the chain reachable through resp.Request.
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

	// Only HTML bodies are read, so a linked 200MB video costs a round trip
	// instead of 200MB of memory.
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
		// Drained so the connection goes back to the pool instead of being
		// dropped and re-handshaked.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	}

	result.ElapsedMS = time.Since(started).Milliseconds()
	return result
}

// canonicalURL renders a fetched URL with an explicit "/" path, since
// url.String() keeps the empty path it was given and nothing else spells it
// that way.
func canonicalURL(u *url.URL) string {
	if u.Path == "" {
		clone := *u
		clone.Path = "/"
		return clone.String()
	}
	return u.String()
}

// headStatus probes an external link. A 403, 405 or 501 usually means the
// server refuses HEAD rather than that the link is broken, so those retry.
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
// the response came back uncompressed. Accept-Encoding is set by hand because
// Go's transport otherwise decodes for you and strips the header off.
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
// everything, so a typo cannot produce an empty audit that reads as healthy.
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

// fetchPageAllowingText is fetchPage for robots.txt and sitemap.xml, whose
// bodies fetchPage would throw away as non-HTML.
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

// loadSitemap collects the page URLs a site advertises. Sitemaps nest, so it
// follows index documents too, capped at twenty against a self-referring one.
func loadSitemap(ctx context.Context, client *http.Client, origin, robotsText string) []string {
	var queue []string
	for _, line := range strings.Split(robotsText, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 8 || !strings.EqualFold(trimmed[:8], "sitemap:") {
			continue
		}
		// Sliced off the original and not the lowercased copy, since a URL path
		// is case sensitive.
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

// sameSite counts the www variant as the same site in both directions, or
// every link between a site's apex and its www host reports as external.
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

// crawlerHostileHosts answer 403 or 404 to anything that is not a browser, so
// the HEAD probe skips them. Keep the list short, it suppresses real findings.
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
