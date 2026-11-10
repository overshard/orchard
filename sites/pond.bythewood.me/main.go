// pond.bythewood.me: a frog each, kept by a key in a cookie, with the game's
// state in SQLite and its rules on the server's clock.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pond.bythewood.me/web"
)

// Templates are source and ship in the binary unconditionally. The Vite bundle
// is build output, so it ships only in a release build, see assets_embed.go.
//
//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

// Package vars so a test parses the same list the server does, since a page
// listed here with no file behind it compiles and then fails at boot.
var (
	layoutTemplates = []string{"base.html"}
	pageTemplates   = []string{"index.html", "key.html", "k.html", "share.html", "notfound.html"}
)

// 'unsafe-inline' in script-src is for the analytics collector snippet, which
// is an inline script.
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
		"frame-ancestors 'none'",
	}, "; ")
}

// The pages that show or carry a key load no script but their own.
func keyCSP() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

var templateFuncs = template.FuncMap{
	"commas":     commas,
	"lower":      strings.ToLower,
	"hatsInGame": func() int { return hatsInGame },
}

type site struct {
	renderer *web.Renderer
	dist     fs.FS
	assets   *web.Assets
	store    *store
	restores *limiter
}

func main() {
	web.SetupLogging()

	// A FROM scratch image has no shell for HEALTHCHECK to call, so the binary
	// probes itself.
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
	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

	// The pepper keys the hash a pond key is stored as, and it lives in .env
	// so that the database alone opens nobody's pond.
	pepper := os.Getenv("POND_PEPPER")
	if pepper == "" && devBuild {
		pepper = "a pepper for development only"
	}
	if len(pepper) < 32 && !devBuild {
		slog.Error("startup failed", slog.String("err", "POND_PEPPER is unset or under 32 characters"))
		os.Exit(1)
	}
	path := os.Getenv("POND_DB")
	if path == "" {
		path = "data/pond.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	db, err := openStore(path, []byte(pepper))
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer db.Close()
	go sweepHourly(db)
	go db.day.poll(context.Background())

	s, err := newSite(distFS(), db)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	slog.Info(fmt.Sprintf("%s serving %s", selfSource, baseURL))
	if err := web.Serve(listenAddr, s.handler()); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
	}
}

func sweepHourly(db *store) {
	for {
		if n, err := db.sweep(); err != nil {
			slog.Error("sweep", slog.Any("err", err))
		} else if n > 0 {
			slog.Info(fmt.Sprintf("swept %d eggs nobody came back to", n))
		}
		time.Sleep(time.Hour)
	}
}

func newSite(dist fs.FS, db *store) (*site, error) {
	assets, err := web.LoadAssets(dist)
	if err != nil {
		return nil, err
	}
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		return nil, err
	}
	renderer, err := web.NewRenderer(templates, templateFuncs, layoutTemplates, pageTemplates)
	if err != nil {
		return nil, err
	}
	return &site{
		renderer: renderer, dist: dist, assets: assets, store: db,
		restores: newLimiter(10*time.Minute, 10, 600),
	}, nil
}

