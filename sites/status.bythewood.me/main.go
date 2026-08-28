// status.bythewood.me, rebuilt from Rust onto Go.
//
// Self-hosted, single-operator uptime monitoring: an in-process scheduler
// probes every tracked URL every three minutes, audits it with Lighthouse
// daily and crawls it for SEO weekly, and a dashboard renders the results as
// charts, an insight list and downloadable reports. Fourth site through the
// cloudflared -> Caddy -> Go path, after isaacbythewood.com,
// blog.bythewood.me and analytics.bythewood.me, and the fourth step of
// decisions/0008's move off Rust.
//
// It is the largest port of the four, 5,303 lines of Rust across 24 files, and
// the only one of them that is not primarily a web server: most of what this
// binary does happens on a timer with nobody watching. That shapes everything.
// The scheduler is the load-bearing part and the HTTP handlers mostly render
// what it wrote.
//
// Two things came out rather than across, both settled before the port began:
// email, which decisions/0007 established cannot work from a residential
// address, and the Discord webhook, which Isaac dropped with it on 2026-08-26.
// Both are replaced by a single ntfy publish. See alerts.go.
//
// Identity is hardcoded in site.go. The one thing still read from the
// environment is STATUS_PASSWORD, because it is the one thing that is actually
// a secret.
package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"status.bythewood.me/web"
)

// Templates are source, so they ship inside the binary. dist/ is not: it is a
// build artifact Docker copies in beside it, which keeps `go build ./...`
// working on a fresh clone before anyone has run Vite.
//
//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

func dir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

// Content-Security-Policy.
//
// 'unsafe-inline' in script-src is load bearing and cannot be removed without
// changing the markup: the dashboard ships its chart data as inline
// <script type="application/json"> blocks. style-src carries it for
// Bootstrap's inline style attributes.
//
// analytics.bythewood.me appears in script-src and connect-src because this
// site now carries the collector, which loads a script from that origin and
// then posts events back to it. It is the only third-party origin here, it is
// Isaac's own, and it is listed unconditionally rather than only when the
// snippet renders: a policy that changes shape between staging and production
// is a policy that gets tested in one shape and shipped in the other.
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
		"frame-ancestors 'self'",
	}, "; ")
}

