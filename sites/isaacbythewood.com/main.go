// isaacbythewood.com: server-rendered html/template with a Vite-built
// frontend. One static binary, no database, no framework, and no third party
// Go dependency.
//
// Nothing here is configurable and nothing here is secret. Identity is
// hardcoded in site.go.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"isaacbythewood.com/web"
)

// Templates are source, so they ship inside the binary unconditionally. The
// Vite bundle is build output and ships only in a release build; see
// assets_disk.go and assets_embed.go.
//
//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

// Content-Security-Policy. No 'unsafe-eval' anywhere. 'unsafe-inline' in
// script-src is only there for the analytics collector loader, which is a
// literal inline script.
func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' https://analytics.bythewood.me",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"connect-src 'self' https://analytics.bythewood.me",
		"manifest-src 'self'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'self'",
	}, "; ")
}

func main() {
	web.SetupLogging()

	// The container HEALTHCHECK runs this. Two of these images are FROM
	// scratch and have no shell for a check to call, so the binary probes
	// itself.
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	// Log shipping. Installed on top of the stdout handler rather than in
	// place of it: every record still goes where it always went, and a copy
	// is queued for logging.bythewood.me. Nothing here can block a request,
	// and a logging site that is down or slow costs some lines on a
	// dashboard. See web/shipper.go.
	//
	// After the healthcheck branch above, so a HEALTHCHECK invocation does
	// not spin up a queue it will never flush.
	shipper := web.ShipLogs("isaacbythewood", web.HTTPSink())
	defer shipper.Close()

	dist := distFS()

	assets, err := web.LoadAssets(dist)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	renderer, err := web.NewRenderer(
		templates,
		nil,
		[]string{"base.html"},
		[]string{"index.html", "about.html", "code.html", "art.html", "contact.html", "notfound.html"},
	)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	commits := NewCommitCache()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Only the live projects. Archived repositories do not change, so polling
	// them would spend the unauthenticated rate limit re-reading the same
	// commit forever.
	slugs := make([]string, 0, len(projects))
	for _, project := range projects {
		if !project.Archived {
			slugs = append(slugs, project.Slug)
		}
	}
	commits.Start(ctx, slugs)

	// The home page's promo slot, filled from the blog rather than hardcoded
	// to a project that can be retired out from under it.
	latest := NewLatestCache(blogLatestSources)
	latest.Start(ctx)

	s := &site{
		renderer: renderer,
		commits:  commits,
		latest:   latest,
		script:   assets.Script("index.js"),
		styles:   assets.Styles("index.js"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.home)
	mux.HandleFunc("GET /about", s.about)
	mux.HandleFunc("GET /code", s.code)
	mux.HandleFunc("GET /art", s.art)
	mux.HandleFunc("GET /contact", s.contact)

	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET /manifest.json", s.manifest)

	// Browsers ask for /favicon.ico at the root whatever the markup says, so
	// it is served from both places rather than logging a 404 on every fresh
	// visit.
	mux.Handle("GET /favicon.ico", rootAsset(dist, "favicon.ico"))
	mux.Handle("GET /favicon.svg", rootAsset(dist, "favicon.svg"))

	// Not logged and not behind the security headers: a line per probe would
	// bury the real traffic.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.Handle("GET /static/", web.Static(dist, assets))

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		// Five minutes fresh, then a day of serving stale while revalidating
		// behind the request, then a week of serving stale if the origin is
		// down.
		//
		// No s-maxage. Per RFC 9111 it carries proxy-revalidate semantics, so
		// Cloudflare reads it as "never serve stale without asking first" and
		// disables stale-while-revalidate and stale-if-error both.
		web.EdgeCache("public, max-age=300, "+
			"stale-while-revalidate=86400, stale-if-error=604800"),
	)

	slog.Info(fmt.Sprintf("isaacbythewood.com serving %s (staging=%t)", baseURL, Staging))
	if err := web.Serve(listenAddr, handler); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

// rootAsset serves one file out of dist at a root path.
func rootAsset(dist fs.FS, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := dist.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		seeker, ok := f.(io.ReadSeeker)
		if !ok {
			http.NotFound(w, r)
			return
		}

		// Unhashed, so a short cache rather than an immutable one.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeContent(w, r, filepath.Base(name), info.ModTime(), seeker)
	})
}
