// isaacbythewood.com: server-rendered html/template with a Vite-built frontend,
// one static binary, no database and no third party Go dependency.
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

// Templates are source and ship in the binary unconditionally. The Vite bundle
// is build output, so it ships only in a release build, see assets_embed.go.
//
//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

// 'unsafe-inline' in script-src is there for the analytics collector loader,
// which is a literal inline script. No 'unsafe-eval' anywhere.
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

	// The container HEALTHCHECK runs this: a FROM scratch image has no shell
	// for a check to call, so the binary probes itself.
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

	// Live cards only. An archived repo does not change, so polling it spends
	// the rate limit re-reading the same commit.
	targets := make([]CommitTarget, 0, len(sites)+len(projects))
	for _, live := range sites {
		targets = append(targets, live.CommitTarget())
	}
	for _, project := range projects {
		if !project.Archived {
			targets = append(targets, CommitTarget{Key: project.Slug, Repo: project.Slug})
		}
	}
	commits.Start(ctx, targets)

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

	mux.Handle("GET /favicon.ico", rootAsset(dist, "favicon.ico"))
	mux.Handle("GET /favicon.svg", rootAsset(dist, "favicon.svg"))

	// Not logged, or a line per probe would bury the real traffic.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// EdgeCache fills in the site policy whenever a handler sets no
		// Cache-Control of its own, so saying nothing here means the edge
		// answers a liveness check out of cache long after this process has
		// stopped serving.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.Handle("GET /static/", web.Static(dist, assets))

	// Next.js image optimiser URLs, still arriving from search indexes.
	s.nextImages = newNextImageIndex(dist)
	mux.HandleFunc("GET /_next/image", s.nextImage)

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
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
