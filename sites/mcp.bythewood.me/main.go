// mcp.bythewood.me is every orchard tool, read only, as one MCP server, with an
// OAuth front that signs an agent in through auth.bythewood.me.
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

	"mcp.bythewood.me/suite"
	"mcp.bythewood.me/web"
)

//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self' https://pond.bythewood.me",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

type site struct {
	tpl    *template.Template
	store  *Store
	oauth  *oauth
	auth   *web.Authenticator
	bridge *http.Client
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

	store, err := OpenStore(env("MCP_DB", "data/mcp.db"))
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer store.Close()

	s, err := newSite(store, web.NewAuthenticator())
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	slog.Info(fmt.Sprintf("%s serving %s", selfSource, baseURL))
	if err := web.Serve(listenAddr, s.handler()); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func newSite(store *Store, auth *web.Authenticator) (*site, error) {
	tpl, err := template.New("").Funcs(suite.FuncsWith(template.FuncMap{"ago": ago})).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if tpl, err = suite.Parse(tpl); err != nil {
		return nil, err
	}
	return &site{tpl: tpl, store: store, oauth: newOAuth(store), auth: auth, bridge: &http.Client{
		Timeout: 30 * time.Second,
		// A tool that wants a sign in answers with a redirect to auth, which is
		// the signal the session is gone and not somewhere to go.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (s *site) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("POST /mcp", s.serveMCP)
	mux.HandleFunc("GET /mcp", methodNotAllowed)
	mux.HandleFunc("DELETE /mcp", methodNotAllowed)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", protectedResource)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", protectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", authServerMetadata)
	mux.HandleFunc("GET /.well-known/openid-configuration", authServerMetadata)
	mux.HandleFunc("GET /authorize", s.oauth.authorize)
	mux.HandleFunc("GET /oauth/back", s.oauth.back)
	mux.HandleFunc("POST /token", s.oauth.token)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /suite/", suite.Handler())
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})
	mux.HandleFunc("GET /", s.notFound)

	return web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		web.SameOrigin("/mcp", "/token"),
		web.EdgeCache("no-store"),
	)
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "POST")
	http.Error(w, "POST JSON-RPC here", http.StatusMethodNotAllowed)
}

func (s *site) chrome(r *http.Request) (suite.Page, bool) {
	in := s.auth.Authenticated(r)
	return suite.Page{Tool: "mcp", SignedIn: in, Source: sourceURL, Author: author, Year: time.Now().Year(),
		Tabs: []suite.Tab{{Name: "connector", Href: "/", On: r.URL.Path == "/"}, {Name: "source", Href: sourceURL}},
		Meta: "read only · " + resourceURL, Right: []suite.Seg{{Text: "times are UTC"}}}, in
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	page, in := s.chrome(r)
	var grants []Grant
	if in {
		grants, _ = s.store.Grants()
	}
	page.Status = []suite.Seg{{Text: fmt.Sprintf("%d tools", len(tools))}}
	if in {
		page.Status = append(page.Status, suite.Seg{Text: fmt.Sprintf("● %d connected", len(grants)), Kind: "ok"})
	}
	s.render(w, 200, "home.html", map[string]any{"Suite": page, "SignedIn": in, "Grants": grants, "Tools": tools, "Resource": resourceURL})
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r)
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, http.StatusNotFound, "notfound.html", map[string]any{"Suite": page})
}

func (s *site) render(w http.ResponseWriter, code int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render", slog.String("page", name), slog.Any("err", err))
	}
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}