// site is everything the handlers share.
type site struct {
	renderer *web.Renderer
	db       *sql.DB
	dist     fs.FS
	assets   *web.Assets
	typst    *Typst

	password  string
	cookieKey []byte

	baseScript  string
	baseStyles  []string
	pagesScript string
	pagesStyles []string
	propsScript string
	propsStyles []string
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	previewKind := flag.String("preview-alert", "",
		"print the ntfy notification for 'down' or 'recovery' and exit")
	flag.Parse()

	// Rendering a preview needs neither a database nor a password, so it is
	// handled before anything else is set up. It replaces the Rust version's
	// `status preview-email` subcommand.
	if *previewKind != "" {
		if err := previewAlert(*previewKind); err != nil {
			log.Fatal(err)
		}
		return
	}

	// Fail fast rather than falling back to a default. An internet-facing
	// dashboard whose password is "admin" because the environment was empty is
	// the failure mode this refuses to have; it was added to the Rust version
	// in the 2026-07-21 hardening pass and is the reason the check is here
	// rather than at first login.
	password := os.Getenv("STATUS_PASSWORD")
	if password == "" {
		log.Fatal("STATUS_PASSWORD is unset; refusing to start an internet-facing server without one")
	}

	dataDir := dir("SITE_DATA", "data")
	db, err := openDB(dataDir + "/db.sqlite3")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	distDir := dir("SITE_DIST", "dist")
	dist := os.DirFS(distDir)

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
		templateFuncs,
		[]string{"base.html", "partials.html"},
		[]string{
			"home.html", "changelog.html", "login.html",
			"properties.html", "property.html", "notfound.html",
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	s := &site{
		renderer:    renderer,
		db:          db,
		dist:        dist,
		assets:      assets,
		typst:       NewTypst(),
		password:    password,
		cookieKey:   sessionKey(password),
		baseScript:  assets.Script("static_src/base/index.js"),
		baseStyles:  assets.Styles("static_src/base/index.js"),
		pagesScript: assets.Script("static_src/pages/index.js"),
		pagesStyles: assets.Styles("static_src/pages/index.js"),
		propsScript: assets.Script("static_src/properties/index.js"),
		propsStyles: assets.Styles("static_src/properties/index.js"),
	}

	// The scheduler stops when the process is asked to stop, so a deploy does
	// not kill a crawl halfway and leave the row wedged for the watchdog to
	// find. The HTTP server drains on the same signal, in internal/web.Serve.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduler := NewScheduler(db, NewNotifier(), dir("SITE_ROOT", "."))
	if err := scheduler.ResetOnBoot(ctx); err != nil {
		log.Fatalf("reset wedged states: %v", err)
	}
	go scheduler.Run(ctx)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /changelog", s.changelog)

	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("POST /logout", s.logout)

	mux.HandleFunc("GET /properties", s.requireAuth(s.properties))
	mux.HandleFunc("POST /properties", s.requireAuth(s.propertyCreate))
	mux.HandleFunc("POST /properties/{id}/delete", s.requireAuth(s.propertyDelete))
	mux.HandleFunc("POST /properties/{id}/public", s.requireAuthJSON(s.propertyPublic))

	// Polled by the dashboard, so it is reachable for a public property
	// without a session. The handler does that check itself, because it needs
	// the property row to know whether it is public.
	mux.HandleFunc("GET /properties/{id}/status", s.propertyStatus)
	mux.HandleFunc("POST /properties/{id}/recrawl", s.requireAuthJSON(s.propertyRecrawl))
	mux.HandleFunc("POST /properties/{id}/rerun-lighthouse", s.requireAuthJSON(s.propertyRerunLighthouse))

	// A wrong method on a route that exists is a 405, not a 404.
	//
	// Go's mux does this on its own, but only when no other pattern matches,
	// and the bare "GET /" catch-all below matches every GET path there is. So
	// without these the answer is the 404 page, which tells a caller the
	// endpoint does not exist when it does. Answering 404 where the Rust
	// version said 405 was the one real defect analytics' end-to-end sweep
	// found in its own port, so it is fixed here before it can be found again.
	// 405 also has to carry Allow, which is why the header is not optional.
	for path, allow := range map[string]string{
		"/logout":                           "POST",
		"/properties/{id}/delete":           "POST",
		"/properties/{id}/public":           "POST",
		"/properties/{id}/recrawl":          "POST",
		"/properties/{id}/rerun-lighthouse": "POST",
	} {
		mux.HandleFunc("GET "+path, methodNotAllowed(allow))
	}

	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /sitemap.xml", sitemap)

	mux.Handle("GET /static/", web.Static(dist, assets))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	// The dashboard's bare-UUID path is the catch-all. Registering it as "/"
	// rather than "/{id}" is what keeps it from shadowing /login and
	// /properties: Go's mux prefers a literal segment over a wildcard, but
	// only among patterns that match, and "/{id}" would also claim
	// /nonsense-that-should-404.
	mux.HandleFunc("GET /", s.dashboardOrNotFound)

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
	)

	log.Printf("status serving %s (staging=%t)", baseURL, Staging)
	if err := web.Serve(listenAddr, handler); err != nil {
		log.Fatal(err)
	}
}

func methodNotAllowed(allow string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// dashboardOrNotFound routes a bare "/<uuid>" to a dashboard and everything
// else to a 404.
func (s *site) dashboardOrNotFound(w http.ResponseWriter, r *http.Request) {
	slug := strings.Trim(r.URL.Path, "/")
	if id, err := uuid.Parse(slug); err == nil && !strings.Contains(slug, "/") {
		s.dashboard(w, r, id)
		return
	}
	s.notFound(w, r)
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "404", "That page does not exist.")
	s.renderer.Render(w, http.StatusNotFound, "notfound.html", data)
}

// page builds the half of PageData every template needs.
func (s *site) page(r *http.Request, title, description string) PageData {
	return PageData{
		Title:       title,
		Description: description,
		Path:        r.URL.Path,
		Canonical:   baseURL + r.URL.Path,
		Staging:     Staging,
		// Off on the staging hostname, on at cutover, the same gate the blog
		// and isaacbythewood.com use. Nothing to flip by hand.
		Analytics:     !Staging,
		AnalyticsID:   analyticsID,
		Authenticated: isAuthenticated(r, s.cookieKey),
		Year:          time.Now().Year(),
		BaseURL:       baseURL,
		SourceURL:     sourceURL,
		OGImage:       baseURL + "/static/og/card.png",
		JSONLD:        pageGraph(title, description, baseURL+r.URL.Path),
		SiteName:      siteName,
		AuthorName:    authorName,
		Script:        s.baseScript,
		Styles:        s.baseStyles,
	}
}
