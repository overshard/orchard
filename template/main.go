// __SITE__: server-rendered html/template with a Vite-built frontend and one
// static binary.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"__SITE__/web"
)

// Templates are source and ship in the binary unconditionally. The Vite bundle
// is build output, so it ships only in a release build, see assets_embed.go.
//
//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

// Package vars so a test parses the same list the server does, since a page
// listed here with no file behind it compiles and then fails at boot.
var (
	layoutTemplates = []string{"base.html"}
	pageTemplates   = []string{"index.html", "notfound.html"}
)

// 'unsafe-inline' in script-src is for the analytics collector snippet, which
// is an inline script.
func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' https://analytics.bythewood.me",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self' https://analytics.bythewood.me",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

type site struct {
	renderer *web.Renderer
	dist     fs.FS
	assets   *web.Assets
}

func main() {
	web.SetupLogging()

	// A FROM scratch image has no shell for HEALTHCHECK to call, so the binary
	// probes itself.
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	// Below the healthcheck branch, so a HEALTHCHECK invocation does not start
	// a queue it will never flush.
	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

	s, err := newSite(distFS())
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	slog.Info(fmt.Sprintf("%s serving %s (staging=%t)", selfSource, baseURL, Staging))
	if err := web.Serve(listenAddr, s.handler()); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func newSite(dist fs.FS) (*site, error) {
	assets, err := web.LoadAssets(dist)
	if err != nil {
		return nil, err
	}
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		return nil, err
	}
	renderer, err := web.NewRenderer(templates, nil, layoutTemplates, pageTemplates)
	if err != nil {
		return nil, err
	}
	return &site{renderer: renderer, dist: dist, assets: assets}, nil
}

// handler wires every route. It is a method so the tests drive the real mux.
func (s *site) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /static/", web.Static(s.dist, s.assets))
	mux.HandleFunc("GET /", s.notFound)

	return web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		web.SameOrigin(),
		// No s-maxage, since Cloudflare reads it as never serve stale and that
		// turns off stale-if-error, which is what covers the tunnel dropping.
		web.EdgeCache("public, max-age=300, "+
			"stale-while-revalidate=86400, stale-if-error=604800"),
	)
}

type page struct {
	Title       string
	Description string
	SiteName    string
	SourceURL   string
	Staging     bool
	AnalyticsID string
	Script      string
	Styles      []string
	Year        int
}

func (s *site) page(title, description string) page {
	return page{
		Title:       title,
		Description: description,
		SiteName:    siteName,
		SourceURL:   sourceURL,
		Staging:     Staging,
		AnalyticsID: analyticsID,
		Script:      s.assets.Script("src/index.js"),
		Styles:      s.assets.Styles("src/index.js"),
		Year:        time.Now().UTC().Year(),
	}
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	s.renderer.Render(w, http.StatusOK, "index.html", s.page("Home", siteDesc))
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderer.Render(w, http.StatusNotFound, "notfound.html",
		s.page("Not Found", "That page does not exist."))
}

// robots keeps a staging hostname out of search results, since a noindex meta
// tag only works if the crawler fetches the page.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if Staging {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	_, _ = w.Write([]byte("User-agent: *\nDisallow:\n"))
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Without this EdgeCache fills in the site policy and the edge answers a
	// liveness check out of cache long after this process has stopped.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}
