// Package tools is what the model can do besides talk. Every backend here is
// keyless and every one is reachable from a scratch image over HTTPS, which is
// the whole selection criterion: a tool that needs an account is a tool that
// stops working when a key expires and nobody notices.
//
// A tool answers with a Go value that becomes JSON. It never returns prose,
// because the model writes the prose and the tool supplies the facts.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// UserAgent is a browser string because several of these endpoints answer 403
// to anything that looks automated. It is not a disguise, it is the price of
// entry for reading a public page.
// Chrome on Windows because it is the commonest thing an edge sees, and current
// because a two year old version on X11 Linux is a combination almost nothing
// real sends. Chrome itself zeroes the last two version fields.
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

// Deps is what every tool shares. Passing a clock rather than calling
// time.Now means a test can sit on a fixed day, which matters for anything
// that decides what "this weekend" is.
type Deps struct {
	// HTTP reaches anything, and is what the orchard tools use to read this
	// machine's own services by container name.
	HTTP *http.Client
	// Public refuses to connect to this machine or its network, and is what
	// every tool that takes a url from the model goes through.
	Public  *http.Client
	Now     func() time.Time
	Guard   *Guard
	Budgets *Budgets

	// The session of whoever is chatting, forwarded by the orchard tools so
	// each site does its own check. It is per turn rather than per process,
	// so it is set on a copy and never on the shared Deps.
	Session string

	// Where a tool hangs a chart or a forecast for the page to draw. Per turn
	// and on the copy, for the same reason the session is.
	Widgets *Sink

	// Whether this turn is incognito, which a tool that reaches a service with
	// a model behind it has to pass along. Per turn and on the copy too.
	Incognito bool
}

// WithSession returns a copy carrying one turn's session and its own widget
// sink. A copy because Deps is shared across every turn, and writing either of
// those onto it would hand one person's session, and one turn's charts, to the
// next request.
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
		// One host that starts refusing keeps refusing for a while, and asking
		// it again in the meantime is what keeps a rate limit alive rather than
		// letting it expire. See the DuckDuckGo and ESPN bans this was written
		// after.
		Guard: NewGuard(10 * time.Minute),
		// What we allow ourselves, as opposed to what a host has told us. A gap
		// bounds the rate and this bounds the total, and only the second one
		// stops a long turn spending a day's searches on one question.
		Budgets: NewBudgets(),
	}
}

// Guard is a per host circuit breaker. Anything that answers with a refusal
// puts its host in the penalty box, and every call to that host fails locally
// until the box empties.
// Store is however the caller persists the penalty box. It is an interface so
// this package stays a leaf and does not import the database.
type PenaltyStore interface {
	SavePenalty(host string, till time.Time, trips int)
	ClearPenalty(host string)
}

type Guard struct {
	mu   sync.Mutex
	till map[string]time.Time
	last map[string]time.Time
	// How many times in a row a host has refused. Asking again the moment a
	// ten minute box empties is what keeps a rate limit alive, so each repeat
	// doubles the wait instead of poking the same endpoint six times an hour.
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

// Wait blocks until this host may be called again. It holds the lock across the
// sleep on purpose, so two turns asking the same host queue rather than both
// deciding the gap has passed.
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

// How long a host that refused is left completely alone.
//
// Long and flat rather than short and escalating. A ten minute box means asking
// again six times an hour, and every one of those is a request to an endpoint
// that has already said no, which is how a soft limit turns into a hard one.
// There is no signal that a ban has lifted other than a call, so the only safe
// policy is to wait out a period long enough that the question is settled and
// let a person's next real question be the one that finds out.
const refusalCool = 6 * time.Hour

func (g *Guard) Trip(host string) {
	g.mu.Lock()
	g.trips[host]++
	till := time.Now().Add(refusalCool)
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

// Cleared on a call that worked, so one bad afternoon does not leave a host on
// a four hour backoff for the rest of the process.
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
// is unavailable rather than letting each turn discover it again.
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

// get fetches a URL through the breaker and returns the body.
func get(ctx context.Context, d *Deps, url string, accept string) ([]byte, error) {
	return getWith(ctx, d, url, accept, nil)
}

// getWith is get plus per host headers, which exists because pollen.com refuses
// a request that arrives without a Referer naming the page its own front end
// would have been on.
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
		d.Guard.Trip(host)
		return nil, fmt.Errorf("%s did not answer: %w", host, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	// 202 with an empty body is how both DuckDuckGo and ESPN say no. It reads
	// as an empty result rather than a refusal, which is worse than an error,
	// so it is turned into one here.
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == 200 && len(strings.TrimSpace(string(body))) == 0) {
		d.Guard.Trip(host)
		return nil, fmt.Errorf("%s is rate limiting us (status %d)", host, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s answered %d", host, resp.StatusCode)
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

func hostOf(url string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return s
}

// Registry is the set of tools the model is offered.
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

// Schemas renders the registry as the OpenAI tools array.
// Without drops one schema from an offer. Taking a tool off the table is how
// this harness stops something it cannot afford twice, the same way the repeat
// ledger does, because a rule in the prompt is a request and this is not.
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

// Call runs one tool. A tool that fails returns its error as content rather
// than blowing up the turn, because "the search engine is refusing us" is
// something the model should say out loud rather than something that should
// end the conversation.
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
		if err := json.Unmarshal(raw, &args); err != nil {
			args = map[string]any{}
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

// helpers for reading loosely typed arguments off a model
func argStr(a map[string]any, k string) string {
	if v, ok := a[k]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
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
