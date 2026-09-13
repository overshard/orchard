// house.bythewood.me: a house hunting dashboard. It takes listings from an MLS
// export, hard filters the ones that break a dealbreaker, scores the rest against
// the household's actual daily driving, and shows the survivors photo first.
//
// Unlike every other site in this repo there is no public half. Every route is
// behind the session, because what is on these pages is an address, a budget and
// a child's school run.
package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"house.bythewood.me/web"
)

//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

func dir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

// img-src allows data: for the inline SVG placeholders a card without photos
// shows. Everything else is same origin, which the photo proxy is what makes
// possible: no listing CDN is ever in the page.
func csp() string {
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

type site struct {
	renderer  *web.Renderer
	db        *sql.DB
	dist      fs.FS
	assets    *web.Assets
	auth      *web.Authenticator
	cfg       Config
	photos    *Photos
	refresher *Refresher

	// One refresh at a time. Two concurrent runs would double every external
	// request and race each other writing the same facts rows.
}

var (
	layoutTemplates = []string{"base.html", "partials.html"}
	pageTemplates   = []string{"grid.html", "detail.html", "notfound.html"}
)

// A refresh paces itself against six external services, so the ceiling is
// generous. It is a ceiling and not a target: a run over a normal export
// finishes in minutes.
func contextWithTimeout() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return ctx
}

// startStubVerifier answers every verify call yes, on loopback only. It exists so
// -dev-open goes through the same gate as production rather than adding a branch
// to every handler, and it binds 127.0.0.1 so nothing off the machine can reach
// the thing that is saying yes.
func startStubVerifier() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	go func() {
		_ = http.Serve(ln, mux)
	}()
	return "http://" + ln.Addr().String() + "/verify", nil
}

func main() {
	web.SetupLogging()

	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	refreshOnce := flag.Bool("refresh", false, "run one refresh, print what happened, and exit")
	// Every route here is behind the session, and the verifier is another
	// container on the bridge, so a dev run on a laptop can see nothing at all.
	// This is the way in, and it is a flag rather than an environment variable so
	// it cannot arrive by accident: the Dockerfile's entrypoint is the bare binary
	// and compose adds no arguments.
	openLocal := flag.Bool("dev-open", false, "skip the session check, for a local dev run only")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	shipper := web.ShipLogs("house", web.HTTPSink())
	defer shipper.Close()

	dataDir := dir("SITE_DATA", "data")
	db, err := openDB(dataDir + "/db.sqlite3")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer db.Close()

	cfg, err := LoadConfig(dataDir + "/config.json")
	if err != nil {
		// Not fatal. A bad config file means the defaults, which every page says
		// it is using, and a dashboard that will not start is harder to fix.
		slog.Error("config not loaded, using defaults", slog.Any("err", err))
	}
	if cfg.Defaulted {
		slog.Warn("no data/config.json, running on defaults with no office to measure from")
	}

	refresher := NewRefresher(db, cfg, dataDir)

	if *refreshOnce {
		res, err := refresher.Run(contextWithTimeout())
		if err != nil {
			slog.Error("refresh failed", slog.Any("err", err))
			os.Exit(1)
		}
		fmt.Printf("seen %d, added %d, repriced %d, scored %d, excluded %d\n",
			res.Seen, res.Added, res.Repriced, res.Scored, res.Excluded)
		for _, e := range res.Errors {
			fmt.Println("  " + e)
		}
		return
	}

	dist := distFS()
	assets, err := web.LoadAssets(dist)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	watchAssets(assets, dist)

	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	renderer, err := web.NewRenderer(templates, templateFuncs, layoutTemplates, pageTemplates)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	auth := web.NewAuthenticator()
	if *openLocal {
		verify, err := startStubVerifier()
		if err != nil {
			slog.Error("startup failed", slog.Any("err", err))
			os.Exit(1)
		}
		auth = web.NewAuthenticatorAt(verify)
		slog.Warn("running with -dev-open, every page is served with no session check")
	}

	// The gate short circuits on a missing cookie before it ever asks the
	// verifier, so a stub that says yes is not enough on its own: a dev run has to
	// arrive carrying a cookie too.
	devCookie := func(next http.Handler) http.Handler { return next }
	if *openLocal {
		devCookie = func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := r.Cookie(web.SessionCookie); err != nil {
					r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "dev-open"})
				}
				next.ServeHTTP(w, r)
			})
		}
	}

	s := &site{
		renderer:  renderer,
		db:        db,
		dist:      dist,
		assets:    assets,
		auth:      auth,
		cfg:       cfg,
		photos:    NewPhotos(db, dataDir),
		refresher: refresher,
	}
	// Zero, so the first refresh after a start is never on cooldown.

	mux := http.NewServeMux()

	// Every page, behind the session. There is no signed out view of anything
	// here except the redirect to auth.
	// One report is shareable and everything else is not. A link carries an opaque
	// token rather than a row number, so handing somebody a house to look at does
	// not hand them the list, the budget behind the list, or a way to count up
	// from one and read the rest.
	//
	// Reading one report, its photographs and its progress needs no session. The
	// list, checking a new address, the thumbs and the delete all do.
	mux.HandleFunc("GET /{$}", s.auth.RequireAuth(s.grid))
	mux.HandleFunc("GET /listing/{id}", s.detail)
	mux.HandleFunc("GET /photo/{id}/{idx}", s.photo)
	mux.HandleFunc("GET /listing/{id}/status", s.status)
	mux.HandleFunc("POST /listing/{id}/verdict", s.auth.RequireAuthJSON(s.verdict))
	mux.HandleFunc("POST /listing/{id}/delete", s.auth.RequireAuthJSON(s.remove))
	mux.HandleFunc("POST /check", s.auth.RequireAuth(s.check))

	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})

	mux.Handle("GET /static/", web.Static(dist, assets))

	// Nothing here is ever indexed, so this is a blanket refusal rather than a
	// list of paths.
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// EdgeCache fills in a site policy whenever a handler sets none, so
		// saying nothing here means the edge answers a liveness check out of
		// cache long after this process stopped serving.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /", s.auth.RequireAuth(s.notFound))

	handler := web.Chain(mux,
		devCookie,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
	)

	// Reports that came up short finish themselves. Nothing on a page should ever
	// ask somebody to press a button because a county server was busy.
	mendCtx, stopMending := context.WithCancel(context.Background())
	defer stopMending()
	go NewMender(db, refresher).Run(mendCtx)

	slog.Info(fmt.Sprintf("house serving %s (staging=%t, config=%s)", baseURL, Staging, cfg.Label))
	if err := web.Serve(listenAddr, handler); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}
