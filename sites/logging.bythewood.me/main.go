// logging.bythewood.me: the consumer for logs every other site here was already
// producing and nothing was reading.
//
// Each site ships its own slog records in process over the Docker bridge (see
// web/shipper.go); this one batches them into SQLite, ages the raw lines out at
// thirty days while keeping hourly rollups forever, and renders the result as
// graphs behind one password.
//
// It is the third seat of three. status probes from outside and answers whether
// a site is up as seen from the internet. analytics observes from the visitor's
// browser and answers who came and from where. Neither can say what the origin
// itself actually did, and that is this.
//
// Identity is hardcoded in site.go. The one value read from the environment is
// LOGGING_PASSWORD, because it is the one value that is actually a secret.
package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"logging.bythewood.me/web"
)

// Templates are source, so they ship inside the binary unconditionally. The
// Vite bundle is build output and ships only in a release build; see
// assets_disk.go and assets_embed.go.
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
// script-src carries 'unsafe-inline' and https://analytics.bythewood.me, which
// matches the other four sites exactly. It was briefly dropped here on the
// grounds that this site had no inline executable script and, uniquely, renders
// text written by other programs. Adding the analytics collector brought it
// back: the collector is an inline snippet by design, and Isaac's call was
// parity with the rest of the repo over a second line of defence on one site.
//
// The first line of defence is unaffected and is the one that is tested: every
// log field is escaped for HTML, for Typst and for Markdown, and there are
// tests for all three, including one asserting that a message containing
// </script> cannot close the inline JSON blocks the charts read.
//
// style-src carries it for Bootstrap's inline style attributes.
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
	writer   *Writer
	typst    *Typst

	password  string
	cookieKey []byte

	baseScript  string
	baseStyles  []string
	pagesScript string
	pagesStyles []string
	dashScript  string
	dashStyles  []string
}

func main() {
	web.SetupLogging()

	seed := flag.Bool("seed", false, "fill the database with realistic fake records, then exit")
	seedRecords := flag.Int("seed-records", 40000, "records to generate in -seed mode")
	seedDays := flag.Int("seed-days", 14, "days to spread -seed records over")
	// The container HEALTHCHECK runs this. Three of the images in this repo
	// are FROM scratch and have no shell for a check to call, so every binary
	// probes itself and there is one behaviour across the repo rather than
	// two.
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	dataDir := dir("SITE_DATA", "data")
	db, err := openDB(dataDir + "/db.sqlite3")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer db.Close()

	if *seed {
		if err := runSeed(context.Background(), db, *seedRecords, *seedDays); err != nil {
			slog.Error(fmt.Sprintf("seed: %v", err))
			os.Exit(1)
		}
		return
	}

	// Fail fast rather than defaulting. An internet-facing dashboard over
	// every request every site here has served, whose password is "admin"
	// because the environment was empty, is the failure mode this refuses to
	// have.
	password := os.Getenv("LOGGING_PASSWORD")
	if password == "" {
		slog.Error("LOGGING_PASSWORD is unset; refusing to start an internet-facing server without one")
		os.Exit(1)
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
			"home.html", "documentation.html", "login.html",
			"overview.html", "source.html", "search.html", "notfound.html",
		},
	)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	writer := NewWriter(db)
	defer writer.Close()

	s := &site{
		renderer:    renderer,
		db:          db,
		dist:        dist,
		assets:      assets,
		writer:      writer,
		typst:       NewTypst(),
		password:    password,
		cookieKey:   sessionKey(password),
		baseScript:  assets.Script("static_src/base/index.js"),
		baseStyles:  assets.Styles("static_src/base/index.js"),
		pagesScript: assets.Script("static_src/pages/index.js"),
		pagesStyles: assets.Styles("static_src/pages/index.js"),
		dashScript:  assets.Script("static_src/dashboard/index.js"),
		dashStyles:  assets.Styles("static_src/dashboard/index.js"),
	}

	// This site ships its own logs like every other one, but through a local
	// sink rather than over HTTP: posting to itself would mean an ingest
	// request that logs a request that becomes an ingest request. Registered
	// after the writer exists and closed before it, so the last records in
	// the queue are committed rather than dropped.
	shipper := web.ShipLogs(selfSource, s.LocalSink)
	defer shipper.Close()

	// Retention runs for the life of the process. Cancelled on shutdown so a
	// sweep in progress stops between chunks instead of holding the write
	// lock while everything else is trying to drain.
	sweepCtx, stopSweeping := context.WithCancel(context.Background())
	defer stopSweeping()
	go NewSweeper(db).Run(sweepCtx)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /documentation", s.documentation)

	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("POST /logout", s.logout)

	// Everything behind the login is no-store. Nothing caches these today
	// (Cloudflare will not hold HTML on a free plan without a Cache Rule), but
	// that is a dashboard setting rather than a contract, and a zone-wide rule
	// added later for the other four sites would silently make these eligible.
	// These pages carry request paths, IP addresses and CF-Ray values; they are
	// the last thing that should sit in a shared cache.
	mux.HandleFunc("GET /overview", noStore(s.requireAuth(s.overview)))
	mux.HandleFunc("GET /sources/{source}", noStore(s.requireAuth(s.sourceDetail)))
	mux.HandleFunc("GET /search", noStore(s.requireAuth(s.search)))

	// The one route another program talks to. It is unauthenticated on
	// purpose and unreachable from outside on purpose: Caddy refuses /ingest
	// on the public hostname, so the only address that answers is the
	// container name on the orchard-edge bridge. See ingest.go.
	mux.HandleFunc("POST /ingest", s.ingest)

	// A wrong method on a route that exists should answer 405, not 404. The
	// mux does that on its own only when no other pattern matches, and the
	// "GET /" catch-all below matches every GET path there is. 405 has to
	// carry Allow.
	for path, allow := range map[string]string{
		"/ingest": "POST",
		"/logout": "POST",
	} {
		mux.HandleFunc("GET "+path, methodNotAllowed(allow))
	}

	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /sitemap.xml", sitemap)

	mux.Handle("GET /static/", web.Static(dist, assets))

	mux.HandleFunc("GET /healthz", s.healthz)

	mux.HandleFunc("GET /", s.notFound)

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
	)

	slog.Info(fmt.Sprintf("logging serving %s (staging=%t, retention=%dd)",
		baseURL, Staging, int(rawRetention/(24*time.Hour))))
	if err := web.Serve(listenAddr, handler); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

