// Command logging aggregates the slog records the other sites in this repo ship
// over the Docker bridge (web/shipper.go), batching them into SQLite, ageing raw
// lines out while keeping hourly rollups forever, and serving them behind a login.
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
	"sync"
	"time"

	"logging.bythewood.me/web"
)

// Templates are source, so they ship in the binary unconditionally, the Vite
// bundle is build output and only embeds in a release build.
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

// csp allows 'unsafe-inline' for the analytics collector snippet and for
// Bootstrap's inline style attributes.
func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' https://analytics.bythewood.me/static/collector.js",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self' https://analytics.bythewood.me",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'self'",
	}, "; ")
}

type site struct {
	renderer *web.Renderer
	db       *sql.DB
	dist     fs.FS
	assets   *web.Assets
	writer   *Writer
	typst    *Typst
	watchdog *Watchdog

	auth *web.Authenticator

	slow slowAggregates

	baseScript  string
	baseStyles  []string
	pagesScript string
	pagesStyles []string
	dashScript  string
	dashStyles  []string
}

// Shared with the tests, which parse them for real, since a page listed here
// with no file behind it builds fine and only fails at boot.
var (
	layoutTemplates = []string{"base.html", "partials.html"}
	pageTemplates   = []string{
		"home.html", "documentation.html",
		"overview.html", "source.html", "search.html", "notfound.html",
	}
)

func main() {
	if err := realMain(); err != nil {
		slog.Error("exiting", slog.Any("err", err))
		os.Exit(1)
	}
}

