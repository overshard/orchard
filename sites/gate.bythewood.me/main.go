// gate.bythewood.me is the firewall in front of every site. Caddy asks it about
// each request before handing it on, and its rules change while it runs.
package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"gate.bythewood.me/suite"
	"gate.bythewood.me/web"
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
	tpl    *template.Template
	store  *Store
	engine *engine
	geo    *Geo
	auth   *web.Authenticator
	shadow shadowCache
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
	flag.Parse()
	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

	dataDir := env("GATE_DATA", "data")
	store, err := OpenStore(dataDir + "/gate.db")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer store.Close()
	seed(store)

	geo := OpenGeo(dataDir)
	s, err := newSite(store, geo, web.NewAuthenticator())
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	go func() {
		for {
			geo.refresh()
			s.engine.bots.refresh()
			time.Sleep(24 * time.Hour)
		}
	}()
	go func() {
		for range time.Tick(15 * time.Second) {
			p := s.engine.stats.take()
			if err := store.Flush(p); err != nil {
				s.engine.stats.putBack(p)
				slog.Error("flushing counts", slog.Any("err", err))
			}
		}
	}()
	go func() {
		for range time.Tick(30 * time.Second) {
			if s.store.Setting("mode") != modeOn && s.engine.Mode() == modeOn {
				_ = s.reload()
			}
		}
	}()
	go func() {
		for {
			now := time.Now()
			s.engine.sweep(now)
			store.Sweep(now)
			s.reload()
			time.Sleep(10 * time.Minute)
		}
	}()

	slog.Info(fmt.Sprintf("%s serving %s", selfSource, baseURL))
	if err := web.Serve(listenAddr, s.handler()); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func newSite(store *Store, geo *Geo, auth *web.Authenticator) (*site, error) {
	tpl, err := template.New("").Funcs(suite.FuncsWith(funcs())).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if tpl, err = suite.Parse(tpl); err != nil {
		return nil, err
	}
	challengeTpl = tpl
	secret, err := store.Secret()
	if err != nil {
		return nil, err
	}
	s := &site{tpl: tpl, store: store, geo: geo, auth: auth, engine: newEngine(geo, auth, secret, newStats())}
	if err := s.reload(); err != nil {
		return nil, err
	}
	bans, _ := store.Bans()
	s.engine.setBans(bans)
	return s, nil
}

// reload hands the engine the rules, the mode and the box's own addresses as
// the database has them now. Every change calls it, and so does a timer, so
// an expired rule drops out of the table without anyone touching it.
func (s *site) reload() error {
	rules, err := s.store.Rules()
	if err != nil {
		return err
	}
	s.engine.load(rules)
	mode, until := s.modeSetting(time.Now())
	s.engine.until.Store(until)
	s.engine.mode.Store(mode)
	s.engine.setSelf(selfAddrs(s.store.Setting("self")))
	return nil
}

func selfAddrs(raw string) []netip.Prefix {
	var out []netip.Prefix
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if p, err := netip.ParsePrefix(f); err == nil {
			out = append(out, p.Masked())
		} else if a, err := netip.ParseAddr(f); err == nil {
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
		}
	}
	return out
}

// handler sends /check straight to the engine and everything else through the
// usual middleware. A log line per check would double every request in logging.
func (s *site) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /rules", s.auth.RequireAuth(s.rulesPage))
	mux.HandleFunc("POST /rules", s.auth.RequireAuth(s.addRule))
	mux.HandleFunc("GET /rules/new", s.auth.RequireAuth(s.newRulePage))
	mux.HandleFunc("GET /rules/{id}", s.auth.RequireAuth(s.rulePage))
	mux.HandleFunc("POST /rules/{id}", s.auth.RequireAuth(s.editRule))
	mux.HandleFunc("POST /rules/{id}/{op}", s.auth.RequireAuth(s.ruleOp))
	mux.HandleFunc("POST /rules/test", s.auth.RequireAuth(s.testRulePage))
	mux.HandleFunc("POST /changes/{id}/restore", s.auth.RequireAuth(s.restore))
	mux.HandleFunc("POST /held/{id}/{op}", s.auth.RequireAuth(s.heldOp))
	mux.HandleFunc("GET /traffic", s.auth.RequireAuth(s.trafficPage))
	mux.HandleFunc("GET /bans", s.auth.RequireAuth(s.bansPage))
	mux.HandleFunc("POST /bans", s.auth.RequireAuth(s.addBan))
	mux.HandleFunc("POST /bans/lift", s.auth.RequireAuth(s.liftBan))
	mux.HandleFunc("GET /settings", s.auth.RequireAuth(s.settingsPage))
	mux.HandleFunc("POST /settings", s.auth.RequireAuth(s.saveSettings))
	mux.HandleFunc("GET /docs", s.docs)
	mux.HandleFunc("GET /api/report", s.auth.RequireAuthJSON(s.apiReport))
	mux.HandleFunc("GET /api/traffic", s.auth.RequireAuthJSON(s.apiTraffic))
	mux.HandleFunc("POST /api/propose", s.auth.RequireAuthJSONConnector(s.apiPropose))
	mux.HandleFunc("POST /api/edit", s.auth.RequireAuthJSONConnector(s.apiEdit))
	mux.Handle("GET /static/", static())
	mux.Handle("GET /suite/state", suite.StateHandler(s.auth.Authenticated, s.suiteState))
	mux.Handle("GET /favicon.svg", suite.Favicon("gate"))
	mux.Handle("GET /favicon.ico", suite.Favicon("gate"))
	mux.Handle("GET /suite/", suite.Handler())
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})
	mux.HandleFunc("GET /", s.notFound)

	site := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		web.SameOrigin(),
		web.EdgeCache("no-store"),
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/check":
			s.engine.check(w, r)
		case r.URL.Path == "/__gate/solve" && r.Method == http.MethodPost:
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			web.SameOrigin()(http.HandlerFunc(s.engine.solve)).ServeHTTP(w, r)
		case r.URL.Path == "/__gate/pow.js":
			powScript(w, r)
		case strings.HasPrefix(r.URL.Path, "/__gate/"):
			plain(w, http.StatusNotFound, "404 page not found")
		default:
			site.ServeHTTP(w, r)
		}
	})
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

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}
