package main

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const badPage = `<html><head><title>Shop</title>
<meta name="viewport" content="width=device-width, user-scalable=no">
<link rel="stylesheet" href="/a.css"><link rel="stylesheet" href="/b.css"><link rel="stylesheet" href="/c.css">
<script src="/js/jquery-3.4.1.min.js"></script>
</head><body>
<h1>Shop</h1><h3>Skipped</h3>
<img src="/hero.png"><img src="/gone.png" alt="">
<a href="/x"></a><a href="/y">click here</a>
<button></button>
<form><input name="email"></form>
<div id="d"></div><div id="d" role="bogus" tabindex="3"></div>
</body></html>`

const goodPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Shop</title>
<meta name="description" content="A shop."><meta name="viewport" content="width=device-width">
<link rel="stylesheet" href="/a.css"><script type="module" src="/app.js"></script>
</head><body><main><h1>Shop</h1><h2>Things</h2>
<img src="/small.webp" alt="A thing" width="100" height="100">
<a href="/x">All the things</a>
<label for="e">Email</label><input id="e" name="email">
</main></body></html>`

func auditServer(t *testing.T, page string, secure bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if secure {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "strict-origin")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Encoding", "gzip")
		var b bytes.Buffer
		zw := gzip.NewWriter(&b)
		_, _ = zw.Write([]byte(page))
		_ = zw.Close()
		_, _ = w.Write(b.Bytes())
	})
	css := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/a.css", css(`@font-face { font-family: "Geist"; src: url(/geist.woff2); }`))
	mux.HandleFunc("/b.css", css(`body{}`))
	mux.HandleFunc("/c.css", css(`body{}`))
	mux.HandleFunc("/js/jquery-3.4.1.min.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte("/*! jQuery v3.4.1 */" + strings.Repeat("x", 3000)))
	})
	mux.HandleFunc("/hero.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bytes.Repeat([]byte{1}, 300<<10))
	})
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("User-agent: *\n")) })
	for _, p := range []string{"/app.js", "/small.webp", "/geist.woff2"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			_, _ = w.Write([]byte("ok"))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func findingTitles(d *Details) string {
	var b strings.Builder
	for _, f := range d.Findings {
		b.WriteString(f.Category + ": " + f.Title + " | " + f.Detail + "\n")
	}
	return b.String()
}

func TestAuditFindsWhatsWrong(t *testing.T) {
	srv := auditServer(t, badPage, false)
	scores, d, err := runAudit(t.Context(), srv.Client(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	got := findingTitles(d)
	for _, want := range []string{
		"Page has no lang attribute", "Images without alt text", "Links with no text", "Buttons with no name",
		"Form fields without a label", "Zooming is blocked", "Unknown ARIA roles", "Positive tabindex",
		"Duplicate ids", "Headings skip a level | h1 to h3", "No main landmark",
		"No Content-Security-Policy", "Missing or old doctype", "jQuery 3.4.1 has known vulnerabilities",
		"No meta description", "Links that say nothing",
		"Render-blocking stylesheets and scripts", "Images over 250 KB", "could be WebP",
		"Images without width and height", "text stays invisible while they load | Geist", "Requests that fail",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no finding for %q in:\n%s", want, got)
		}
	}
	if scores.Accessibility > 60 || scores.BestPractices > 60 {
		t.Errorf("a page this broken scored %+v", scores)
	}
	if d.Findings[0].Severity != sevError {
		t.Errorf("errors should sort first, got %+v", d.Findings[0])
	}
}

func TestAuditPassesACleanPage(t *testing.T) {
	srv := auditServer(t, goodPage, true)
	scores, d, err := runAudit(t.Context(), srv.Client(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	// httptest is plain http, so HTTPS and HSTS are the only fair failures.
	for _, f := range d.Findings {
		if f.Category == catA11y || f.Category == catSEO {
			t.Errorf("a clean page got %s: %s %s", f.Category, f.Title, f.Detail)
		}
	}
	if scores.Accessibility != 100 || scores.SEO != 100 || scores.Performance < 90 {
		t.Errorf("a clean page scored %+v\n%s", scores, findingTitles(d))
	}
	var weight *Metric
	for i := range d.Metrics {
		if d.Metrics[i].ID == "page-weight" {
			weight = &d.Metrics[i]
		}
	}
	if weight == nil || weight.Score == nil || *weight.Score != 1 {
		t.Errorf("page weight metric = %+v", weight)
	}
}

func TestAuditRefusesAnErrorPage(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, _, err := runAudit(t.Context(), srv.Client(), srv.URL+"/"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a 404 page audited as %v", err)
	}
}

func TestLibraryVersions(t *testing.T) {
	for _, c := range []struct {
		v    string
		rule map[int]string
		bad  bool
	}{
		{"3.4.1", map[int]string{3: "3.5.0"}, true},
		{"3.5.0", map[int]string{3: "3.5.0"}, false},
		{"3.10.0", map[int]string{3: "3.5.0"}, false},
		{"1.12.4", map[int]string{1: ""}, true},
		{"5.3.0", map[int]string{3: "3.4.1"}, false},
	} {
		if got := libraryProblem(c.v, c.rule) != ""; got != c.bad {
			t.Errorf("%s flagged %v, want %v", c.v, got, c.bad)
		}
	}
}

func TestCacheSeconds(t *testing.T) {
	for cc, want := range map[string]int{
		"public, max-age=31536000, immutable": 365 * 24 * 3600,
		"max-age=600":                         600,
		"no-store":                            0,
		"":                                    0,
	} {
		if got := cacheSeconds(cc); got != want {
			t.Errorf("cacheSeconds(%q) = %d, want %d", cc, got, want)
		}
	}
}
