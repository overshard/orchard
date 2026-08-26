// analytics.bythewood.me, rebuilt from Rust onto Go.
//
// Self-hosted, single-operator website analytics: an embedded collector script
// writes events into SQLite, and a dashboard renders them as metric tiles,
// charts, a world map and downloadable reports. Third site through the
// cloudflared -> Caddy -> Go path, after isaacbythewood.com and
// blog.bythewood.me, and the third step of decisions/0008's move off Rust.
//
// It is the first of the three with a database, so it is also where
// modernc.org/sqlite gets proved out. That mattered: decisions/0008 rested the
// whole Rust-to-Go case partly on the claim that a pure-Go SQLite keeps the
// static-binary property, and four more apps behind this one need it to be
// true.
//
// Identity is hardcoded in site.go. The one thing still read from the
// environment is ANALYTICS_PASSWORD, because it is the one thing that is
// actually a secret.
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
	"strings"
	"time"

	"bythewood.me/orchard/internal/web"
	"github.com/google/uuid"
)

// Templates are source, so they ship inside the binary. dist/ and static_maps/
// are not: they are build artifacts Docker copies in beside it, which keeps
// `go build ./...` working on a fresh clone before anyone has run Vite.
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
// <script type="application/json"> blocks and the self-tracking snippet is an
// inline script. style-src carries it for Bootstrap's inline style attributes.
//
// connect-src is 'self' only. Unlike the other two sites this app is its own
// analytics backend, so there is no third-party origin to allow.
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
	propriumID  uuid.UUID
	baseScript  string
	baseStyles  []string
	pagesScript string
	pagesStyles []string
	propsScript string
	propsStyles []string
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	seed := flag.Bool("seed", false, "fill a Seed Test property with realistic fake events, then exit")
	seedSessions := flag.Int("seed-sessions", 500, "sessions to generate in -seed mode")
	seedDays := flag.Int("seed-days", 60, "days to spread -seed sessions over")
	flag.Parse()

	// Fail fast rather than falling back to a default. An internet-facing
	// dashboard whose password is "admin" because the environment was empty is
	// the failure mode this refuses to have; it was added to the Rust version
	// in the 2026-07-20 hardening pass and is the reason the check is here
	// rather than at first login.
	password := os.Getenv("ANALYTICS_PASSWORD")
	if password == "" {
		log.Fatal("ANALYTICS_PASSWORD is unset; refusing to start an internet-facing server without one")
	}

	dataDir := dir("SITE_DATA", "data")
	db, err := openDB(dataDir + "/db.sqlite3")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	propriumID, err := ensureProprium(ctx, db)
	if err != nil {
		log.Fatalf("proprium property: %v", err)
	}

	if *seed {
		if err := runSeed(ctx, db, *seedSessions, *seedDays); err != nil {
			log.Fatalf("seed: %v", err)
		}
		return
	}

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
			"home.html", "changelog.html", "documentation.html",
			"login.html", "properties.html", "property.html", "notfound.html",
		},
	)
	if err != nil {
		log.Fatal(err)
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
		propriumID:  propriumID,
		baseScript:  assets.Script("static_src/base/index.js"),
		baseStyles:  assets.Styles("static_src/base/index.js"),
		pagesScript: assets.Script("static_src/pages/index.js"),
		pagesStyles: assets.Styles("static_src/pages/index.js"),
		propsScript: assets.Script("static_src/properties/index.js"),
		propsStyles: assets.Styles("static_src/properties/index.js"),
	}

	// Best effort, in the background, exactly once. The server is already
	// listening by the time this runs: a 130MB download is not something to
	// hold a deploy open for, and the only cost of it failing is that
	// session_start events land without a country until the next boot.
	go func() {
		fresh, err := EnsureGeoIPDB(geoipPath)
		switch {
		case err != nil:
			log.Printf("geoip refresh skipped: %v", err)
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

	// The public collector. /collect/ is a compatibility alias for embeds that
	// hardcoded the trailing slash, and it has to stay: those snippets are in
	// other people's HTML and cannot be edited from here.
	for _, path := range []string{"/collect", "/collect/"} {
		mux.HandleFunc("POST "+path, s.collect)
		mux.HandleFunc("OPTIONS "+path, s.collectOptions)
	}
	mux.HandleFunc("GET /static/collector.js", s.collectorScript)

	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /sitemap.xml", sitemap)

	mux.Handle("GET /static/", web.Static(dist, assets))

	// Per-country admin-1 topojson, lazy-fetched when the map is clicked.
	// Generated at image build from Natural Earth, so the names are stable and
	// a year of caching is right.
	mapsDir := dir("SITE_MAPS", "static_maps")
	mux.Handle("GET /static_maps/", http.StripPrefix("/static_maps/",
		cacheControl("public, max-age=31536000, immutable",
			http.FileServer(http.Dir(mapsDir)))))

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

	log.Printf("analytics serving %s (staging=%t, proprium=%s)", baseURL, Staging, propriumID)
	if err := web.Serve(listenAddr, handler); err != nil {
		log.Fatal(err)
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
		Script:        s.baseScript,
		Styles:        s.baseStyles,

		// Self-tracking. The collector posts to whatever origin served the
		// page, so this works unchanged on localhost, on the staging hostname
		// and in production.
		CollectorID:     s.propriumID.String(),
		CollectorServer: "",
	}
}
