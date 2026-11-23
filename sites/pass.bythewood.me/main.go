// pass.bythewood.me is the page beside a Vaultwarden server. Vaultwarden
// answers the Bitwarden apps, and this reads its database to show who signed
// in, from where and on what, and pushes anything that looks wrong.
package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"pass.bythewood.me/ntfy"
	"pass.bythewood.me/suite"
	"pass.bythewood.me/web"
)

//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' https://analytics.bythewood.me/static/collector.js",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self' https://analytics.bythewood.me https://pond.bythewood.me",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

type site struct {
	tpl   *template.Template
	vault *Vault
	watch *watcher
	auth  *web.Authenticator
	// signups mirrors Vaultwarden's own setting, which it doesn't report.
	signups  bool
	webVault bool
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	web.SetupLogging()

	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	probe := flag.String("probe", "", "GET this url and exit 0 on a 200, for Vaultwarden's container")
	flag.Parse()
	if *healthcheck || *probe != "" {
		url := "http://127.0.0.1:8000/healthz"
		if *probe != "" {
			url = *probe
		}
		if err := web.HealthCheck(url, 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

	vault, err := OpenVault(env("PASS_VAULT", "data/db.sqlite3"))
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer vault.Close()

	var push pusher
	if c := ntfy.New("alerts", os.Getenv("NTFY_TOKEN")); c.Configured() {
		push = c
	} else {
		slog.Warn("NTFY_TOKEN is unset, so failed sign ins and new devices are logged and never pushed")
	}

	s, err := newSite(vault, web.NewAuthenticator(), push)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	s.signups = os.Getenv("PASS_SIGNUPS") == "true"
	s.webVault = os.Getenv("PASS_WEB_VAULT") == "true"
	go s.watch.run()

	slog.Info(fmt.Sprintf("%s serving %s", selfSource, baseURL))
	if err := web.Serve(listenAddr, s.handler()); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func newSite(vault *Vault, auth *web.Authenticator, push pusher) (*site, error) {
	tpl, err := template.New("").Funcs(suite.FuncsWith(funcs())).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if tpl, err = suite.Parse(tpl); err != nil {
		return nil, err
	}
	return &site{tpl: tpl, vault: vault, auth: auth, watch: newWatcher(vault, push)}, nil
}

func (s *site) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /logins", s.auth.RequireAuth(s.loginsPage))
	mux.HandleFunc("GET /devices", s.auth.RequireAuth(s.devicesPage))
	mux.Handle("GET /static/", static())
	mux.Handle("GET /suite/state", suite.StateHandler(s.auth.Authenticated, s.suiteState))
	mux.Handle("GET /favicon.svg", suite.Favicon("pass"))
	mux.Handle("GET /favicon.ico", suite.Favicon("pass"))
	mux.Handle("GET /suite/", suite.Handler())
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})
	mux.HandleFunc("GET /", s.notFound)

	return web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		web.SameOrigin(),
		web.EdgeCache("no-store"),
	)
}

func (s *site) render(w http.ResponseWriter, code int, name string, data map[string]any) {
	data["Source"], data["Author"], data["Year"] = sourceURL, author, time.Now().Year()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render", slog.String("page", name), slog.Any("err", err))
	}
}

func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

// healthz is this page answering. Vaultwarden being down shows on the page
// and in status, and shouldn't take the page that says so down with it.
func (s *site) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}
