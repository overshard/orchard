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
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"llm.bythewood.me/ntfy"
	"llm.bythewood.me/suite"
	"llm.bythewood.me/web"
)

const (
	listenAddr = ":8000"
	sourceURL  = "https://github.com/overshard/orchard/tree/main/sites/llm.bythewood.me"
	selfSource = "llm"
	author     = "Isaac Bythewood"
)

// style-src keeps 'unsafe-inline' for the style attributes the templates set.
// script-src doesn't, so keep inline script and on* handlers out of templates.
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
	auth     *web.Authenticator
	store    *Store
	tpl      *template.Template
	client   *http.Client
	upstream string
	gpuProbe string
	model    string
	retain   time.Duration
	card     cardGate
	// Set when ANTHROPIC_API_KEY is, and then every completion goes to Claude.
	claude *claude
	budget float64
	limits keyLimiter
	// Keyed requests log their call on the way out, so the store waits for them.
	calls sync.WaitGroup
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
		addKey   = flag.String("addkey", "", "register the key on stdin under this name, and exit")
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
	// The secret comes on stdin so it never shows up in the host's process list.
	if *addKey != "" {
		secret, err := io.ReadAll(io.LimitReader(os.Stdin, 256))
		if err != nil {
			return err
		}
		added, err := store.AddKey(*addKey, string(secret))
		if err != nil {
			return err
		}
		if added {
			fmt.Println("added")
		} else {
			fmt.Println("known")
		}
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
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		s.claude = newClaude(key, env("LLM_CLAUDE_MODEL", defaultClaudeModel))
		s.upstream = "the Anthropic API as " + s.claude.model
	}
	s.budget, _ = strconv.ParseFloat(env("LLM_BUDGET", "100"), 64)
	if err := s.loadTemplates(); err != nil {
		return fmt.Errorf("templates: %w", err)
	}
	defer s.calls.Wait()
	// Waited on after the cancel so the store isn't closed under a prune.
	pruneCtx, stopPruning := context.WithCancel(context.Background())
	var pruning sync.WaitGroup
	defer pruning.Wait()
	defer stopPruning()
	pruning.Go(func() { s.prune(pruneCtx) })
	if n := ntfy.New("alerts", os.Getenv("NTFY_TOKEN")); s.claude != nil && n.Configured() {
		pruning.Go(func() { s.watchSpend(pruneCtx, n) })
	}

	mux := http.NewServeMux()

	// The root is the one public page, and a signed in visitor gets the dashboard
	// there instead of the pitch.
	mux.HandleFunc("GET /{$}", s.root)
	mux.HandleFunc("GET /", s.notFound)
	mux.HandleFunc("GET /calls", s.auth.RequireAuth(s.callsPage))
	mux.Handle("GET /static/", s.static())
	mux.Handle("GET /suite/state", suite.StateHandler(s.auth.Authenticated, s.suiteState))
	mux.Handle("GET /favicon.svg", suite.Favicon("llm"))
	mux.Handle("GET /favicon.ico", suite.Favicon("llm"))
	mux.Handle("GET /suite/", suite.Handler())
	mux.HandleFunc("GET /keys", s.auth.RequireAuth(s.keysPage))
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})
	mux.HandleFunc("POST /keys", s.auth.RequireAuth(s.keyCreate))
	mux.HandleFunc("POST /keys/{id}/revoke", s.auth.RequireAuth(s.keyRevoke))
	mux.HandleFunc("POST /keys/{id}/cap", s.auth.RequireAuth(s.keyCap))
	mux.HandleFunc("POST /keys/{id}/delete", s.auth.RequireAuth(s.keyDelete))

	// The gateway. A key, not a cookie, because the callers are containers.
	mux.HandleFunc("POST /v1/chat/completions", s.requireKey(s.completions))
	mux.HandleFunc("POST /v1/completions", s.requireKey(s.completions))
	mux.HandleFunc("GET /v1/models", s.requireKey(s.passthrough))
	mux.HandleFunc("POST /v1/embeddings", s.requireKey(s.passthrough))
	// Web search for a caller that wants the hits, not an answer.
	mux.HandleFunc("POST /v1/search", s.requireKey(s.search))
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
	t, err := template.New("").Funcs(suite.Funcs()).Funcs(template.FuncMap{
		"when":     fmtWhen,
		"short":    short,
		"comma":    comma,
		"num":      num,
		"asset":    assetURL,
		"secs":     secs,
		"count":    count,
		"pct":      func(a, b float64) float64 { return 100 * a / b },
		"usd":      usd,
		"clock":    clock,
		"clockday": func(t time.Time) string { return t.In(budgetZone).Format("Jan 2 15:04") },
		"sub":      func(a, b float64) float64 { return a - b },
		"mul":      func(a, b float64) float64 { return a * b },
		"int64":    func(v float64) int64 { return int64(v) },
		"compact":  compact,
		"asked":    asked,
		"dollars":  func(micros int64) float64 { return float64(micros) / 1e6 },
	}).ParseFS(assets(), "templates/*.html")
	if err != nil {
		return err
	}
	if t, err = suite.Parse(t); err != nil {
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
	s.renderStatus(w, http.StatusOK, name, data)
}

