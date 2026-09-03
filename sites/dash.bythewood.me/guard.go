package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Every upstream this site reads is free, keyless and someone else's. A ban on
// one of them is not something a retry fixes and it lands on Isaac rather than
// on a reviewer, so each endpoint is fenced: a hard budget per hour, a pause
// between consecutive calls, and a breaker that opens on the responses that
// mean stop asking.
//
// The state is written to disk because a breaker that resets on restart is not
// a breaker. A restart loop against an endpoint that just returned 429 is
// exactly the case this is here to prevent, and an in-memory counter would
// forget about it every few seconds. It is JSON rather than SQLite because
// four counters per endpoint do not need a database, and skipping one keeps
// this the only site in the repo with no cgo-free driver, no volume of
// consequence and no schema to migrate.

var errGuardOpen = errors.New("circuit open")

// Budgets are per rolling hour and are ceilings to fail against, not targets.
// The pollers ask for far less: markets at 30s is 120/hr, and everything else
// is minutes apart.
type budget struct {
	perHour int
	pace    time.Duration
}

var budgets = map[string]budget{
	"yahoo":     {perHour: 900, pace: 2 * time.Second},
	"algolia":   {perHour: 120, pace: 2 * time.Second},
	"lobsters":  {perHour: 120, pace: 2 * time.Second},
	"openmeteo": {perHour: 120, pace: 2 * time.Second},
	"npr":       {perHour: 120, pace: 2 * time.Second},
	"bbc":       {perHour: 60, pace: 2 * time.Second},
	"nasdaq":    {perHour: 120, pace: 2 * time.Second},
	"nws":       {perHour: 120, pace: 2 * time.Second},
	"pollen":    {perHour: 60, pace: 2 * time.Second},
	"steam":     {perHour: 240, pace: time.Second},
	"justwatch": {perHour: 60, pace: 2 * time.Second},
	// No pacing on these two. They go to Isaac's own machine rather than to a
	// stranger's endpoint, and the strip fires every probe at once, so a
	// pause between them would only refuse all but the first.
	"uptime":  {perHour: 900},
	"logging": {perHour: 900},
}

const (
	// Trip after this many consecutive failures. A single timeout on a free
	// endpoint is normal and should not stop the panel.
	failsToTrip = 4

	minBackoff = 30 * time.Second
	maxBackoff = 30 * time.Minute
)

type guardEntry struct {
	WindowStart time.Time     `json:"window_start"`
	Count       int           `json:"count"`
	Fails       int           `json:"fails"`
	LastCall    time.Time     `json:"last_call"`
	OpenUntil   time.Time     `json:"open_until"`
	Backoff     time.Duration `json:"backoff"`
}

// Guard is safe for concurrent use. Every poller shares one.
type Guard struct {
	path string

	mu      sync.Mutex
	entries map[string]*guardEntry
	dirty   bool
}

func NewGuard(dataDir string) *Guard {
	g := &Guard{
		path:    filepath.Join(dataDir, "guard.json"),
		entries: map[string]*guardEntry{},
	}

	b, err := os.ReadFile(g.path)
	if err != nil {
		// A missing file is the first boot. Anything else is worth saying out
		// loud, since it means the breaker starts closed against an endpoint
		// that may have been the reason for the restart.
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("guard state unreadable, starting closed",
				slog.String("component", "guard"), slog.Any("err", err))
		}
		return g
	}
	if err := json.Unmarshal(b, &g.entries); err != nil {
		slog.Warn("guard state unparseable, starting closed",
			slog.String("component", "guard"), slog.Any("err", err))
		g.entries = map[string]*guardEntry{}
	}
	return g
}

func (g *Guard) entry(name string) *guardEntry {
	e, ok := g.entries[name]
	if !ok {
		e = &guardEntry{}
		g.entries[name] = e
	}
	return e
}

// Allow reports whether a call to name may go out now, and reserves a slot in
// the budget when it may. A caller that is refused must not make the request.
//
// Pacing is reported as a refusal here, which is right for a probe that has
// something better to do than queue. Anything on a timer should call Reserve
// instead: three pollers sharing the Yahoo endpoint all fired at boot, two were
// refused for being 1.6 seconds early, and because their retry interval is an
// hour and six hours the panels they fill stayed empty for the rest of the day.
func (g *Guard) Allow(name string) error {
	wait, err := g.tryReserve(name)
	if err != nil {
		return err
	}
	if wait > 0 {
		return fmt.Errorf("%s: paced, %s early", name, wait.Round(time.Millisecond))
	}
	return nil
}

