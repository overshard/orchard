// llm.bythewood.me, the model gateway.
//
// One model on one card, in front of every service that wants one. Before this,
// chat and search each carried their own llama-swap and their own copy of the
// weights, which on an 8GB card means whichever one you used last evicted the
// other. This holds the model, hands out API keys, and writes down every prompt
// and every completion that passes through it.
//
// The web half is behind auth.bythewood.me like the other dashboards. The API
// half is behind a key, because the callers are containers with no browser.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"llm.bythewood.me/web"
)

const (
	listenAddr = ":8000"
	sourceURL  = "https://github.com/overshard/orchard/tree/main/sites/llm.bythewood.me"
	selfSource = "llm"
)

// csp is the same shape as every other site here. 'unsafe-inline' is for the
// analytics collector snippet, which is an inline script by design, and for the
// handful of inline style attributes the templates set.
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
		"frame-ancestors 'none'",
	}, "; ")
}

type site struct {
	auth     *web.Authenticator
	store    *Store
	tpl      *template.Template
	client   *http.Client
	upstream string
	model    string
	retain   time.Duration
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	var (
		addr     = flag.String("addr", env("LLM_ADDR", listenAddr), "listen address")
		upstream = flag.String("upstream", env("LLM_UPSTREAM", "http://swap:8091"), "llama-swap base url")
		model    = flag.String("model", env("LLM_MODEL", "local"), "the model name callers ask for")
		dbPath   = flag.String("db", env("LLM_DB", "data/llm.db"), "database path")
		retain   = flag.Duration("retain", 90*24*time.Hour, "how long a logged call is kept")
		check    = flag.Bool("healthcheck", false, "probe this process and exit")
		newKey   = flag.String("newkey", "", "mint an api key with this name, print it, and exit")
	)
	flag.Parse()

	if *check {
		if err := web.HealthCheck("http://127.0.0.1"+*addr+"/healthz", 3*time.Second); err != nil {
			os.Exit(1)
		}
		return
	}

	web.SetupLogging()
	web.ShipLogs(selfSource, web.HTTPSink())

	store, err := OpenStore(*dbPath)
	if err != nil {
		slog.Error("opening the database", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	// The first key cannot come from the web UI, because that is behind auth
	// and auth is reached over the tunnel this gateway is meant to be feeding.
	// Same shape as auth's own recovery codes: a flag, printed once.
	if *newKey != "" {
		secret, _, err := store.NewKey(*newKey)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(secret)
		store.Checkpoint()
		return
	}

	s := &site{
		auth:     web.NewAuthenticator(),
		store:    store,
		upstream: strings.TrimSuffix(*upstream, "/"),
		model:    *model,
		retain:   *retain,
		// No timeout on the client. A cold model load plus a long generation
		// runs well past any value worth picking, and the caller's own context
		// is what bounds this instead.
		client: &http.Client{},
	}
	if err := s.loadTemplates(); err != nil {
		slog.Error("templates", "err", err)
		os.Exit(1)
	}
	go s.prune()

	mux := http.NewServeMux()

	// The dashboard. Behind auth like every other one here.
	// The root is the one public page: what this is and a way in. A signed in
	// visitor gets the dashboard here rather than the pitch.
	mux.HandleFunc("GET /{$}", s.root)
	mux.HandleFunc("GET /calls", s.auth.RequireAuth(s.callsPage))
	mux.Handle("GET /static/", s.static())
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})
	mux.HandleFunc("POST /keys", s.auth.RequireAuth(s.keyCreate))
	mux.HandleFunc("POST /keys/{id}/revoke", s.auth.RequireAuth(s.keyRevoke))
	mux.HandleFunc("POST /keys/{id}/delete", s.auth.RequireAuth(s.keyDelete))

	// The gateway. A key, not a cookie, because the callers are containers.
	mux.HandleFunc("POST /v1/chat/completions", s.requireKey(s.completions))
	mux.HandleFunc("POST /v1/completions", s.requireKey(s.completions))
	mux.HandleFunc("GET /v1/models", s.requireKey(s.passthrough))
	mux.HandleFunc("POST /v1/embeddings", s.requireKey(s.passthrough))

	// Unkeyed, and it says nothing but whether this process is up. Asking the
	// upstream here would wake the weights every thirty seconds and defeat the
	// idle unload this whole service exists to make possible.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintln(w, "ok")
	})

	slog.Info("llm listening", "addr", *addr, "upstream", s.upstream, "model", s.model)
	web.Serve(*addr, web.Chain(mux, web.Recovered, web.Logged, web.SecurityHeaders(csp())))
}

