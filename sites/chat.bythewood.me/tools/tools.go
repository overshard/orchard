// Package tools is what the model can do besides talk. Every backend is keyless,
// since a tool that needs an account stops working when a key expires and nobody
// notices, and a tool answers with JSON and leaves the prose to the model.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// UserAgent is a current Chrome on Windows because several of these endpoints
// answer 403 to anything that looks automated, and an old version on Linux is a
// combination almost nothing real sends. Chrome itself zeroes the last two fields.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"

// Chrome sends these on every request, so a Chrome user agent arriving without
// them contradicts itself, which is cheaper to score than the agent string is.
const (
	clientHintUA       = `"Google Chrome";v="149", "Chromium";v="149", "Not?A_Brand";v="24"`
	clientHintMobile   = "?0"
	clientHintPlatform = `"Windows"`
)

// Result is what a tool hands back. Content is JSON encoded for the model.
type Result struct {
	Name    string          `json:"name"`
	Args    json.RawMessage `json:"args,omitempty"`
	Content any             `json:"content"`
	Err     string          `json:"error,omitempty"`
	Elapsed time.Duration   `json:"-"`
}

// Tool is one capability. Schema is the JSON Schema the model is handed, and
// Run is given the arguments it chose.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Run         func(ctx context.Context, d *Deps, args map[string]any) (any, error)
}

// Deps is what every tool shares. Now is a clock instead of time.Now so a test
// can sit on a fixed day, which matters for anything that decides what "this
// weekend" is.
type Deps struct {
	// HTTP reaches anything, including this machine's own services by container
	// name for the orchard tools.
	HTTP *http.Client
	// Public refuses to connect to this machine or its network, and every tool
	// that takes a url from the model goes through it.
	Public  *http.Client
	Now     func() time.Time
	Guard   *Guard
	Budgets *Budgets

	// The session of whoever is chatting, forwarded by the orchard tools so
	// each site does its own check. It's per turn, so it is set on a copy and
	// never on the shared Deps.
	Session string

	// Where a tool hangs a chart or a forecast for the page to draw. Per turn
	// and on the copy, for the same reason the session is.
	Widgets *Sink

	// Memory is the site's own fact store, so the remember tool writes to the
	// database this process owns instead of to a service over http.
	Memory Memory
	// History is the site's own conversation store, for the same reason.
	History History
	// Property is the house lookup engine, wired the same way since it owns
	// tables in the site's database.
	Property Property
	// A tool that reaches a service with a model behind it has to pass this
	// along. Per turn and on the copy too.
	Incognito bool

	// OnImage is where the turn wants to hear how a picture is getting on, and
	// Picture is the one the turn already showed as expected. Both per turn and
	// set on the copy.
	Images  Images
	OnImage func(ImageProgress)
	Picture *ImageProgress
	// Start is what this turn's picture starts from whatever the model says,
	// and LastPicture is what change_last means. Per turn, on the copy.
	Start       []string
	LastPicture string
}

// WithSession returns a copy carrying one turn's session and its own widget
// sink, since Deps is shared across every turn and writing either onto it would
// hand one person's session and one turn's charts to the next request.
func (d *Deps) WithSession(session string) *Deps {
	c := *d
	c.Session = session
	c.Widgets = NewSink()
	return &c
}

func NewDeps() *Deps {
	return &Deps{
		HTTP:   &http.Client{Timeout: 25 * time.Second},
		Public: publicClient(25 * time.Second),
		Now:    time.Now,
		// A host that starts refusing keeps refusing for a while, and asking it
		// again in the meantime keeps a rate limit alive instead of letting it expire.
		Guard: NewGuard(10 * time.Minute),
		// What we allow ourselves, as opposed to what a host has told us. A gap
		// bounds the rate but only a total stops a long turn spending a day's
		// searches on one question.
		Budgets: NewBudgets(),
	}
}

// PenaltyStore is an interface so this package stays a leaf and doesn't import
// the database.
type PenaltyStore interface {
	SavePenalty(host string, till time.Time, trips int)
	ClearPenalty(host string)
}

// Guard is a per host circuit breaker. A host that refuses goes in the penalty
// box, and every call to it fails locally until the box empties.
type Guard struct {
	mu   sync.Mutex
	till map[string]time.Time
	last map[string]time.Time
	// How many times in a row a host has failed, saved with the box.
	trips map[string]int
	cool  time.Duration
	store PenaltyStore
}

func NewGuard(cool time.Duration) *Guard {
	return &Guard{
		till: map[string]time.Time{}, last: map[string]time.Time{},
		trips: map[string]int{}, cool: cool,
	}
}

// Wait blocks until this host may be called again. The lock is held across the
// sleep so two turns asking the same host queue instead of both deciding the gap
// has passed, so don't narrow it.
func (g *Guard) Wait(host string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	gap := budgetFor(host).gap
	if t, seen := g.last[host]; seen {
		if d := gap - time.Since(t); d > 0 {
			time.Sleep(d)
		}
	}
	g.last[host] = time.Now()
}

