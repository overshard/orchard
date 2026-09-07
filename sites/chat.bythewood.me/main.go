// Command chat serves chat.bythewood.me: a conversation with a small model
// running on Isaac's own card, with tools for anything it cannot know, history
// in SQLite, and an incognito mode that writes nothing down.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"chat.bythewood.me/tools"
	"chat.bythewood.me/web"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

const (
	listenAddr = ":8000"
	sourceURL  = "https://github.com/overshard/orchard/tree/main/sites/chat.bythewood.me"
	selfSource = "chat"
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
	auth    *web.Authenticator
	llm     *LLM
	engine  *Engine
	store   *Store
	comp    *Compactor
	tpl     *template.Template
	md      goldmark.Markdown
	label   string
	ctxSize int
	dev     bool
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

func main() {
	var (
		addr    = flag.String("addr", env("CHAT_ADDR", listenAddr), "listen address")
		llmURL  = flag.String("llm", env("LLM_URL", "http://orchard-llm:8000"), "model gateway base url")
		llmKey  = flag.String("llm-key", os.Getenv("LLM_KEY"), "api key for the model gateway")
		verify  = flag.String("verify", "", "development only: check sessions against this url instead of auth")
		model   = flag.String("model", env("LLM_MODEL", "local"), "model name the server answers to")
		label   = flag.String("model-name", env("LLM_NAME", "Ornith 1.5 9B"), "readable model name, shown in the UI and told to the model")
		dbPath  = flag.String("db", env("CHAT_DB", "data/chat.db"), "conversation database")
		wikiURL = flag.String("wiki", env("WIKI_URL", "http://orchard-wiki:8000"), "offline wikipedia base url")
		ctxSize = flag.Int("ctx", envInt("LLM_CTX", 32768), "model context window in tokens")
		health  = flag.Bool("healthcheck", false, "probe the local server and exit")
	)
	flag.Parse()
	web.SetupLogging()

	if *health {
		if err := web.HealthCheck("http://127.0.0.1"+*addr+"/healthz", 3*time.Second); err != nil {
			os.Exit(1)
		}
		return
	}

	// Past the healthcheck branch, so a HEALTHCHECK invocation does not start a
	// queue it will never flush. This was the only one of the eleven sites not
	// shipping, which is why logging.bythewood.me had no record of chat at all.
	web.ShipLogs(selfSource, web.HTTPSink())

	store, err := OpenStore(*dbPath)
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	tools.WikiBase = *wikiURL

	llm := NewLLM(*llmURL, *model, *llmKey)
	// The verifier can only be moved in a development build. Reloaded is false
	// in the shipped image, so this cannot be turned on however the environment
	// is set, the same fence assets_disk.go has.
	auth := web.NewAuthenticator()
	if Reloaded && *verify != "" {
		slog.Warn("checking sessions against a development verifier", "url", *verify)
		auth = web.NewAuthenticatorAt(*verify)
	}

	s := &site{
		auth: auth,
		llm:  llm, engine: NewEngine(llm, *label), store: store, label: *label,
		comp: NewCompactor(llm, *ctxSize), ctxSize: *ctxSize, dev: Reloaded,
		md: goldmark.New(goldmark.WithExtensions(extension.GFM),
			goldmark.WithRendererOptions()),
	}
	s.engine.Render = s.render
	// The rate limit boxes survive a restart. Without this every deploy asked a
	// host that was already refusing, which is how a ban gets renewed rather
	// than expiring.
	s.engine.RestoreGuard(store, store.Penalties())
	s.engine.RestoreSpend(store.Spend(tools.SearchHost))
	if err := s.loadTemplates(); err != nil {
		slog.Error("templates", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	// Everything is gated. This site can read Isaac's own infrastructure
	// through its tools, so there is no anonymous surface at all, not even a
	// landing page, which is the difference between this and the dashboards
	// that show a signed out visitor something.
	// The root is the one public page: what this is and a way in. Everything
	// past it needs a session, and a signed in visitor gets the app here rather
	// than the pitch.
	mux.HandleFunc("GET /{$}", s.root)
	mux.HandleFunc("GET /c/{id}", s.auth.RequireAuth(s.page))
	// Memory. Reading and deleting are plain pages and plain posts, and the
	// only way to write is the one that goes through the model.
	mux.HandleFunc("GET /api/memory", s.auth.RequireAuthJSON(s.memoryList))
	mux.HandleFunc("POST /api/memory/teach", s.auth.RequireAuthJSON(s.memoryTeach))
	mux.HandleFunc("DELETE /api/memory/{id}", s.auth.RequireAuthJSON(s.memoryDelete))
	mux.HandleFunc("DELETE /api/memory", s.auth.RequireAuthJSON(s.memoryForget))
	// Stylesheets and scripts, which the landing page needs and which carry
	// nothing a session would protect.
	mux.Handle("GET /static/", s.static())
	// Signing in happens on auth.bythewood.me, and this is here so an old
	// bookmark lands somewhere useful rather than on a redirect loop.
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})
	// Not gated, because the container's own health check calls it over
	// loopback with no cookie and it says nothing.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("POST /api/send", s.auth.RequireAuthJSON(s.send))
	mux.HandleFunc("GET /api/conversations", s.auth.RequireAuthJSON(s.listConversations))
	mux.HandleFunc("GET /api/conversation/{id}", s.auth.RequireAuthJSON(s.getConversation))
	mux.HandleFunc("DELETE /api/conversation/{id}", s.auth.RequireAuthJSON(s.deleteConversation))
	mux.HandleFunc("DELETE /api/conversations", s.auth.RequireAuthJSON(s.deleteAll))
	mux.HandleFunc("GET /api/status", s.auth.RequireAuthJSON(s.status))
	// The readings behind a chart. Gated like everything else here, and read
	// only: both go out to a public source and neither touches this estate.
	mux.HandleFunc("GET /api/widget/ticker", s.auth.RequireAuthJSON(s.widgetTicker))
	mux.HandleFunc("GET /api/widget/weather", s.auth.RequireAuthJSON(s.widgetWeather))

	slog.Info("chat listening", "addr", *addr, "llm", *llmURL, "model", *label,
		"db", *dbPath, "reloaded", Reloaded)
	web.Serve(*addr, web.Chain(mux, web.Recovered, web.Logged, web.SecurityHeaders(csp())))
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func (s *site) loadTemplates() error {
	// Reading assets off disk means their hashes change while the process is
	// up, so the reload path drops the cached versions with the templates.
	if Reloaded {
		resetAssetVersions()
	}
	t, err := template.New("").Funcs(template.FuncMap{
		"when":  fmtWhen,
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
		// Assets come off disk in development so a CSS edit shows up on
		// reload, and out of the binary in the release build. That split is
		// why an embedded stylesheet does not silently ignore an edit.
		if Reloaded {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fs.ServeHTTP(w, r)
	})
}

// root shows the app to anyone signed in and the landing page to everyone else.
func (s *site) root(w http.ResponseWriter, r *http.Request) {
	if s.auth.Authenticated(r) {
		s.page(w, r)
		return
	}
	s.landing(w, r)
}

func (s *site) landing(w http.ResponseWriter, r *http.Request) {
	if Reloaded {
		if err := s.loadTemplates(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.tpl.ExecuteTemplate(w, "landing.html", map[string]any{
		"Source": sourceURL, "Model": s.label, "Ctx": kfmt(s.ctxSize),
	}); err != nil {
		slog.Error("render landing", "err", err)
	}
}

func (s *site) page(w http.ResponseWriter, r *http.Request) {
	if Reloaded {
		if err := s.loadTemplates(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	convs, _ := s.store.List(40)
	nConv, nMsg := s.store.Count()
	data := map[string]any{
		"Conversations": convs,
		"Source":        sourceURL,
		"Dev":           s.dev,
		"Model":         s.label,
		"Stats":         map[string]int{"Conversations": nConv, "Messages": nMsg},
		"Ctx":           *(&s.ctxSize),
		"Active":        r.PathValue("id"),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "app.html", data); err != nil {
		slog.Error("render", "err", err)
	}
}

type sendReq struct {
	Message   string `json:"message"`
	ConvID    string `json:"conversation_id"`
	Incognito bool   `json:"incognito"`
}

// A turn's whole upload, across every file on it. The per file ceiling is in
// attach.go and this is the one that stops ten of them at once.
const maxUploadBytes = 64 << 20

// readSend accepts either JSON or a multipart form, since a turn carrying files
// cannot be JSON and a turn without them should not have to be a form.
func readSend(w http.ResponseWriter, r *http.Request) (sendReq, []filePart, error) {
	var req sendReq
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			return req, nil, fmt.Errorf("bad request")
		}
		req.Message = strings.TrimSpace(req.Message)
		return req, nil, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		return req, nil, fmt.Errorf("that is more than %s of attachments", humanSize(maxUploadBytes))
	}
	// Anything over the memory limit was spooled to a temp file, so the parts
	// are turned into text here and the spill is dropped before the turn runs,
	// rather than sitting there for the twelve minutes a turn may take.
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	req.Message = strings.TrimSpace(r.FormValue("message"))
	req.ConvID = strings.TrimSpace(r.FormValue("conversation_id"))
	req.Incognito = r.FormValue("incognito") == "true"
	var headers []*multipart.FileHeader
	if r.MultipartForm != nil {
		headers = r.MultipartForm.File["files"]
	}
	return req, readFiles(headers), nil
}

// send runs one turn and streams it. Server sent events rather than a
// websocket because the traffic is one way and this survives a proxy.
func (s *site) send(w http.ResponseWriter, r *http.Request) {
	req, parts, err := readSend(w, r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	// A turn carrying files needs no message, since the files are the question.
	if req.Message == "" && len(parts) == 0 {
		http.Error(w, "empty message", 400)
		return
	}
	prompt := composeTurn(req.Message, parts)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	// A type assertion for http.Flusher does not survive the request logger,
	// which wraps the writer in a recorder that only promotes three methods.
	// ResponseController follows Unwrap and works through it.
	rc := http.NewResponseController(w)

	var mu = make(chan struct{}, 1)
	mu <- struct{}{}
	emit := func(e Event) {
		<-mu
		defer func() { mu <- struct{}{} }()
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "data: %s\n\n", b)
		_ = rc.Flush()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Minute)
	defer cancel()
	// Every model call this turn makes hangs off this context, including the
	// ones the tools start, so marking it here is what keeps the gateway from
	// writing down what the local database is not writing down either.
	if req.Incognito {
		ctx = WithIncognito(ctx)
	}

	// Load the conversation and build the window before anything else, since
	// compaction may need a model call of its own.
	var history []Message
	var conv Conversation
	var stored []Stored
	if !req.Incognito && req.ConvID != "" {
		conv, _ = s.store.Get(req.ConvID)
		stored, _ = s.store.Messages(req.ConvID)
		var summary string
		var covered int
		var changed bool
		history, summary, covered, changed = s.comp.Window(ctx, conv, stored)
		if changed {
			emit(Event{Kind: "status", Text: "compacting"})
			_ = s.store.SetSummary(req.ConvID, summary, covered)
		}
	}

	// Warm the weights while the window is being built rather than after.
	go s.llm.Warm(context.WithoutCancel(ctx))

	var session string
	if c, err := r.Cookie(web.SessionCookie); err == nil {
		session = c.Value
	}
	// Retrieval is against what the user typed, not the composed prompt, since
	// the text of an attachment would swamp the scoring with its own words.
	recalled := s.store.Relevant(req.Message, factsPerTurn)
	reply, used, srcs, widgets, stats, err := s.engine.Run(ctx, history, prompt, session, memoryBlock(recalled), emit)
	// Whether the turn worked or not, whatever it spent has been spent, and a
	// failed turn is exactly when the counts matter most.
	s.engine.SaveSpend(s.store.SaveSpend)
	if err != nil {
		emit(Event{Kind: "error", Text: err.Error()})
		return
	}

	// Whatever the model wrote, the stored copy has no leaked markup in it.
	reply.Content, _ = salvageCalls(reply.Content, func(string) bool { return false })

	summaries := make([]ToolSummary, 0, len(used))
	for _, u := range used {
		summaries = append(summaries, ToolSummary{
			Name: u.Name, Args: shortArgs(string(u.Args)),
			MS: u.Elapsed.Milliseconds(), OK: u.Err == "", Err: u.Err,
		})
	}

	convID := req.ConvID
	if !req.Incognito {
		if convID == "" {
			if id, e := s.store.NewConversation(""); e == nil {
				convID = id
			}
		}
		if convID != "" {
			user := Stored{Role: RoleUser, Content: prompt}
			if len(parts) > 0 {
				user.Display, user.Files = req.Message, attachments(parts)
			}
			_ = s.store.Append(convID, user)
			_ = s.store.Append(convID, Stored{Role: RoleAssistant, Content: reply.Content,
				Tools: summaries, Sources: srcs, Widgets: widgets})
			if len(stored) == 0 {
				if t := s.comp.Title(context.WithoutCancel(ctx), titleSeed(req.Message, parts)); t != "" {
					_ = s.store.SetTitle(convID, t)
				}
			}
			s.store.Checkpoint()
		}
	}

	// After the answer is on its way, never in front of it. Incognito is
	// excluded: a mode that writes nothing down cannot be the one that teaches
	// it something to write down later.
	if !req.Incognito {
		go func() {
			// Recovered here and not by the middleware, which only wraps the
			// handler. A panic on this goroutine would take the process down
			// and lose every conversation in flight.
			defer func() {
				if r := recover(); r != nil {
					slog.Error("the memory pass panicked", "err", r)
				}
			}()
			bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			defer cancel()
			s.Remember(bg, req.Message, reply.Content)
		}()
	}

	done := map[string]any{"kind": "done", "conversation_id": convID,
		"tools": summaries, "html": s.renderCited(reply.Content, srcs), "incognito": req.Incognito,
		"sources": srcs,
		"files":   attachments(parts),
		"stats": map[string]any{
			"prompt_tokens": stats.Prompt, "completion_tokens": stats.Completion,
			"decode_tps": round1(stats.Decode), "prefill_tps": round1(stats.Prefill),
			"ctx": s.ctxSize,
		}}
	b, _ := json.Marshal(done)
	fmt.Fprintf(w, "data: %s\n\n", b)
	_ = rc.Flush()
}

// render turns the model's markdown into HTML on the server, so the browser
// never has to parse markdown and the sanitising happens in one place.
func (s *site) render(md string) string {
	var sb strings.Builder
	if err := s.md.Convert([]byte(md), &sb); err != nil {
		return "<p>" + template.HTMLEscapeString(md) + "</p>"
	}
	return sb.String()
}

// renderCited is the same render with the citation numbers turned into links.
// The markdown is stored with its numbers rather than its anchors, so a change
// to how a pill looks does not need every old message rewritten.
func (s *site) renderCited(md string, srcs []Source) string {
	return linkCitations(s.render(md), srcs)
}

func (s *site) listConversations(w http.ResponseWriter, r *http.Request) {
	convs, err := s.store.List(60)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"conversations": convs})
}

func (s *site) getConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	msgs, err := s.store.Messages(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	type out struct {
		Role    Role          `json:"role"`
		HTML    string        `json:"html"`
		Text    string        `json:"text"`
		Files   []Attachment  `json:"files,omitempty"`
		Tools   []ToolSummary `json:"tools,omitempty"`
		Sources []Source      `json:"sources,omitempty"`
		Widgets []Widget      `json:"widgets,omitempty"`
	}
	rendered := make([]out, 0, len(msgs))
	for _, m := range msgs {
		o := out{Role: m.Role, Text: m.Shown(), Files: m.Files, Tools: m.Tools,
			Sources: m.Sources, Widgets: m.Widgets}
		if m.Role == RoleAssistant {
			o.HTML = s.renderCited(m.Content, m.Sources)
		}
		rendered = append(rendered, o)
	}
	conv, _ := s.store.Get(id)
	writeJSON(w, map[string]any{"id": id, "title": conv.Title, "messages": rendered})
}

func (s *site) deleteConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Delete(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"deleted": id})
}

func (s *site) deleteAll(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAll(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"deleted": "all"})
}

func (s *site) status(w http.ResponseWriter, r *http.Request) {
	nConv, nMsg := s.store.Count()
	out := map[string]any{
		"model": s.label, "up": s.llm.Healthy(r.Context()), "ctx": s.ctxSize,
		"tools": tools.Default().Names(), "conversations": nConv, "messages": nMsg,
	}
	// Search being unavailable is the one tool failure worth saying out loud,
	// because a turn without it answers from memory and reads like an ordinary
	// answer. Everything else is narrow enough to report itself in the turn.
	if left, down := s.engine.SearchDown(); down {
		out["search_down"] = true
		out["search_back_in"] = left.String()
	}
	minute, hour, day := s.engine.SearchSpend()
	out["search_left"] = map[string]int{"minute": minute, "hour": hour, "day": day}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// kfmt matches the meter in the app, which divides by 1024 because a context
// window is a power of two and 65536 has to read as the 64k it was set to.
func kfmt(n int) string {
	if n < 1024 {
		return strconv.Itoa(n)
	}
	return strconv.Itoa(n/1024) + "k"
}