// prune trims the call log on a timer. The prompts are the whole of what
// anybody asked this estate, so they age out rather than accumulating until the
// volume fills.
func (s *site) prune() {
	for {
		if n, err := s.store.Prune(s.retain); err != nil {
			slog.Error("pruning the call log", "err", err)
		} else if n > 0 {
			slog.Info("pruned the call log", "rows", n, "keep", s.retain.String())
		}
		time.Sleep(6 * time.Hour)
	}
}

func (s *site) loadTemplates() error {
	// Reading assets off disk means their hashes change while the process is
	// up, so the reload path drops the cached versions with the templates.
	if Reloaded {
		resetAssetVersions()
	}
	t, err := template.New("").Funcs(template.FuncMap{
		"when":  fmtWhen,
		"short": short,
		"comma": comma,
		"asset": assetURL,
	}).ParseFS(assets(), "templates/*.html")
	if err != nil {
		return err
	}
	s.tpl = t
	return nil
}

func (s *site) static() http.Handler {
	fs := http.FileServerFS(assets())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Reloaded {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fs.ServeHTTP(w, r)
	})
}

func (s *site) render(w http.ResponseWriter, name string, data map[string]any) {
	if Reloaded {
		if err := s.loadTemplates(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	data["Source"] = sourceURL
	data["Model"] = s.model
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render", "name", name, "err", err)
	}
}

func (s *site) root(w http.ResponseWriter, r *http.Request) {
	if s.auth.Authenticated(r) {
		s.overview(w, r)
		return
	}
	s.render(w, "landing.html", map[string]any{"Title": "Overview"})
}

func (s *site) overview(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.Keys()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	usage, _ := s.store.Usage(time.Now().Add(-24 * time.Hour))
	recent, _ := s.store.Calls("", 12)
	nKeys, nCalls := s.store.Counts()

	// A key is shown once, on the redirect straight after it is made, because
	// nothing here can produce it again.
	fresh := r.URL.Query().Get("key")

	s.render(w, "overview.html", map[string]any{
		"Keys": keys, "Usage": usage, "Recent": recent,
		"Fresh": fresh, "NKeys": nKeys, "NCalls": nCalls, "Title": "Overview",
		"Upstream": s.upstream, "Retain": s.retain,
	})
}

func (s *site) callsPage(w http.ResponseWriter, r *http.Request) {
	caller := r.URL.Query().Get("caller")
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	calls, err := s.store.Calls(caller, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "calls.html", map[string]any{"Calls": calls, "Caller": caller, "Limit": limit, "Title": "Calls"})
}

func (s *site) keyCreate(w http.ResponseWriter, r *http.Request) {
	secret, _, err := s.store.NewKey(r.FormValue("name"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	// Carried in the query rather than rendered here so a refresh of the
	// resulting page does not mint a second key.
	http.Redirect(w, r, "/?key="+secret, http.StatusSeeOther)
}

func (s *site) keyRevoke(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.store.Revoke(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *site) keyDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.store.DeleteKey(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func fmtWhen(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Format("2 Jan")
}

// short renders the messages array as something readable in a table cell. The
// whole of it is still in the database and on the call's own row.
func short(s string, n int) string {
	var msgs []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	text := s
	if json.Unmarshal([]byte(s), &msgs) == nil && len(msgs) > 0 {
		parts := make([]string, 0, len(msgs))
		for _, m := range msgs {
			parts = append(parts, m.Role+": "+m.Content)
		}
		text = strings.Join(parts, " | ")
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= n {
		return text
	}
	return text[:n] + "..."
}

func comma(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
