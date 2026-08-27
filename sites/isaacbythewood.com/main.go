// isaacbythewood.com, rebuilt from Next.js onto Go.
//
// Server-rendered html/template with a Vite-built frontend, one static binary,
// no database, no framework. It is the first real site through the
// cloudflared -> Caddy -> Go path proved by taproot's tunnel test on
// 2026-08-24, and the pilot for decisions/0008's move off Rust.
//
// Nothing here is configurable and nothing here is secret. The identity is
// hardcoded in site.go on purpose; see decisions/0008.
package main

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"bythewood.me/orchard/internal/web"
)

// Templates are source, so they ship inside the binary. The Vite build output
// is not: it is a build artifact that Docker copies alongside the binary, the
// same split the Rust apps use. Embedding dist as well would mean `go build`
// failing on a fresh clone until someone had run Vite.
//
//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

// distDir is where the Vite build lands, resolved relative to this file's
// directory at build time in dev and set to /dist in the image.
func distDir() string {
	if dir := os.Getenv("SITE_DIST"); dir != "" {
		return dir
	}
	return "dist"
}

// Content-Security-Policy.
//
// No 'unsafe-eval' anywhere: the Next.js version needed it in dev for its own
// runtime, and there is no dev runtime any more. 'unsafe-inline' for scripts
// survives only for the analytics collector loader, which is a literal inline
// script; it is off on staging anyway, since Analytics is false there.
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
	log.SetFlags(log.LstdFlags | log.LUTC)

	dist := os.DirFS(distDir())

	assets, err := web.LoadAssets(dist)
	if err != nil {
		log.Fatal(err)
	}

	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		log.Fatal(err)
	}

	renderer, err := web.NewRenderer(
		templates,
		nil,
		[]string{"base.html"},
		[]string{"index.html", "about.html", "code.html", "art.html", "contact.html", "notfound.html"},
	)
	if err != nil {
		log.Fatal(err)
	}

	commits := NewCommitCache()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slugs := make([]string, 0, len(projects))
	for _, project := range projects {
		slugs = append(slugs, project.Slug)
	}
	commits.Start(ctx, slugs)

	// The home page's promo slot, kept current from the blog rather than
	// hardcoded to a project that can be retired out from under it.
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

	// Not logged and not behind the security headers: it exists for the
	// container health check and for probing the origin from inside the
	// bridge, and a line per probe would bury the real traffic.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.Handle("GET /static/", web.Static(dist, assets))

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
	)

	log.Printf("isaacbythewood.com serving %s (staging=%t)", baseURL, Staging)
	if err := web.Serve(listenAddr, handler); err != nil {
		log.Fatal(err)
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