// realMain returns instead of exiting so the deferred closes and the log
// shipper's flush run on every path out.
func realMain() error {
	web.SetupLogging()

	seed := flag.Bool("seed", false, "fill the database with realistic fake records, then exit")
	seedRecords := flag.Int("seed-records", 40000, "records to generate in -seed mode")
	seedDays := flag.Int("seed-days", 14, "days to spread -seed records over")
	// The container HEALTHCHECK runs this, the same way it does on the FROM
	// scratch sites that have no shell for a check to call.
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	preview := flag.String("preview-alert", "", "print an example alert ('silence', 'resumed' or 'restart') and exit")
	flag.Parse()

	if *preview != "" {
		if err := previewAlert(*preview); err != nil {
			return err
		}
		return nil
	}

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			return fmt.Errorf("healthcheck: %w", err)
		}
		return nil
	}

	dataDir := dir("SITE_DATA", "data")
	db, err := openDB(dataDir + "/db.sqlite3")
	if err != nil {
		return err
	}
	defer db.Close()

	if *seed {
		if err := runSeed(context.Background(), db, *seedRecords, *seedDays); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
		return nil
	}

	dist := distFS()

	assets, err := web.LoadAssets(dist)
	if err != nil {
		return err
	}

	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		return err
	}

	renderer, err := web.NewRenderer(templates, templateFuncs, layoutTemplates, pageTemplates)
	if err != nil {
		return err
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
		auth:        web.NewAuthenticator(),
		baseScript:  assets.Script("static_src/base/index.js"),
		baseStyles:  assets.Styles("static_src/base/index.js"),
		pagesScript: assets.Script("static_src/pages/index.js"),
		pagesStyles: assets.Styles("static_src/pages/index.js"),
		dashScript:  assets.Script("static_src/dashboard/index.js"),
		dashStyles:  assets.Styles("static_src/dashboard/index.js"),
	}

	// Must exist before anything is observed, since the watchdog is fed from
	// ingest. A failed bootstrap only loses the seeded source list.
	s.watchdog = NewWatchdog(db, NewNotifier().Fire)
	if err := s.watchdog.Bootstrap(context.Background()); err != nil {
		slog.Error("watchdog bootstrap failed; watching only what ships from now on",
			slog.String("component", "watchdog"),
			slog.Any("err", err))
	}
	// Waited on, since it writes to the database until it returns and the
	// deferred db.Close runs after this.
	watchCtx, stopWatching := context.WithCancel(context.Background())
	watched := make(chan struct{})
	go func() {
		s.watchdog.Run(watchCtx)
		close(watched)
	}()
	defer func() {
		stopWatching()
		<-watched
	}()

	// Local sink instead of HTTP, since posting to itself would be an ingest
	// request that logs a request that becomes an ingest request. Must close
	// before the writer so the last queued records are committed.
	shipper := web.ShipLogs(selfSource, s.LocalSink)
	defer shipper.Close()

	// Caddy cannot carry the shipper, so it opens a socket here instead and
	// writes its access log to it. See caddy.go.
	caddyCtx, stopCaddyLogs := context.WithCancel(context.Background())
	defer stopCaddyLogs()
	go func() {
		if err := s.ServeCaddyLogs(caddyCtx, caddyListenAddr); err != nil {
			slog.Error("caddy log listener stopped",
				slog.String("component", "caddy"),
				slog.Any("err", err))
		}
	}()

	// Cancelled on shutdown so a sweep stops between chunks instead of holding
	// the write lock while everything else drains, then waited on so the db is
	// not closed under it.
	sweepCtx, stopSweeping := context.WithCancel(context.Background())
	var sweeping sync.WaitGroup
	defer sweeping.Wait()
	defer stopSweeping()
	sweeping.Go(func() { NewSweeper(db).Run(sweepCtx) })

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /documentation", s.documentation)

	// Signing in happens on auth.bythewood.me. This stays so an old bookmark
	// and the "access dashboard" button both land somewhere useful.
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})

	// These carry request paths, IP addresses and CF-Ray values, and a
	// zone-wide Cloudflare Cache Rule added later would make them eligible.
	mux.HandleFunc("GET /overview", noStore(s.auth.RequireAuth(s.overview)))
	mux.HandleFunc("GET /sources/{source}", noStore(s.auth.RequireAuth(s.sourceDetail)))
	mux.HandleFunc("GET /search", noStore(s.auth.RequireAuth(s.search)))

	// Unauthenticated, and reachable only over the bridge since Caddy refuses
	// /ingest on the public hostname.
	mux.HandleFunc("POST /ingest", s.ingest)

	// Counts per source for the dash health strip, fenced the same way, and
	// since dash is public it never returns a message, a path or an address.
	mux.HandleFunc("GET /aggregate", s.aggregate)
	// The read only view chat.bythewood.me's tools call, behind the session
	// since it carries messages.
	mux.HandleFunc("GET /api/summary", noStore(s.auth.RequireAuthJSON(s.apiSummary)))

	// The mux only answers 405 itself when nothing else matches, and the
	// "GET /" catch-all below matches every GET path there is.
	for path, allow := range map[string]string{
		"/ingest": "POST",
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
		web.SameOrigin(),
	)

	slog.Info(fmt.Sprintf("logging serving %s (retention=%dd)",
		baseURL, int(rawRetention/(24*time.Hour))))
	return web.Serve(listenAddr, handler)
}

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

func (s *site) page(r *http.Request, title, description string) PageData {
	return PageData{
		Title:         title,
		Description:   description,
		Path:          r.URL.Path,
		Canonical:     baseURL + r.URL.Path,
		Authenticated: s.auth.Authenticated(r),
		Year:          time.Now().Year(),
		BaseURL:       baseURL,
		SourceURL:     sourceURL,
		SiteName:      siteName,
		AuthorName:    authorName,
		OGImage:       baseURL + "/static/og/card.png",
		JSONLD:        pageGraph(title, description, baseURL+r.URL.Path),
		Script:        s.baseScript,
		Styles:        s.baseStyles,

		AnalyticsID: analyticsID,
	}
}

// healthz answers 200 whenever the process is serving, it stays shallow because
// a full disk or a corrupt database is not fixed by the restart a failing check
// would trigger. ?verbose adds writer counters and is served to loopback only.
func (s *site) healthz(w http.ResponseWriter, r *http.Request) {
	if !r.URL.Query().Has("verbose") || !isLoopback(web.ClientIP(r)) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// EdgeCache fills in the site policy when a handler sets no
		// Cache-Control, and the edge would then answer a liveness check out
		// of cache long after this process stopped serving.
		w.Header().Set("Cache-Control", "no-store")
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