func (g *Guard) Blocked(host string) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.till[host]
	if !ok || time.Now().After(t) {
		return false, 0
	}
	return true, time.Until(t)
}

// Long and flat, since a short box asks again several times an hour against an
// endpoint that already said no, which turns a soft limit into a hard one, and
// nothing but a call says a ban has lifted.
const refusalCool = 6 * time.Hour

// A page that timed out isn't a host refusing us, so this is long enough to stop
// a turn hammering a dead address and short enough that the next question can try.
const stumbleCool = 5 * time.Minute

// Trip is for a host that said no in as many words, a 202 or a 429.
func (g *Guard) Trip(host string) { g.box(host, refusalCool) }

// Stumble is for a request that failed without the host refusing anything, like
// a timeout, a dead name, or a redirect to somewhere that wouldn't answer.
func (g *Guard) Stumble(host string) { g.box(host, stumbleCool) }

func (g *Guard) box(host string, cool time.Duration) {
	g.mu.Lock()
	g.trips[host]++
	till := time.Now().Add(cool)
	// A stumble never shortens a refusal already in force.
	if old, ok := g.till[host]; ok && old.After(till) {
		till = old
	}
	g.till[host] = till
	trips, store := g.trips[host], g.store
	g.mu.Unlock()
	if store != nil {
		store.SavePenalty(host, till, trips)
	}
}

// Restore puts back the boxes that outlived the last process and takes the
// store to write future ones to.
func (g *Guard) Restore(store PenaltyStore, saved map[string][2]int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.store = store
	for host, v := range saved {
		g.till[host] = time.UnixMilli(v[0])
		g.trips[host] = int(v[1])
	}
}

// OK clears the trip count on a call that worked, so one bad afternoon doesn't
// follow a host around for the rest of the process.
func (g *Guard) OK(host string) {
	g.mu.Lock()
	_, had := g.trips[host]
	delete(g.trips, host)
	store := g.store
	g.mu.Unlock()
	if had && store != nil {
		store.ClearPenalty(host)
	}
}

// Down is every host currently in the penalty box, so the page can say search
// is unavailable instead of each turn discovering it again.
func (g *Guard) Down() map[string]time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]time.Duration{}
	now := time.Now()
	for host, till := range g.till {
		if till.After(now) {
			out[host] = time.Until(till).Round(time.Second)
		}
	}
	return out
}

// SearchHost is the one whose loss the page reports, since a turn without it
// cannot look anything up and every other tool is narrower.
const SearchHost = "html.duckduckgo.com"

func get(ctx context.Context, d *Deps, url string, accept string) ([]byte, error) {
	return getWith(ctx, d, url, accept, nil)
}

// getWith takes per host headers because pollen.com refuses a request without a
// Referer naming the page its own front end would have been on.
func getWith(ctx context.Context, d *Deps, url string, accept string, extra map[string]string) ([]byte, error) {
	host := hostOf(url)
	if blocked, left := d.Guard.Blocked(host); blocked {
		return nil, fmt.Errorf("%s refused us and is being left alone for another %s, "+
			"so nothing can be looked up there until then", host, round(left))
	}
	// The spend ceiling, checked before the gap so a spent pool costs no wait.
	if err := d.Budgets.Take(host); err != nil {
		return nil, err
	}
	d.Guard.Wait(host)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Ch-Ua", clientHintUA)
	req.Header.Set("Sec-Ch-Ua-Mobile", clientHintMobile)
	req.Header.Set("Sec-Ch-Ua-Platform", clientHintPlatform)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	client := d.Public
	if client == nil {
		client = d.HTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		// A refusal by the fence is not the host rate limiting us, so it must
		// not trip the breaker and lock out a host that never answered.
		if strings.Contains(err.Error(), "refusing") {
			return nil, fmt.Errorf("%s is not a public address", host)
		}
		d.Guard.Stumble(host)
		return nil, fmt.Errorf("%s did not answer, so try a different source: %s", host, transportReason(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	// 202 is how both DuckDuckGo and ESPN say no, and it reads as an empty result
	// unless it's turned into an error here. This and a 429 get the long box.
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusTooManyRequests {
		d.Guard.Trip(host)
		return nil, fmt.Errorf("%s is rate limiting us (status %d)", host, resp.StatusCode)
	}
	// An empty 200 from a page is a page with nothing on it, not a ban.
	if resp.StatusCode == 200 && len(strings.TrimSpace(string(body))) == 0 {
		d.Guard.Stumble(host)
		return nil, fmt.Errorf("%s returned an empty page, so try a different source", host)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s answered %d. %s", host, resp.StatusCode, statusAdvice(resp.StatusCode))
	}
	d.Guard.OK(host)
	return body, nil
}

func getJSON(ctx context.Context, d *Deps, url string, into any) error {
	return getJSONHeaders(ctx, d, url, nil, into)
}

func getJSONHeaders(ctx context.Context, d *Deps, url string, extra map[string]string, into any) error {
	b, err := getWith(ctx, d, url, "application/json", extra)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, into)
}

// statusAdvice tells the model what to do next, since a bare status code reads
// as "the tool is broken" and the answer that follows says nothing could be
// found. A wall and a wrong address need opposite responses.
func statusAdvice(code int) string {
	switch code {
	case http.StatusForbidden, http.StatusUnauthorized:
		return "That site blocks automated readers, and asking again will not change it. Use a different source."
	case http.StatusNotFound:
		return "There is no page at that address. Do not guess another one, search for the page and fetch the url the search returns."
	case http.StatusPaymentRequired, http.StatusGone:
		return "That page is not readable without paying or is gone. Use a different source."
	}
	if code >= 500 {
		return "That site is having trouble of its own. Use a different source."
	}
	return "Use a different source."
}

// transportReason keeps the useful half of a transport error. The full text is
// the whole request line, and when a fetch follows a redirect the address in it
// is wherever it ended up, which reads as the wrong host having failed.
func transportReason(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "context deadline exceeded"), strings.Contains(s, "Client.Timeout"):
		return "it took too long to answer"
	case strings.Contains(s, "no such host"):
		return "that name does not resolve"
	case strings.Contains(s, "connection refused"):
		return "nothing is listening there"
	case strings.Contains(s, "certificate"):
		return "its certificate did not check out"
	}
	// A redirect chain names the address it ended on and not the one asked for,
	// so the url is dropped instead of being reported as the host that failed.
	if i := strings.Index(s, ": "); i > 0 && strings.HasPrefix(s, "Get \"") {
		return strings.TrimSpace(s[i+2:])
	}
	return s
}