func (s *site) renderStatus(w http.ResponseWriter, code int, name string, data map[string]any) {
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
	w.WriteHeader(code)
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render", "name", name, "err", err)
	}
}

// notFound is the suite's 404, except under /v1/ where the caller is a program.
func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	s.renderStatus(w, http.StatusNotFound, "notfound.html", map[string]any{"Title": "Not found",
		"Suite": suite.Page{Tool: "llm", SignedIn: s.auth.Authenticated(r), Source: sourceURL, Author: author, Year: time.Now().Year()}})
}

func (s *site) root(w http.ResponseWriter, r *http.Request) {
	if s.auth.Authenticated(r) {
		s.overview(w, r)
		return
	}
	api := `<span class="a">curl</span> llm.bythewood.me/v1/chat/completions \
  -H <span class="g">"Authorization: Bearer orch-…"</span> -d <span class="w">'{"messages": […]}'</span>`
	s.render(w, "landing.html", map[string]any{
		"Title": "llm", "Claude": s.claude != nil, "RetainDays": int(s.retain.Hours() / 24),
		"Suite": suite.Page{Tool: "llm", Tabs: []suite.Tab{{Name: "home", Href: "/", On: true}, {Name: "source", Href: sourceURL}},
			Meta: "openai compatible · self-hosted", Right: []suite.Seg{{Text: "sign in at auth.bythewood.me"}}},
		"Ways": suite.Ways{Host: "llm.bythewood.me",
			APITitle: "The OpenAI shape", APIText: "Anything that already speaks chat completions needs a base url and a key, not a rewrite.",
			API:        template.HTML(api),
			AgentTitle: "Agents call it too", AgentText: "Every service with a key is an agent here, and each call it makes is kept with the key that made it.",
			Agent: `<span class="c">caller</span> <span class="w">cron</span>  <span class="c">tokens</span> <span class="w">1.9k → 212</span>
<span class="c">searches</span> <span class="w">2</span>  <span class="g">logged</span>`,
			RunText: "give each service its own key."},
		"Footer": suite.Footer{Tool: "llm", About: "Sits in front of every model call my services make, with a key for each, a budget and a record of every call.",
			Source: sourceURL, Author: author, Year: time.Now().Year(), Pages: []suite.Tab{{Name: "Home", Href: "/"}}},
	})
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
	http.Redirect(w, r, "/keys", http.StatusSeeOther)
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
	http.Redirect(w, r, "/keys", http.StatusSeeOther)
}

func (s *site) keyDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.store.DeleteKey(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/keys", http.StatusSeeOther)
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

// short renders the messages array as something readable in a table cell,
// the last user message alone since the system prompt repeats on every call.
func short(s string, n int) string {
	var msgs []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	text := s
	if json.Unmarshal([]byte(s), &msgs) == nil && len(msgs) > 0 {
		text = msgs[len(msgs)-1].Content
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "user" {
				text = msgs[i].Content
				break
			}
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > n {
		return string(r[:n]) + "..."
	}
	return text
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