// Reserve waits out the pace and then reserves a slot. An open breaker and a
// spent budget still refuse outright, because neither is fixed by waiting a
// moment and a caller that queued on them would pile up behind a dead endpoint.
func (g *Guard) Reserve(ctx context.Context, name string) error {
	for {
		wait, err := g.tryReserve(name)
		if err != nil {
			return err
		}
		if wait == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// tryReserve returns how long the caller has to wait for the pace, or an error
// for the two conditions that waiting does not help. A zero wait with no error
// means the slot is taken.
func (g *Guard) tryReserve(name string) (time.Duration, error) {
	now := time.Now()

	g.mu.Lock()
	defer g.mu.Unlock()

	e := g.entry(name)

	if now.Before(e.OpenUntil) {
		return 0, fmt.Errorf("%s: %w for another %s", name, errGuardOpen, e.OpenUntil.Sub(now).Round(time.Second))
	}

	b, ok := budgets[name]
	if !ok {
		return 0, fmt.Errorf("%s: no budget defined", name)
	}

	if now.Sub(e.WindowStart) >= time.Hour {
		e.WindowStart = now
		e.Count = 0
	}
	if e.Count >= b.perHour {
		return 0, fmt.Errorf("%s: hourly budget of %d spent", name, b.perHour)
	}
	if since := now.Sub(e.LastCall); since < b.pace {
		return b.pace - since, nil
	}

	e.Count++
	e.LastCall = now
	g.dirty = true
	return 0, nil
}

// Succeed closes the breaker and clears the backoff.
func (g *Guard) Succeed(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	e := g.entry(name)
	if e.Fails != 0 || e.Backoff != 0 {
		e.Fails = 0
		e.Backoff = 0
		g.dirty = true
	}
}

// Fail records an unsuccessful call. status is the HTTP status where there was
// one and 0 for a transport error. 429 and 503 open the breaker at once,
// because they are the endpoint saying so rather than the network being bad.
func (g *Guard) Fail(name string, status int, retryAfter time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	e := g.entry(name)
	e.Fails++
	g.dirty = true

	immediate := status == 429 || status == 503
	if !immediate && e.Fails < failsToTrip {
		return
	}

	if e.Backoff == 0 {
		e.Backoff = minBackoff
	} else {
		e.Backoff *= 2
	}
	if e.Backoff > maxBackoff {
		e.Backoff = maxBackoff
	}
	// An explicit Retry-After wins over the doubling, both ways: the endpoint
	// knows when it wants us back and guessing shorter is how a soft limit
	// becomes a hard one.
	wait := e.Backoff
	if retryAfter > wait {
		wait = retryAfter
	}

	e.OpenUntil = time.Now().Add(wait)
	slog.Warn("upstream breaker opened",
		slog.String("component", "guard"),
		slog.String("endpoint", name),
		slog.Int("status", status),
		slog.String("for", wait.String()))
}

// Flush writes the state out when it has changed. Called on a timer rather
// than on every call so a 30 second poll does not mean a write per tick.
func (g *Guard) Flush() {
	g.mu.Lock()
	if !g.dirty {
		g.mu.Unlock()
		return
	}
	b, err := json.Marshal(g.entries)
	g.dirty = false
	g.mu.Unlock()

	if err != nil {
		slog.Warn("guard state unmarshalable", slog.String("component", "guard"), slog.Any("err", err))
		return
	}
	// Rename onto the real path so a crash mid-write leaves the old state
	// rather than a truncated file the next boot would refuse to parse.
	tmp := g.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		slog.Warn("guard state unwritable", slog.String("component", "guard"), slog.Any("err", err))
		return
	}
	if err := os.Rename(tmp, g.path); err != nil {
		slog.Warn("guard state not renamed", slog.String("component", "guard"), slog.Any("err", err))
	}
}

// Feed is one upstream as the SIGNAL panel shows it.
type Feed struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Age   string `json:"age"`

	// Calls taken out of this hour's budget, and the budget itself, so the
	// panel answers whether anything here is being hammered rather than
	// leaving it to be worked out from the poll intervals in the source.
	Used   int `json:"used"`
	Budget int `json:"budget"`
	Load   int `json:"load"`
}

// Feeds reports every upstream this site reads, in a fixed order so the panel
// does not reshuffle between polls the way a map range would.
var feedOrder = []struct{ key, label string }{
	{"yahoo", "YAHOO"},
	{"algolia", "HN"},
	{"lobsters", "LOBSTERS"},
	{"npr", "NPR"},
	{"bbc", "BBC"},
	{"nasdaq", "NASDAQ"},
	{"openmeteo", "OPEN-METEO"},
	{"nws", "NWS"},
	{"pollen", "POLLEN"},
	{"steam", "STEAM"},
	{"justwatch", "JUSTWATCH"},
	{"logging", "LOGGING"},
}

func (g *Guard) Feeds(now time.Time) []Feed {
	order := feedOrder

	g.mu.Lock()
	defer g.mu.Unlock()

	feeds := make([]Feed, 0, len(order))
	for _, o := range order {
		f := Feed{Name: o.label, State: "idle", Age: "--"}
		if b, ok := budgets[o.key]; ok {
			f.Budget = b.perHour
		}
		if e, ok := g.entries[o.key]; ok {
			// The window rolls, so a count from a window that has already
			// expired is not this hour's usage.
			if now.Sub(e.WindowStart) < time.Hour {
				f.Used = e.Count
				if f.Budget > 0 {
					f.Load = f.Used * 100 / f.Budget
				}
			}
			switch {
			case now.Before(e.OpenUntil):
				f.State = "open"
			case e.Fails > 0:
				f.State = "degraded"
			default:
				f.State = "ok"
			}
			if !e.LastCall.IsZero() {
				f.Age = shortAge(now.Sub(e.LastCall))
			}
		}
		feeds = append(feeds, f)
	}
	return feeds
}

func shortAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// Status is what the footer shows: which endpoints are currently shut off.
func (g *Guard) Status() []string {
	now := time.Now()

	g.mu.Lock()
	defer g.mu.Unlock()

	var open []string
	for name, e := range g.entries {
		if now.Before(e.OpenUntil) {
			open = append(open, name)
		}
	}
	return open
}
