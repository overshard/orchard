// analytics.bythewood.me: self-hosted, single-operator website analytics. An
// embedded collector script writes events into SQLite, and a dashboard renders
// them as metric tiles, charts, a world map and downloadable reports.
//
// Identity is hardcoded in site.go. The one value read from the environment is
// ANALYTICS_PASSWORD, because it is the one value that is actually a secret.
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

// Templates are source, so they ship inside the binary unconditionally. The
// Vite bundle and the topojson are build output and ship only in a release
// build; see assets_disk.go and assets_embed.go.
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
// script-src needs 'unsafe-inline' and cannot drop it without a markup change:
// the dashboard ships its chart data as inline
// <script type="application/json"> blocks and the self-tracking snippet is an
// inline script. style-src carries it for Bootstrap's inline style attributes.
//
// connect-src is 'self' only. This app is its own analytics backend, so there
// is no third-party origin to allow.
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
	shipper := web.ShipLogs("analytics", web.HTTPSink())
	defer shipper.Close()

	// Fail fast rather than defaulting. An internet-facing dashboard whose
	// password is "admin" because the environment was empty is the failure
	// mode this refuses to have.
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

	// Best effort, in the background, once. The server is already listening
	// by the time this runs: a 130MB download is not worth holding a deploy
	// open for, and the cost of failure is that session_start events land
	// without a country until the next boot.
	go func() {
		fresh, err := EnsureGeoIPDB(geoipPath)
		switch {
		case err != nil:
			slog.Info(fmt.Sprintf("geoip refresh skipped: %v", err))
		case fresh:
			s.geoip.Reload()
		}
	}()

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

	// /collect/ is a compatibility alias for embeds that hardcoded the
	// trailing slash. Those snippets are in other people's HTML and cannot be
	// edited from here.
	for _, path := range []string{"/collect", "/collect/"} {
		mux.HandleFunc("POST "+path, s.collect)
		mux.HandleFunc("OPTIONS "+path, s.collectOptions)
	}
	mux.HandleFunc("GET /static/collector.js", s.collectorScript)

	// A wrong method on a route that exists should answer 405, not 404. The
	// mux does that on its own only when no other pattern matches, and the
	// "GET /" catch-all below matches every GET path there is. 405 has to
	// carry Allow.
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

	// Per-country admin-1 topojson, fetched when the map is clicked.
	// Generated at image build from Natural Earth, so the names are stable
	// and a year of caching is right.
	mux.Handle("GET /static_maps/", http.StripPrefix("/static_maps/",
		cacheControl("public, max-age=31536000, immutable",
			http.FileServer(http.FS(mapsFS())))))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	// The dashboard's bare-UUID path is the catch-all. Registered as "/"
	// rather than "/{id}" so it cannot shadow /login and /properties: the mux
	// prefers a literal segment over a wildcard, but only among patterns that
	// match, and "/{id}" would also claim /nonsense-that-should-404.
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

		// Self-tracking. The collector posts to whatever origin served the
		// page, so this works unchanged in dev and in production. Empty on
		// staging, which renders no snippet at all.
		CollectorID:     collectorID(),
		CollectorServer: "",
	}
}