// handler wires every route. It is a method so the tests drive the real mux.
func (s *site) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /key", s.keyPage)
	mux.HandleFunc("GET /k", s.keyLanding)
	mux.HandleFunc("GET /f/{slug}", s.sharePage)
	mux.HandleFunc("GET /f/{slug}/card.png", s.shareCard)
	mux.HandleFunc("GET /robots.txt", robots)

	mux.HandleFunc("GET /api/pond", s.apiPond)
	mux.HandleFunc("GET /api/panel", s.apiPanel)
	mux.HandleFunc("POST /api/greet", post(s.apiGreet))
	mux.HandleFunc("POST /api/key/restore", post(s.apiRestore))
	mux.HandleFunc("POST /api/key/rotate", s.played(s.apiRotate))
	mux.HandleFunc("POST /api/dive/start", s.played(s.apiDiveStart))
	mux.HandleFunc("POST /api/dive/finish", s.played(s.apiDiveFinish))
	mux.HandleFunc("POST /api/wheel/spin", s.played(s.apiSpin))
	mux.HandleFunc("POST /api/log", s.played(s.apiLog))
	mux.HandleFunc("POST /api/geode", s.played(s.apiCrack))
	mux.HandleFunc("POST /api/hats/box", s.played(s.apiBox))
	mux.HandleFunc("POST /api/hats/wear", s.played(s.apiWear))
	mux.HandleFunc("POST /api/hats/sell", s.played(s.apiSell))
	if devBuild {
		mux.HandleFunc("POST /api/dev/clock", post(s.apiDevClock))
		mux.HandleFunc("POST /api/dev/day", post(s.apiDevDay))
	}
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /static/", web.Static(s.dist, s.assets))
	mux.HandleFunc("GET /", s.notFound)

	return web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		web.SameOrigin(),
		// No s-maxage, since Cloudflare reads it as never serve stale and that
		// turns off stale-if-error, which is what covers the tunnel dropping.
		web.EdgeCache("public, max-age=300, "+
			"stale-while-revalidate=86400, stale-if-error=604800"),
	)
}

type page struct {
	Share       *shown
	Describe    string
	Image       string
	Words       []string
	KeyURL      string
	FrogName    string
	Title       string
	Description string
	SiteName    string
	SourceURL   string
	AnalyticsID string
	Script      string
	Styles      []string
	Year        int
}

func (s *site) page(title, description string) page {
	return s.pageFor("src/index.js", title, description)
}

func (s *site) pageFor(entry, title, description string) page {
	return page{
		Title:       title,
		Description: description,
		SiteName:    siteName,
		SourceURL:   sourceURL,
		AnalyticsID: analyticsID,
		Script:      s.assets.Script(entry),
		Styles:      s.assets.Styles("src/style.js"),
		Year:        time.Now().UTC().Year(),
	}
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	s.renderer.Render(w, http.StatusOK, "index.html", s.page("Home", siteDesc))
}

// keyPage shows the key the cookie holds, so it is never cached and never
// sends a referrer. With no pond it is only the box to bring one back.
func (s *site) keyPage(w http.ResponseWriter, r *http.Request) {
	p := s.pageFor("src/key.js", "Pond key", "The six words that open your pond.")
	p.AnalyticsID = ""
	if id, key, err := s.pondOf(r); err == nil {
		if pond, err := loadPond(s.store.r, id); err == nil {
			p.Words = strings.Split(key, "-")
			p.KeyURL = baseURL + "/k#" + key
			p.FrogName = titled(newGenome(pond.Seed).Name)
			if err := s.store.keySeen(id); err != nil {
				slog.Error("pond", slog.String("path", r.URL.Path), slog.Any("err", err))
			}
		}
	}
	w.Header().Set("Content-Security-Policy", keyCSP())
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	// The one page that asks for the camera, to scan a pond key's QR code.
	w.Header().Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=(), interest-cohort=()")
	s.renderer.Render(w, http.StatusOK, "key.html", p)
}

// keyLanding is where a scanned QR code arrives with the key in the fragment,
// which no server sees. Its script posts the key and wipes it from the bar.
func (s *site) keyLanding(w http.ResponseWriter, r *http.Request) {
	p := s.pageFor("src/k.js", "Bringing a frog back", "Opening a pond from its key.")
	p.AnalyticsID = ""
	w.Header().Set("Content-Security-Policy", keyCSP())
	w.Header().Set("Referrer-Policy", "no-referrer")
	s.renderer.Render(w, http.StatusOK, "k.html", p)
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderer.Render(w, http.StatusNotFound, "notfound.html",
		s.page("Not Found", "That page does not exist."))
}

func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow:\n"))
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Without this EdgeCache fills in the site policy and the edge answers a
	// liveness check out of cache long after this process has stopped.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}
