// Command auth is the front door for every bythewood.me site: one account, a
// six digit code pushed over ntfy, and an opaque session the other sites check
// against this process rather than verifying for themselves.
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

	"auth.bythewood.me/web"
)

// Templates are source, so they ship in the binary unconditionally; the Vite
// bundle is build output and only embeds in a release build.
//
//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

// allPages is shared with the tests, which render every one of them against a
// real database: a template naming a field that does not exist fails at execute
// time rather than at parse time, so nothing but rendering it catches that.
var allPages = []string{
	"home.html", "login.html", "code.html", "recovery.html",
	"account.html", "sessions.html", "security.html", "codes.html",
	"activity.html", "uninitialized.html", "error.html", "notfound.html",
}

func templateSub() (fs.FS, error) { return fs.Sub(templateFS, "templates") }

func dir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

// csp is tighter than the other sites here because this one has no analytics
// snippet and loads nothing from anywhere else. 'unsafe-inline' stays for
// Bootstrap's inline style attributes and covers styles alone.
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
	renderer *web.Renderer
	db       *sql.DB
	dist     fs.FS
	assets   *web.Assets
	notifier *Notifier

	baseScript string
	baseStyles []string
}

func main() {
	web.SetupLogging()

	initialize := flag.Bool("init", false, "seed the account and print its recovery codes, then exit")
	check := flag.Bool("check", false, "report whether the account exists and how many recovery codes are left, then exit")
	recovery := flag.Bool("recovery", false, "replace the recovery codes and print them, then exit")
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

	dataDir := dir("SITE_DATA", "data")
	db, err := openDB(dataDir + "/db.sqlite3")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer db.Close()

	if *initialize {
		if err := runInit(db); err != nil {
			slog.Error(fmt.Sprintf("init: %v", err))
			os.Exit(1)
		}
		return
	}

	if *recovery {
		if err := runRecovery(db); err != nil {
			slog.Error(fmt.Sprintf("recovery: %v", err))
			os.Exit(1)
		}
		return
	}

	if *check {
		if err := runCheck(db); err != nil {
			slog.Error(fmt.Sprintf("check: %v", err))
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

	templates, err := templateSub()
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	renderer, err := web.NewRenderer(templates, templateFuncs,
		[]string{"base.html", "partials.html"}, allPages)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	s := &site{
		renderer:   renderer,
		db:         db,
		dist:       dist,
		assets:     assets,
		notifier:   NewNotifier(),
		baseScript: assets.Script("static_src/base/index.js"),
		baseStyles: assets.Styles("static_src/base/index.js"),
	}

	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

	sweepCtx, stopSweeping := context.WithCancel(context.Background())
	defer stopSweeping()
	go runSweeper(sweepCtx, db)

	handler := s.handler()

	slog.Info(fmt.Sprintf("auth serving %s (staging=%t, cookie domain=%q)",
		baseURL, Staging, cookieDomain()))
	if err := web.Serve(listenAddr, handler); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

// noStore is applied to every authenticated page. These carry addresses, user
// agents and session times, and a zone-wide Cloudflare cache rule added later
// would otherwise make them eligible.
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

// runSweeper keeps the four tables that only grow from doing so.
func runSweeper(ctx context.Context, db *sql.DB) {
	sweep := func() {
		for _, f := range []func(*sql.DB) error{sweepSessions, sweepPending, sweepEvents} {
			if err := f(db); err != nil {
				slog.Error("sweep failed", slog.String("component", "auth"), slog.Any("err", err))
			}
		}
	}
	sweep()

	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}

// healthz stays shallow: a corrupt database is not fixed by the restart a
// failing check would trigger.
func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// EdgeCache is not in this site's chain, but saying nothing here would
	// still let a Cloudflare cache rule answer a liveness check long after this
	// process stopped serving.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}

// handler wires every route. It is a method so the tests drive the real mux
// rather than a second copy of it that can drift.
func (s *site) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.landing)

	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("GET /code", s.codeForm)
	mux.HandleFunc("POST /code", s.codeSubmit)
	mux.HandleFunc("GET /recovery", s.recoveryForm)
	mux.HandleFunc("POST /recovery", s.recoverySubmit)
	mux.HandleFunc("POST /logout", s.logout)

	mux.HandleFunc("GET /account", noStore(s.requireAuth(s.account)))
	mux.HandleFunc("POST /account/username", s.requireSudo(s.changeUsername))
	mux.HandleFunc("GET /sessions", noStore(s.requireAuth(s.sessions)))
	mux.HandleFunc("POST /sessions/revoke", s.requireAuth(s.revoke))
	mux.HandleFunc("POST /sessions/revoke-others", s.requireAuth(s.revokeOthers))
	mux.HandleFunc("GET /security", noStore(s.requireAuth(s.security)))
	mux.HandleFunc("POST /security/recovery", s.requireSudo(s.rotateRecovery))
	mux.HandleFunc("GET /activity", noStore(s.requireAuth(s.activity)))

	// Unauthenticated by design and reachable only over the bridge: Caddy
	// refuses /verify on the public hostname. The cookie in the request is the
	// credential, and this only says whether it is live.
	mux.HandleFunc("GET /verify", s.verify)

	// The mux answers 405 itself only when nothing else matches, and the
	// "GET /" catch-all below matches every GET path there is.
	for path, allow := range map[string]string{
		"/logout":                 "POST",
		"/account/username":       "POST",
		"/sessions/revoke":        "POST",
		"/sessions/revoke-others": "POST",
		"/security/recovery":      "POST",
	} {
		mux.HandleFunc("GET "+path, methodNotAllowed(allow))
	}

	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /robots.txt", robots)

	mux.Handle("GET /static/", web.Static(s.dist, s.assets))

	mux.HandleFunc("GET /healthz", healthz)

	mux.HandleFunc("GET /", s.notFound)

	return web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
	)
}
