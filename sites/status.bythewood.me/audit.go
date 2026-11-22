package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// The audit is plain HTTP and HTML parsing, so it sees what a server sends and
// not what a browser renders. Paint timings come from real visitors through
// analytics instead.
const (
	auditTimeout        = 2 * time.Minute
	auditMaxResources   = 80
	auditMaxImages      = 40
	auditMaxFonts       = 8
	auditFetchWorkers   = 4
	auditMaxBody        = 8 << 20
	auditMaxDecodedText = 4 << 20
)

// Scores are the category headlines, as whole percentages. The JSON names
// are the rendered labels and are stored that way, so renaming one orphans rows.
type Scores struct {
	Performance   int64 `json:"Performance"`
	Accessibility int64 `json:"Accessibility"`
	BestPractices int64 `json:"Best practices"`
	SEO           int64 `json:"SEO"`
}

// ScorePair is one category score, for iterating in a fixed order.
type ScorePair struct {
	Label string
	Score int64
}

// Pairs returns the scores as a slice so the tiles can't rearrange themselves
// between page loads.
func (s *Scores) Pairs() []ScorePair {
	return []ScorePair{
		{"Performance", s.Performance},
		{"Accessibility", s.Accessibility},
		{"Best practices", s.BestPractices},
		{"SEO", s.SEO},
	}
}

// Metric is one weighted number behind the performance score.
type Metric struct {
	ID           string   `json:"id"`
	Acronym      string   `json:"acronym"`
	Title        string   `json:"title"`
	DisplayValue string   `json:"display_value"`
	Score        *float64 `json:"score"`
	Weight       float64  `json:"weight"`
}

// Finding is one check that didn't pass, worst first.
type Finding struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Severity string `json:"severity"`
}

// Details is what sits under the scores on the property page.
type Details struct {
	Metrics  []Metric  `json:"metrics"`
	Findings []Finding `json:"findings"`
	Passed   int       `json:"passed"`
	Checked  int       `json:"checked"`
}

// fetched is one response as the wire carried it. Body is decoded, and only
// kept for the document, stylesheets and scripts.
type fetched struct {
	URL          string
	Kind         string
	Status       int
	WireBytes    int64
	Encoding     string
	CacheControl string
	ContentType  string
	TTFBMS       int64
	Body         []byte
	Headers      http.Header
	Err          string
}

// runAudit fetches a page and everything it loads, then scores it.
func runAudit(ctx context.Context, client *http.Client, pageURL string) (*Scores, *Details, error) {
	ctx, cancel := context.WithTimeout(ctx, auditTimeout)
	defer cancel()

	doc := auditFetch(ctx, client, pageURL, "document", true)
	if doc.Err != "" {
		return nil, nil, fmt.Errorf("fetch %s: %s", pageURL, doc.Err)
	}
	if doc.Status != http.StatusOK {
		return nil, nil, fmt.Errorf("%s answered %d", pageURL, doc.Status)
	}
	if !strings.Contains(doc.ContentType, "text/html") {
		return nil, nil, fmt.Errorf("%s is %q, not HTML", pageURL, doc.ContentType)
	}
	base, _ := url.Parse(doc.URL)
	page, err := walkPage(doc.Body, base)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", pageURL, err)
	}

	subs := fetchSubresources(ctx, client, page)
	robots := auditFetch(ctx, client, base.Scheme+"://"+base.Host+"/robots.txt", "robots", false)

	a := &auditRun{doc: doc, base: base, page: page, subs: subs, robotsStatus: robots.Status}
	scores, details := a.score()
	return scores, details, nil
}

// auditFetch asks for gzip and nothing else, so the bytes counted are what the
// wire carried and a body can still be decoded without a brotli dependency.
func auditFetch(ctx context.Context, client *http.Client, rawURL, kind string, keepBody bool) fetched {
	f := fetched{URL: rawURL, Kind: kind}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		f.Err = err.Error()
		return f
	}
	// The probe's browser agent, since the audit stands in for a visit and a
	// site that turns crawlers away would otherwise never be audited.
	req.Header.Set("User-Agent", probeUserAgent)
	req.Header.Set("Accept-Encoding", "gzip")

	var start, first time.Time
	trace := &httptrace.ClientTrace{
		WroteRequest:         func(httptrace.WroteRequestInfo) { start = time.Now() },
		GotFirstResponseByte: func() { first = time.Now() },
	}
	resp, err := client.Do(req.WithContext(httptrace.WithClientTrace(ctx, trace)))
	if err != nil {
		var certErr *tls.CertificateVerificationError
		if errors.As(err, &certErr) {
			f.Err = "certificate: " + certErr.Error()
		} else {
			f.Err = err.Error()
		}
		return f
	}
	defer resp.Body.Close()
	if !start.IsZero() && !first.IsZero() {
		f.TTFBMS = first.Sub(start).Milliseconds()
	}
	f.URL = resp.Request.URL.String()
	f.Status = resp.StatusCode
	f.Headers = resp.Header
	f.Encoding = strings.ToLower(resp.Header.Get("Content-Encoding"))
	f.CacheControl = strings.ToLower(resp.Header.Get("Cache-Control"))
	f.ContentType = strings.ToLower(resp.Header.Get("Content-Type"))

	raw, err := io.ReadAll(io.LimitReader(resp.Body, auditMaxBody))
	f.WireBytes = int64(len(raw))
	if err != nil && len(raw) == 0 {
		f.Err = err.Error()
		return f
	}
	if !keepBody {
		return f
	}
	switch f.Encoding {
	case "", "identity":
		f.Body = raw
	case "gzip":
		if zr, err := gzip.NewReader(bytes.NewReader(raw)); err == nil {
			f.Body, _ = io.ReadAll(io.LimitReader(zr, auditMaxDecodedText))
		}
	}
	return f
}