// noStore marks a response as private and uncacheable, at every layer.
func noStore(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, private")
		next(w, r)
	}
}

func methodNotAllowed(allow string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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

		// Off on staging, for the reason on the field itself.
		Analytics:   !Staging,
		AnalyticsID: analyticsID,
	}
}

// healthz is the container's liveness probe, and deliberately shallow: it
// answers 200 whenever the process is serving.
//
// It does NOT fail when the writer is failing, and that is a decision rather
// than an omission. Docker restarts a container whose health check fails, and
// the things that break ingest (a full disk, a corrupt database) are not fixed
// by restarting; failing here would turn a degraded site into a crash loop and
// lose the very stderr line that says what is wrong.
//
// What ingest health needs is a readiness signal for a person, so `?verbose`
// returns the writer's counters and the age of the newest record. Record
// freshness is the single best signal, because "the newest record is twenty
// minutes old" catches every silent failure at once: a wedged flusher, a full
// disk, commits failing every batch. It is served only to loopback, so
// `make doctor` and the health check can read it while the public hostname
// cannot: the counts are operational detail nobody outside needs.
func (s *site) healthz(w http.ResponseWriter, r *http.Request) {
	if !r.URL.Query().Has("verbose") || !isLoopback(web.ClientIP(r)) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
		return
	}

	stats := s.writer.Stats()
	body := map[string]any{
		"written":  stats.Written,
		"rejected": stats.Rejected,
		"failed":   stats.Failed,
		"queued":   stats.Queued,
		"capacity": stats.Capacity,
	}

	var newest sql.NullInt64
	if err := s.db.QueryRowContext(r.Context(), `SELECT MAX(ts) FROM records`).Scan(&newest); err == nil && newest.Valid {
		body["newest_record_age_s"] = (time.Now().UTC().UnixMilli() - newest.Int64) / 1000
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}
