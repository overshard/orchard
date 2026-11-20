// dns.bythewood.me is a private resolver for my own devices. It blocks ads,
// trackers and malware from lists, logs every lookup per device, and asks
// unbound for everything it lets through.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"dns.bythewood.me/suite"
	"dns.bythewood.me/web"
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
		"connect-src 'self' https://analytics.bythewood.me https://pond.bythewood.me https://*." + probeZone,
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

type site struct {
	tpl   *template.Template
	store *Store
	lists *lister
	res   *resolver
	log   *logger
	certs *certs
	auth  *web.Authenticator

	seenMu sync.Mutex
	seen   map[int64]Lookup
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
	dot := flag.Bool("dot", false, "run the DNS over TLS front instead of the site")
	acmeMode := flag.Bool("acme", false, "keep the DoT certificate instead of running the site")
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

	tlsDir := env("DNS_TLS", "data/tls")
	if *acmeMode {
		runACME(tlsDir, env("DNS_ACME", "data/acme"))
		return
	}
	if *dot {
		if err := runFront(tlsDir); err != nil {
			slog.Error("dot front stopped", slog.Any("err", err))
			os.Exit(1)
		}
		return
	}

	dataDir := env("DNS_DATA", "data")
	store, err := OpenStore(dataDir + "/dns.db")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer store.Close()

	s, err := newSite(store, dataDir, tlsDir, web.NewAuthenticator())
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	s.lists.load()
	go s.res.watchUpstream()
	go s.log.run(s.noteSeen)
	go func() {
		for {
			s.lists.refresh(context.Background())
			time.Sleep(12 * time.Hour)
		}
	}()
	go func() {
		for range time.Tick(30 * time.Second) {
			s.flushSeen()
		}
	}()
	go func() {
		for {
			store.Sweep(time.Now())
			time.Sleep(time.Hour)
		}
	}()

	slog.Info(fmt.Sprintf("%s serving %s", selfSource, baseURL))
	if err := web.Serve(listenAddr, s.handler()); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func newSite(store *Store, dataDir, tlsDir string, auth *web.Authenticator) (*site, error) {
	tpl, err := template.New("").Funcs(suite.FuncsWith(funcs())).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if tpl, err = suite.Parse(tpl); err != nil {
		return nil, err
	}
	lg := newLogger(store)
	l := newLister(dataDir+"/lists", store)
	s := &site{tpl: tpl, store: store, lists: l, log: lg, auth: auth,
		res:   newResolver(l, env("DNS_UPSTREAM", upstreamAddr), lg),
		certs: newCerts(tlsDir, "", ""),
		seen:  map[int64]Lookup{},
	}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// reload hands the resolver the devices and rules as the database has them.
// Every change calls it.
func (s *site) reload() error {
	ds, err := s.store.Devices()
	if err != nil {
		return err
	}
	rs, err := s.store.Rules()
	if err != nil {
		return err
	}
	s.res.setDevices(ds)
	s.res.setRules(rs)
	return nil
}

func (s *site) noteSeen(l Lookup) {
	s.seenMu.Lock()
	s.seen[l.Device] = l
	s.seenMu.Unlock()
}

func (s *site) flushSeen() {
	s.seenMu.Lock()
	seen := s.seen
	s.seen = map[int64]Lookup{}
	s.seenMu.Unlock()
	for id, l := range seen {
		_ = s.store.Seen(id, l.At, l.Proto, l.IP)
	}
	if len(seen) > 0 {
		_ = s.reload()
	}
}

// handler keeps DoH and the bridge routes out of the request log, since a
// line per lookup would bury every other line in logging.
func (s *site) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /log", s.auth.RequireAuth(s.logPage))
	mux.HandleFunc("GET /log/rows", s.auth.RequireAuth(s.logRows))
	mux.HandleFunc("GET /devices", s.auth.RequireAuth(s.devicesPage))
	mux.HandleFunc("POST /devices", s.auth.RequireAuth(s.addDevice))
	mux.HandleFunc("GET /devices/{id}", s.auth.RequireAuth(s.devicePage))
	mux.HandleFunc("POST /devices/{id}", s.auth.RequireAuth(s.editDevice))
	mux.HandleFunc("POST /devices/{id}/{op}", s.auth.RequireAuth(s.deviceOp))
	mux.HandleFunc("GET /lists", s.auth.RequireAuth(s.listsPage))
	mux.HandleFunc("POST /lists/refresh", s.auth.RequireAuth(s.refreshLists))
	mux.HandleFunc("POST /rules", s.auth.RequireAuth(s.addRule))
	mux.HandleFunc("POST /rules/{id}/delete", s.auth.RequireAuth(s.deleteRule))
	mux.HandleFunc("POST /rules/delete", s.auth.RequireAuth(s.deleteRules))
	mux.HandleFunc("GET /devices/{id}/profile", s.auth.RequireAuth(s.appleProfile))
	mux.HandleFunc("GET /check", s.auth.RequireAuth(s.checkPage))
	mux.HandleFunc("GET /probe/{nonce}", s.auth.RequireAuthJSON(s.probe))
	mux.HandleFunc("GET /docs", s.docs)
	mux.HandleFunc("GET /api/report", s.auth.RequireAuthJSON(s.apiReport))
	mux.Handle("GET /static/", static())
	mux.Handle("GET /suite/state", suite.StateHandler(s.auth.Authenticated, s.suiteState))
	mux.Handle("GET /suite/", suite.Handler())
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /healthz", s.healthz)
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
	quiet := http.NewServeMux()
	quiet.HandleFunc("GET /q/{token}", s.doh)
	quiet.HandleFunc("POST /q/{token}", s.doh)
	quiet.HandleFunc("POST /internal/query", s.internalQuery)
	quiet.HandleFunc("GET /internal/token/{token}", s.internalToken)
	bare := web.Chain(quiet, web.Recovered)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/q/") || strings.HasPrefix(r.URL.Path, "/internal/") {
			bare.ServeHTTP(w, r)
			return
		}
		site.ServeHTTP(w, r)
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

// healthz is the app's own state, the database answering and a table built.
// unbound being down isn't here, since the fallback still answers.
func (s *site) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.db.PingContext(ctx); err != nil {
		plainNoStore(w, http.StatusServiceUnavailable, "database: "+err.Error())
		return
	}
	if s.lists.tab.Load() == nil {
		plainNoStore(w, http.StatusServiceUnavailable, "no blocklist table yet")
		return
	}
	healthz(w, r)
}

// runACME keeps the certificate and serves only its own health check.
func runACME(tlsDir, acmeDir string) {
	for _, d := range []string{tlsDir, acmeDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			slog.Error("acme startup failed", slog.Any("err", err))
			os.Exit(1)
		}
	}
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /healthz", healthz)
		_ = http.ListenAndServe("127.0.0.1:8000", mux)
	}()
	newCerts(tlsDir, acmeDir, os.Getenv("CLOUDFLARE_TOKEN")).keep()
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}
