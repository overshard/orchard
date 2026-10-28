// llm.bythewood.me holds one model on one card for every service, since a
// llama-swap per service on an 8GB card evicts whichever was used last. It hands
// out API keys and logs every call unless the caller marks it incognito.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"llm.bythewood.me/web"
)

const (
	listenAddr = ":8000"
	sourceURL  = "https://github.com/overshard/orchard/tree/main/sites/llm.bythewood.me"
	selfSource = "llm"
	author     = "Isaac Bythewood"
)

// 'unsafe-inline' is for the analytics collector snippet, which is an inline
// script, and for the inline style attributes the templates set.
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

type site struct {
	auth     *web.Authenticator
	store    *Store
	tpl      *template.Template
	client   *http.Client
	upstream string
	gpuProbe string
	model    string
	retain   time.Duration
	card     cardGate
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	if err := realMain(); err != nil {
		slog.Error("exiting", slog.Any("err", err))
		os.Exit(1)
	}
}

// realMain returns instead of exiting so the deferred closes and the log
// shipper's flush run on every path out.
func realMain() error {
	var (
		addr     = flag.String("addr", env("LLM_ADDR", listenAddr), "listen address")
		upstream = flag.String("upstream", env("LLM_UPSTREAM", "http://swap:8091"), "llama-swap base url")
		gpuProbe = flag.String("gpu", env("LLM_GPU", "http://gpu:8092"), "gpu probe base url")
		model    = flag.String("model", env("LLM_MODEL", "local"), "the model name callers ask for")
		dbPath   = flag.String("db", env("LLM_DB", "data/llm.db"), "database path")
		retain   = flag.Duration("retain", 90*24*time.Hour, "how long a logged call is kept")
		check    = flag.Bool("healthcheck", false, "probe this process and exit")
		newKey   = flag.String("newkey", "", "mint an api key with this name, print it, and exit")
	)
	flag.Parse()

	if *check {
		if err := web.HealthCheck("http://127.0.0.1"+*addr+"/healthz", 3*time.Second); err != nil {
			return fmt.Errorf("healthcheck: %w", err)
		}
		return nil
	}

	web.SetupLogging()
	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

	store, err := OpenStore(*dbPath)
	if err != nil {
		return fmt.Errorf("opening the database: %w", err)
	}
	defer store.Close()

	// The first key can't come from the web UI, since that's behind auth and auth
	// is reached over the tunnel this gateway feeds, so it's a flag printed once
	// like auth's own recovery codes.
	if *newKey != "" {
		secret, _, err := store.NewKey(*newKey)
		if err != nil {
			return err
		}
		fmt.Println(secret)
		store.Checkpoint()
		return nil
	}

	s := &site{
		auth:     web.NewAuthenticator(),
		store:    store,
		upstream: strings.TrimSuffix(*upstream, "/"),
		gpuProbe: strings.TrimSuffix(*gpuProbe, "/"),
		model:    *model,
		retain:   *retain,
		// No timeout on the client, since a cold model load plus a long generation
		// runs past any value worth picking and the caller's own context bounds it.
		client: &http.Client{},
	}
	s.card.started = time.Now()
	if err := s.loadTemplates(); err != nil {
		return fmt.Errorf("templates: %w", err)
	}
	// Waited on after the cancel so the store isn't closed under a prune.
	pruneCtx, stopPruning := context.WithCancel(context.Background())
	var pruning sync.WaitGroup
	defer pruning.Wait()
	defer stopPruning()
	pruning.Go(func() { s.prune(pruneCtx) })

	mux := http.NewServeMux()

	// The root is the one public page, and a signed in visitor gets the dashboard
	// there instead of the pitch.
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
	mux.HandleFunc("POST /v1/images/generations", s.requireKey(s.images))
	mux.HandleFunc("POST /v1/images/edits", s.requireKey(s.images))
	// What's on the card, and a way to take it off before the idle ttl runs out.
	// Neither loads anything, so a caller can poll the first as often as it likes
	// without keeping the weights awake.
	mux.HandleFunc("GET /v1/running", s.requireKey(s.running))
	mux.HandleFunc("POST /v1/unload", s.requireKey(s.unload))
	// Whether the desktop is using the card, for callers that run on a timer
	// and would rather wait than load a model on top of a game.
	mux.HandleFunc("GET /v1/gpu", s.requireKey(s.gpu))

	// None of this is for a stranger, and without a robots.txt of our own the edge
	// serves a default that restricts nothing. The noindex meta tag needs this
	// too, since a crawler obeying robots.txt never fetches the page.
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
	})

	mux.HandleFunc("GET /healthz", s.healthz)

	slog.Info("llm listening", "addr", *addr, "upstream", s.upstream, "model", s.model)
	return web.Serve(*addr, web.Chain(mux, web.Recovered, web.Logged, web.SecurityHeaders(csp()), web.SameOrigin()))
}

// prune trims the call log on a timer. The prompts are the whole of what
// anybody asked this estate, so they age out instead of piling up until the
// volume fills.
func (s *site) prune(ctx context.Context) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		if n, err := s.store.Prune(s.retain); err != nil {
			slog.Error("pruning the call log", "err", err)
		} else if n > 0 {
			slog.Info("pruned the call log", "rows", n, "keep", s.retain.String())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
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
		"num":   num,
		"asset": assetURL,
	}).ParseFS(assets(), "templates/*.html")
	if err != nil {
		return err
	}
	s.tpl = t
	return nil
}

func (s *site) static() http.Handler {
	fs := web.NoDirs(http.FileServerFS(assets()))
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
	nKeys, nCalls := s.store.Counts()
	s.render(w, "landing.html", map[string]any{
		"Title": "Overview", "Keys": nKeys, "Calls": nCalls,
		"RetainDays": int(s.retain.Hours() / 24), "Author": author, "Year": time.Now().Year(),
	})
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

	// A key is shown once, on the redirect straight after it's made, because
	// nothing here can produce it again.
	fresh := ""
	if c, err := r.Cookie(newKeyCookie); err == nil {
		fresh = c.Value
		http.SetCookie(w, &http.Cookie{
			Name: newKeyCookie, Value: "", Path: "/",
			Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1,
		})
	}

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
	// A one-shot cookie instead of a query string, since a key in a URL lands in
	// the edge access log, a Referer header and browser history.
	http.SetCookie(w, &http.Cookie{
		Name: newKeyCookie, Value: secret, Path: "/",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 60,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// newKeyCookie carries a freshly minted key from the POST to the page that
// shows it, once.
const newKeyCookie = "new_key"

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

// num is comma for templates. It takes any because a template handing it the
// wrong integer type fails at render time, not at build.
func num(v any) string {
	switch t := v.(type) {
	case int:
		return comma(t)
	case int64:
		return comma(int(t))
	}
	return fmt.Sprint(v)
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
