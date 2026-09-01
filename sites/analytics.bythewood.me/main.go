// analytics.bythewood.me: self-hosted, single-operator website analytics. An
// embedded collector script writes events into SQLite, and a dashboard renders
// them as metric tiles, charts, a world map and downloadable reports.
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
	"strings"
	"time"

	"analytics.bythewood.me/web"
	"github.com/google/uuid"
)

// Templates are source and always ship in the binary; the Vite bundle and the
// topojson are build output and only ship in a release build.
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

// script-src needs 'unsafe-inline' for the inline self-tracking snippet, and
// style-src for Bootstrap's style attributes. Neither can drop it without a
// markup change.
func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
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
	geoip    *GeoIP
	ua       *UAParser
	typst    *Typst

	password    string
	cookieKey   []byte
	baseScript  string
	baseStyles  []string
	pagesScript string
	pagesStyles []string
	propsScript string
	propsStyles []string
}

func main() {
	web.SetupLogging()

	seed := flag.Bool("seed", false, "fill a Seed Test property with realistic fake events, then exit")
	seedSessions := flag.Int("seed-sessions", 500, "sessions to generate in -seed mode")
	seedDays := flag.Int("seed-days", 60, "days to spread -seed sessions over")
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

	// Tees stdout records to logging.bythewood.me; see web/shipper.go. It goes
	// after the healthcheck branch so a HEALTHCHECK does not start a queue it
	// will never flush.
	shipper := web.ShipLogs("analytics", web.HTTPSink())
	defer shipper.Close()

	// No default: an empty environment must not silently ship a weak password.
	password := os.Getenv("ANALYTICS_PASSWORD")
	if password == "" {
		slog.Error("ANALYTICS_PASSWORD is unset; refusing to start an internet-facing server without one")
		os.Exit(1)
	}

	dataDir := dir("SITE_DATA", "data")
	db, err := openDB(dataDir + "/db.sqlite3")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer db.Close()

	ctx := context.Background()

	if *seed {
		if err := runSeed(ctx, db, *seedSessions, *seedDays); err != nil {
			slog.Error(fmt.Sprintf("seed: %v", err))
			os.Exit(1)
		}
		return
	}

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
			"home.html", "changelog.html", "documentation.html",
			"login.html", "properties.html", "property.html", "notfound.html",
		},
	)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	geoipPath := dataDir + "/db.mmdb"
	s := &site{
		renderer:    renderer,
		db:          db,
		dist:        dist,
		assets:      assets,
		geoip:       LoadGeoIP(geoipPath),
		ua:          NewUAParser(),
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

	// Best effort in the background, since the download is large and failing
	// only costs country enrichment until the next boot.
	go func() {
		fresh, err := EnsureGeoIPDB(geoipPath)
		switch {
		case err != nil:
			slog.Info(fmt.Sprintf("geoip refresh skipped: %v", err))
		case fresh:
			s.geoip.Reload()
		}
	}()

	// Cancelled on shutdown so a sweep stops between chunks instead of holding
	// the write lock while everything else drains.
	sweepCtx, stopSweeping := context.WithCancel(context.Background())
	defer stopSweeping()
	go NewSweeper(db).Run(sweepCtx)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /changelog", s.changelog)
	mux.HandleFunc("GET /documentation", s.documentation)

	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("POST /logout", s.logout)

	mux.HandleFunc("GET /properties", s.requireAuth(s.properties))
	mux.HandleFunc("POST /properties", s.requireAuth(s.propertyCreate))
	mux.HandleFunc("POST /properties/{id}/delete", s.requireAuth(s.propertyDelete))
	mux.HandleFunc("POST /properties/{id}/cards", s.requireAuth(s.propertyCards))
	mux.HandleFunc("POST /properties/{id}/public", s.requireAuth(s.propertyPublic))

	// /collect/ is an alias for embeds that hardcoded the trailing slash; those
	// snippets live in other people's HTML.
	for _, path := range []string{"/collect", "/collect/"} {
		mux.HandleFunc("POST "+path, s.collect)
		mux.HandleFunc("OPTIONS "+path, s.collectOptions)
	}
	mux.HandleFunc("GET /static/collector.js", s.collectorScript)

	// The mux only answers 405 by itself when nothing else matches, and the
	// "GET /" catch-all below matches every GET path there is.
	for path, allow := range map[string]string{
		"/collect":                "OPTIONS, POST",
		"/collect/":               "OPTIONS, POST",
		"/logout":                 "POST",
		"/properties/{id}/delete": "POST",
		"/properties/{id}/cards":  "POST",
		"/properties/{id}/public": "POST",
	} {
		mux.HandleFunc("GET "+path, methodNotAllowed(allow))
	}

	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /sitemap.xml", sitemap)

	mux.Handle("GET /static/", web.Static(dist, assets))

	// Per-country admin-1 topojson, generated at image build, so the filenames
	// are stable and cacheable for a year.
	mux.Handle("GET /static_maps/", http.StripPrefix("/static_maps/",
		cacheControl("public, max-age=31536000, immutable",
			http.FileServer(http.FS(mapsFS())))))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// EdgeCache fills in the site policy whenever a handler sets no
		// Cache-Control of its own, so saying nothing here means the edge
		// answers a liveness check out of cache long after this process has
		// stopped serving.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})

	// Registered as "/" and not "/{id}", which would claim every one-segment
	// path and leave nothing to 404.
	mux.HandleFunc("GET /", s.dashboardOrNotFound)

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
	)

	slog.Info(fmt.Sprintf("analytics serving %s (staging=%t, property=%s)", baseURL, Staging, analyticsID))
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

func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
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
		Authenticated: isAuthenticated(r, s.cookieKey),
		Year:          time.Now().Year(),
		BaseURL:       baseURL,
		SourceURL:     sourceURL,
		SiteName:      siteName,
		AuthorName:    authorName,
		OGImage:       baseURL + "/static/og/card.png",
		JSONLD:        pageGraph(title, description, baseURL+r.URL.Path),
		Script:        s.baseScript,
		Styles:        s.baseStyles,

		// The collector posts to whatever origin served the page, so an empty
		// server works in dev and in production alike.
		CollectorID:     collectorID(),
		CollectorServer: "",
	}
}