// fetchSubresources gets what the page loads, a handful at a time. Fonts are
// found inside the stylesheets, so they're a second round.
func fetchSubresources(ctx context.Context, client *http.Client, page *pageInfo) []fetched {
	type job struct{ url, kind string }
	var jobs []job
	seen := map[string]bool{}
	add := func(u, kind string) {
		if u == "" || seen[u] || len(jobs) >= auditMaxResources || !strings.HasPrefix(u, "http") {
			return
		}
		seen[u] = true
		jobs = append(jobs, job{u, kind})
	}
	for _, s := range page.Stylesheets {
		add(s.URL, "stylesheet")
	}
	for _, s := range page.Scripts {
		add(s.URL, "script")
	}
	for i, img := range page.Images {
		if i >= auditMaxImages {
			break
		}
		add(img.URL, "image")
	}

	run := func(jobs []job) []fetched {
		out := make([]fetched, len(jobs))
		sem := make(chan struct{}, auditFetchWorkers)
		var wg sync.WaitGroup
		for i, j := range jobs {
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				out[i] = auditFetch(ctx, client, j.url, j.kind, j.kind != "image" && j.kind != "font")
			})
		}
		wg.Wait()
		return out
	}
	got := run(jobs)

	var fonts []job
	for _, f := range got {
		if f.Kind != "stylesheet" || f.Body == nil {
			continue
		}
		u, err := url.Parse(f.URL)
		if err != nil {
			continue
		}
		for _, ff := range parseFontFaces(string(f.Body), u) {
			if ff.URL != "" && !seen[ff.URL] && len(fonts) < auditMaxFonts {
				seen[ff.URL] = true
				fonts = append(fonts, job{ff.URL, "font"})
			}
		}
	}
	return append(got, run(fonts)...)
}

// fontFace is one @font-face rule, its first src and whether it sets
// font-display.
type fontFace struct {
	URL     string
	Display bool
	Family  string
}

func parseFontFaces(css string, base *url.URL) []fontFace {
	var out []fontFace
	lower := strings.ToLower(css)
	for {
		i := strings.Index(lower, "@font-face")
		if i < 0 {
			return out
		}
		open := strings.Index(lower[i:], "{")
		if open < 0 {
			return out
		}
		end := strings.Index(lower[i+open:], "}")
		if end < 0 {
			return out
		}
		block := css[i+open+1 : i+open+end]
		lb := lower[i+open+1 : i+open+end]
		ff := fontFace{Display: strings.Contains(lb, "font-display")}
		if j := strings.Index(lb, "font-family"); j >= 0 {
			v := block[j+len("font-family"):]
			if k := strings.IndexAny(v, ";}"); k >= 0 {
				v = v[:k]
			}
			ff.Family = strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), ":")), `"'`)
		}
		if j := strings.Index(lb, "url("); j >= 0 {
			v := block[j+4:]
			if k := strings.Index(v, ")"); k >= 0 {
				ref := strings.Trim(strings.TrimSpace(v[:k]), `"'`)
				if !strings.HasPrefix(ref, "data:") {
					ff.URL = resolve(base, ref)
				}
			}
		}
		out = append(out, ff)
		lower = lower[i+open+end:]
		css = css[i+open+end:]
	}
}

// thirdParty is a host outside the page's own site, compared on the last two
// labels so a www or static subdomain still counts as the site itself.
func thirdParty(pageHost, host string) bool {
	site := func(h string) string {
		parts := strings.Split(strings.ToLower(h), ".")
		if len(parts) <= 2 {
			return strings.Join(parts, ".")
		}
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return site(pageHost) != site(host)
}

func kb(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
