// status.bythewood.me is single-operator uptime monitoring. An in-process
// scheduler probes every tracked URL, audits it with Lighthouse and crawls it
// for SEO; the handlers mostly render what the scheduler wrote.
package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"status.bythewood.me/web"
)

// Templates are source and ship in the binary unconditionally; the Vite bundle
// is build output and ships only in a release build.
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

// script-src needs 'unsafe-inline' because the dashboard ships its chart data as
// inline <script type="application/json"> blocks, and style-src carries it for
// Bootstrap's inline style attributes.
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
	web.SetupLogging()

	previewKind := flag.String("preview-alert", "",
		"print the ntfy notification for 'down' or 'recovery' and exit")
	// The container HEALTHCHECK runs this, since a scratch image has no shell.
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	// Tees onto the stdout handler rather than replacing it. Kept after the
	// healthcheck branch, so a HEALTHCHECK never starts a queue it cannot flush.
	shipper := web.ShipLogs("status", web.HTTPSink())
	defer shipper.Close()

	if *previewKind != "" {
		if err := previewAlert(*previewKind); err != nil {
			slog.Error("startup failed", slog.Any("err", err))
			os.Exit(1)
		}
		return
	}

	password := os.Getenv("STATUS_PASSWORD")
	if password == "" {
		slog.Error("STATUS_PASSWORD is unset; refusing to start an internet-facing server without one")
		os.Exit(1)
	}

	dataDir := dir("SITE_DATA", "data")
	db, err := openDB(dataDir + "/db.sqlite3")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer db.Close()

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
		templateFuncs,
		[]string{"base.html", "partials.html"},
		[]string{
			"home.html", "changelog.html", "login.html",
			"properties.html", "property.html", "notfound.html",
		},
	)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
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

	// The scheduler stops with the process, so a deploy does not kill a crawl
	// halfway and leave the row wedged for the watchdog to find.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduler := NewScheduler(db, NewNotifier(), dir("SITE_ROOT", "."))
	if err := scheduler.ResetOnBoot(ctx); err != nil {
		slog.Error(fmt.Sprintf("reset wedged states: %v", err))
		os.Exit(1)
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

	// Reachable without a session for a public property; the handler does that
	// check itself, because it needs the property row to know.
	mux.HandleFunc("GET /properties/{id}/status", s.propertyStatus)
	mux.HandleFunc("POST /properties/{id}/recrawl", s.requireAuthJSON(s.propertyRecrawl))
	mux.HandleFunc("POST /properties/{id}/rerun-lighthouse", s.requireAuthJSON(s.propertyRerunLighthouse))

	// A wrong method on an existing route should be 405 with Allow. The mux only
	// does that when nothing else matches, and "GET /" below always matches.
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
		// EdgeCache fills in the site policy whenever a handler sets no
		// Cache-Control of its own, so saying nothing here means the edge
		// answers a liveness check out of cache long after this process has
		// stopped serving.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})

	// Registered as "/" rather than "/{id}" so it cannot shadow /login and
	// /properties; "/{id}" would also claim /nonsense-that-should-404.
	mux.HandleFunc("GET /", s.dashboardOrNotFound)

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
	)

	slog.Info(fmt.Sprintf("status serving %s (staging=%t)", baseURL, Staging))
	if err := web.Serve(listenAddr, handler); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
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
		Title:         title,
		Description:   description,
		Path:          r.URL.Path,
		Canonical:     baseURL + r.URL.Path,
		Staging:       Staging,
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
