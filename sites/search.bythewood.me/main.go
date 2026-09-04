// Command search serves search.bythewood.me: ask a question, it runs
// DuckDuckGo, fetches and cleans the pages it finds, and writes an answer whose
// every sentence is checked against the passage it cites. The model is a 4B in
// a container with the GPU attached, started on demand, so asking nothing costs
// nothing.
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
	"strings"
	"time"

	"search.bythewood.me/web"
)

const (
	listenAddr = ":8000"

	// The public repository this site lives in. Every gated site here links to
	// its own source from its landing page.
	sourceURL = "https://github.com/overshard/orchard/tree/main/sites/search.bythewood.me"
)

type site struct {
	engine   *Engine
	store    *Store
	llm      *LLM
	sessions *Sessions
	budget   *Budget
	auth     *web.Authenticator

	// devOpen skips the auth check so the UI can be worked on without a
	// session. It is gated on Reloaded, which is false in any build made with
	// -tags embed, so the shipped image cannot turn this on however the
	// environment is set.
	devOpen bool
}

func devOpen() bool {
	if !Reloaded {
		return false
	}
	if os.Getenv("SEARCH_DEV_NOAUTH") == "" {
		return false
	}
	slog.Warn("auth is bypassed, this is a development build only")
	return true
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if *healthcheck {
		resp, err := http.Get("http://127.0.0.1:8000/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}

	store, err := OpenStore(env("SITE_DATA", "build/data"))
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer store.Close()

	llm := NewLLM(env("LLM_URL", "http://127.0.0.1:8091"))

	if _, err := loadTemplates(); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	budget := NewBudget()
	s := &site{
		auth:     web.NewAuthenticator(),
		devOpen:  devOpen(),
		budget:   budget,
		engine:   NewEngine(store, llm, budget),
		store:    store,
		llm:      llm,
		sessions: NewSessions(),
	}

	// Everything here is behind auth. This site spends Isaac's GPU and searches
	// from his address on every question, so an open one would be a stranger's
	// search engine running on his hardware.
	mux := http.NewServeMux()
	// The root is public and explains what this is, the way every other gated
	// site here does. The tool itself is behind auth, because it spends a GPU
	// and searches from Isaac's address on every question.
	mux.HandleFunc("GET /{$}", s.landing)
	mux.HandleFunc("GET /search", s.gate(s.app))
	mux.HandleFunc("GET /stream", s.gate(s.ask))
	mux.HandleFunc("POST /reset", s.gateJSON(s.reset))
	mux.HandleFunc("GET /budget", s.gateJSON(s.budgetState))

	// Signing in happens on auth.bythewood.me. This stays so an old bookmark
	// or a typed /login still lands somewhere sensible.
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})

	// Unauthenticated on purpose: the health strip on dash probes this over the
	// bridge, and Caddy refuses it from outside.
	mux.HandleFunc("GET /healthz", s.healthz)

	mux.Handle("GET /static/", http.FileServer(http.FS(assets())))

	slog.Info("search serving",
		slog.String("addr", listenAddr),
		slog.String("llm", llm.BaseURL),
		slog.Bool("assets_from_disk", Reloaded))
	server := &http.Server{
		Addr:        listenAddr,
		Handler:     mux,
		ReadTimeout: 15 * time.Second,
		// Go's write bound covers the whole response, so any value at all is a
		// ceiling on how long an event stream may stay open. Same reason dash
		// sets it to zero.
		WriteTimeout: 0,
	}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

// loadTemplates parses from whichever source this build uses. In development
// that is the disk, so a template edit shows on reload rather than at the next
// rebuild.
func loadTemplates() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{"hostname": hostname}).
		ParseFS(assets(), "templates/*.html")
}

func (s *site) render(w http.ResponseWriter, name string, data any) {
	tmpl, err := loadTemplates()
	if err != nil {
		slog.Error("template parse failed", slog.Any("err", err))
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render failed", slog.Any("err", err))
	}
}

func (s *site) gate(next http.HandlerFunc) http.HandlerFunc {
	if s.devOpen {
		return next
	}
	return s.auth.RequireAuth(next)
}

func (s *site) gateJSON(next http.HandlerFunc) http.HandlerFunc {
	if s.devOpen {
		return next
	}
	return s.auth.RequireAuthJSON(next)
}

// landing is what a signed out visitor sees. It says what this is and where the
// source is, and nothing about what has been asked.
func (s *site) landing(w http.ResponseWriter, r *http.Request) {
	pages, chunks := s.store.Stats()
	s.render(w, "landing.html", map[string]any{
		"Pages":         pages,
		"Chunks":        chunks,
		"SourceURL":     sourceURL,
		"Authenticated": s.devOpen || s.auth.Authenticated(r),
	})
}

func (s *site) app(w http.ResponseWriter, r *http.Request) {
	pages, chunks := s.store.Stats()
	s.render(w, "app.html", map[string]any{
		"Pages":     pages,
		"Chunks":    chunks,
		"LLMUp":     s.llm.Healthy(r.Context()),
		"SessionID": NewSessionID(),
		"Budget":    s.budget.State(),
		"Ambient":   AmbientFacts(),
		"SourceURL": sourceURL,
	})
}

func (s *site) reset(w http.ResponseWriter, r *http.Request) {
	s.sessions.Reset(r.URL.Query().Get("sid"))
	w.WriteHeader(http.StatusNoContent)
}

// ask streams the pipeline. Every step reports as it happens, because a
// question can take fifteen seconds and a spinner that says nothing is the
// difference between "working" and "broken".
func (s *site) ask(w http.ResponseWriter, r *http.Request) {
	question := strings.TrimSpace(r.URL.Query().Get("q"))
	sid := r.URL.Query().Get("sid")
	if question == "" {
		http.Error(w, "no question", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	send := func(event string, payload any) {
		blob, err := json.Marshal(payload)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, blob)
		flusher.Flush()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Minute)
	defer cancel()

	progress := Progress(func(step, detail string) {
		send("status", map[string]string{"step": step, "detail": detail})
	})

	history := s.sessions.History(sid)
	if len(history) > 0 {
		send("status", map[string]string{"step": "followup", "detail": "following on from the last answer"})
	}

	ans, err := s.engine.Run(ctx, question, history, progress)
	if err != nil {
		send("failed", map[string]string{"error": err.Error()})
		return
	}

	if sid != "" {
		s.sessions.Append(sid, Turn{Question: question, Answer: ans.Text})
	}
	pages, chunks := s.store.Stats()
	send("answer", map[string]any{
		"budget":     s.budget.State(),
		"question":   ans.Query,
		"standalone": ans.Standalone,
		"shape":      ans.Shape,
		"html":       ans.HTML,
		"sources":    ans.Sources,
		"citations":  ans.Citations,
		"passages":   ans.Passages,
		"queries":    ans.Queries,
		"elapsed":    ans.Elapsed,
		"warnings":   ans.Warnings,
		"retried":    ans.Retried,
		"support":    ans.Support,
		"pages":      pages,
		"chunks":     chunks,
	})
}

// budgetState is polled by the page so the search allowance is visible before
// someone runs into it rather than after.
func (s *site) budgetState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.budget.State())
}

func (s *site) healthz(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

func hostname(raw string) string {
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if i := strings.IndexByte(raw, '/'); i > 0 {
		raw = raw[:i]
	}
	return strings.TrimPrefix(raw, "www.")
}