func hostOf(url string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return s
}

type Registry struct {
	byName map[string]Tool
	order  []string
}

func (r *Registry) Add(t Tool) {
	if r.byName == nil {
		r.byName = map[string]Tool{}
	}
	r.byName[t.Name] = t
	r.order = append(r.order, t.Name)
	sort.Strings(r.order)
}

func (r *Registry) Get(name string) (Tool, bool) { t, ok := r.byName[name]; return t, ok }
func (r *Registry) Names() []string              { return append([]string(nil), r.order...) }

// Without drops one schema from an offer. Taking a tool off the table stops
// something the harness can't afford twice, since a rule in the prompt is only
// a request.
func Without(schemas []map[string]any, name string) []map[string]any {
	out := make([]map[string]any, 0, len(schemas))
	for _, s := range schemas {
		if fn, ok := s["function"].(map[string]any); ok {
			if n, _ := fn["name"].(string); n == name {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// Schemas renders the registry as the OpenAI tools array.
func (r *Registry) Schemas() []map[string]any {
	out := make([]map[string]any, 0, len(r.order))
	for _, n := range r.order {
		t := r.byName[n]
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Schema,
			},
		})
	}
	return out
}

// Call returns a failed tool's error as content instead of ending the turn,
// because "the search engine is refusing us" is something the model should say
// out loud.
func (r *Registry) Call(ctx context.Context, d *Deps, name string, raw json.RawMessage) Result {
	start := time.Now()
	res := Result{Name: name, Args: raw}
	t, ok := r.Get(name)
	if !ok {
		res.Err = fmt.Sprintf("no tool named %q", name)
		res.Content = map[string]any{"error": res.Err}
		res.Elapsed = time.Since(start)
		return res
	}
	var args map[string]any
	if len(raw) > 0 {
		// Running the tool on no arguments would blame a missing field, when
		// what the model has to fix is the length of the call.
		if err := json.Unmarshal(raw, &args); err != nil {
			res.Err = "the arguments were cut off or are not valid JSON, call it again with shorter arguments"
			res.Content = map[string]any{"error": res.Err}
			res.Elapsed = time.Since(start)
			return res
		}
	}
	out, err := t.Run(ctx, d, args)
	if err != nil {
		res.Err = err.Error()
		res.Content = map[string]any{"error": err.Error()}
	} else {
		res.Content = out
	}
	res.Elapsed = time.Since(start)
	return res
}

func argStr(a map[string]any, k string) string {
	if v, ok := a[k]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

// argBool takes a small model's "true" as readily as its true.
func argBool(a map[string]any, k string) bool {
	switch v := a[k].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return false
}

func argNum(a map[string]any, k string, def float64) float64 {
	v, ok := a[k]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%g", &f); err == nil {
			return f
		}
	}
	return def
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
func integer(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

// Only narrows an offer to the named tools, for a turn with one job to do where
// every other tool is a way to get lost.
func Only(schemas []map[string]any, names ...string) []map[string]any {
	var out []map[string]any
	for _, s := range schemas {
		if fn, ok := s["function"].(map[string]any); ok {
			if n, _ := fn["name"].(string); slices.Contains(names, n) {
				out = append(out, s)
			}
		}
	}
	return out
}
