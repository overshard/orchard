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

	// Must match the first hostname label, like every other source label.
	selfSource = "search"

	// analyticsID is in the page source of every site; it is identity, not a
	// credential. Its own property, since a copied one silently files this
	// site's traffic under whichever site it was copied from.
	analyticsID = "5782ea95-0169-4095-b94a-0b2b420440ed"
)

type site struct {
	engine   *Engine
	store    *Store
	hist     *History
	llm      *LLM
	sessions *Sessions
	budget   *Budget
	queue    *Queue
	assets   *Assets
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

	web.SetupLogging()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	// Tees stdout records to logging.bythewood.me; see web/shipper.go. It goes
	// after the healthcheck branch so a HEALTHCHECK does not start a queue it
	// will never flush.
	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

	dataDir := env("SITE_DATA", "build/data")
	store, err := OpenStore(dataDir)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer store.Close()

	llm := NewLLM(env("LLM_URL", "http://orchard-llm:8000"), os.Getenv("LLM_KEY"))

	budget := NewBudget()
	hist, err := OpenHistory(dataDir)
	if err != nil {
		slog.Error("history open failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer hist.Close()

	s := &site{
		queue:    NewQueue(),
		hist:     hist,
		assets:   NewAssets(assets()),
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
	if _, err := s.loadTemplates(); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	mux := http.NewServeMux()
	// The root is public and explains what this is, the way every other gated
	// site here does. The tool itself is behind auth, because it spends a GPU
	// and searches from Isaac's address on every question.
	mux.HandleFunc("GET /{$}", s.landing)
	mux.HandleFunc("GET /search", s.gate(s.app))
	mux.HandleFunc("GET /stream", s.gate(s.ask))
	mux.HandleFunc("POST /reset", s.gateJSON(s.reset))
	mux.HandleFunc("GET /budget", s.gateJSON(s.budgetState))

	// The history of what was asked, which is the one thing the cache does not
	// hold and the only page here that can delete anything.
	mux.HandleFunc("GET /history", s.gate(s.historyPage))
	mux.HandleFunc("POST /rate", s.gateJSON(s.rate))
	mux.HandleFunc("POST /forget", s.gateJSON(s.forget))

	// Signing in happens on auth.bythewood.me. This stays so an old bookmark
	// or a typed /login still lands somewhere sensible.
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})

	// Unauthenticated on purpose: the health strip on dash probes this over the
	// bridge, and Caddy refuses it from outside.
	mux.HandleFunc("GET /healthz", s.healthz)

	mux.Handle("GET /static/", s.assets.Handler())

	slog.Info("search serving",
		slog.String("addr", listenAddr),
		slog.String("llm", llm.BaseURL),
		slog.Bool("assets_from_disk", Reloaded))
	// Recovered is outermost so a panic in the pipeline becomes a 500 rather
	// than taking the process and every question in flight with it.
	handler := web.Chain(mux, web.Recovered, web.Logged)

	if err := web.Serve(listenAddr, handler); err != nil {
		slog.Error("server stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

// loadTemplates parses from whichever source this build uses. In development
// that is the disk, so a template edit shows on reload rather than at the next
// rebuild.
func (s *site) loadTemplates() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"hostname": hostname,
		"asset":    s.assets.URL,
		"num":      formatNum,
		"reason":   reasonText,
	}).ParseFS(assets(), "templates/*.html")
}

func (s *site) render(w http.ResponseWriter, name string, data any) {
	tmpl, err := s.loadTemplates()
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
	pages, chunks, sites := s.store.Stats()
	s.render(w, "landing.html", map[string]any{
		"Pages":         pages,
		"Chunks":        chunks,
		"Sites":         sites,
		"SourceURL":     sourceURL,
		"Authenticated": s.devOpen || s.auth.Authenticated(r),
		"Analytics":     !Reloaded,
		"AnalyticsID":   analyticsID,
	})
}

func (s *site) app(w http.ResponseWriter, r *http.Request) {
	pages, chunks, _ := s.store.Stats()
	s.render(w, "app.html", map[string]any{
		"Pages":       pages,
		"Chunks":      chunks,
		"LLMUp":       s.llm.Healthy(r.Context()),
		"SessionID":   NewSessionID(),
		"Budget":      s.budget.State(),
		"Ambient":     AmbientFacts(),
		"SourceURL":   sourceURL,
		"Analytics":   !Reloaded,
		"AnalyticsID": analyticsID,
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
	incognito := r.URL.Query().Get("incognito") == "1"
	// chat's deep_search wants the history row skipped on every question it
	// asks, since chat is already keeping that conversation, without claiming
	// the turn was incognito when it was not.
	nohistory := incognito || r.URL.Query().Get("nohistory") == "1"
	if question == "" {
		http.Error(w, "no question", http.StatusBadRequest)
		return
	}

	// ResponseController rather than a type assertion for http.Flusher, because
	// the request logger wraps the writer and an assertion would see the
	// wrapper. It follows Unwrap down to the real one.
	rc := http.NewResponseController(w)

	// web/server.go sets no write bound for this site, and this clears any
	// per-connection deadline anyway, so an answer that takes minutes is never
	// cut mid-frame. It doubles as the check that this writer can be flushed.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc.Flush()

	send := func(event string, payload any) {
		blob, err := json.Marshal(payload)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, blob)
		rc.Flush()
	}

	// Longer than one question needs, because it now covers waiting for the
	// people ahead as well as answering.
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
	defer cancel()
	// Every model call the pipeline makes hangs off this, so marking it here is
	// what keeps the gateway from writing down a question this site is not
	// writing down either.
	if incognito {
		ctx = WithIncognito(ctx)
	}

	// One question runs at a time, since there is one GPU and the model server
	// holds a single slot. Waiting is shown rather than hidden.
	release, ok := s.queue.Enter(ctx, func(q QueueState) {
		send("queued", q)
	})
	if !ok {
		return // the client went away while waiting
	}
	defer release()

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

	// Incognito skips this row and the gateway's copy of the prompts, and
	// nothing else. The pages fetched on the way still go in the archive, which
	// Isaac decided is fine: it is public articles with no question attached,
	// so nothing there reads back as what was asked. The row is what would.
	var logged int64
	if !nohistory {
		id, err := s.hist.Log(ans, s.stamp())
		if err != nil {
			slog.Warn("history write", slog.Any("err", err))
		}
		logged = id
	}

	pages, chunks, _ := s.store.Stats()
	send("answer", map[string]any{
		"id":         logged,
		"incognito":  incognito,
		"budget":     s.budget.State(),
		"question":   ans.Query,
		"standalone": ans.Standalone,
		"shape":      ans.Shape,
		"skill":      ans.Skill,
		"text":       ans.Text,
		"html":       ans.HTML,
		"sources":    ans.Sources,
		"links":      ans.Links,
		"citations":  ans.Citations,
		"checks":     ans.Checks,
		"deps":       ans.Deps,
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

// stamp says what produced an answer. The model comes off the last response
// rather than the config, so it names the repository and quant actually loaded.
func (s *site) stamp() Stamp {
	return Stamp{
		Model:    s.llm.Served(),
		Prompts:  promptVersion(),
		Sampling: samplingVersion(),
		Build:    map[bool]string{true: "dev", false: "release"}[Reloaded],
	}
}

func (s *site) historyPage(w http.ResponseWriter, r *http.Request) {
	only := r.URL.Query().Get("only")
	entries, err := s.hist.List(200, 0, only)
	if err != nil {
		slog.Error("history read", slog.Any("err", err))
		http.Error(w, "history unavailable", http.StatusInternalServerError)
		return
	}
	total, rated := s.hist.Count()
	s.render(w, "history.html", map[string]any{
		"Entries": entries,
		"Only":    only,
		"Total":   total,
		"Rated":   rated,
	})
}

// rate takes the thumb. The reason is a short enum rather than free text
// because a bare thumb cannot say which step went wrong, and which step went
// wrong is the entire value of collecting it.
func (s *site) rate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      int64  `json:"id"`
		Verdict int    `json:"verdict"`
		Reason  string `json:"reason"`
		Note    string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !validReason(in.Reason) {
		in.Reason = ""
	}
	if err := s.hist.Rate(in.ID, in.Verdict, in.Reason, truncate(in.Note, 500)); err != nil {
		slog.Warn("rate failed", slog.Any("err", err))
		http.Error(w, "could not save that", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

// The reasons map onto the steps of the pipeline, so a month of them says
// where to spend the effort rather than only how often it was wrong.
// The wording matches the buttons on the answer, so a row on the history page
// reads back as the thing that was actually clicked.
var reasons = map[string]string{
	"wrong":   "it is wrong",             // synthesis or validation
	"stale":   "already happened",        // shape and planning
	"missed":  "answered something else", // routing and shape
	"sources": "bad sources",             // retrieval
}

func validReason(r string) bool { _, ok := reasons[r]; return ok }

// reasonText is what the history page shows on a row. The stored value is the
// key, so the wording can change without rewriting what was already collected.
func reasonText(key string) string {
	if text, ok := reasons[key]; ok {
		return text
	}
	return key
}

func (s *site) forget(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID  int64 `json:"id"`
		All bool  `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var err error
	if in.All {
		err = s.hist.DeleteAll()
	} else if in.ID > 0 {
		err = s.hist.Delete(in.ID)
	} else {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err != nil {
		slog.Warn("forget failed", slog.Any("err", err))
		http.Error(w, "could not delete that", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}
