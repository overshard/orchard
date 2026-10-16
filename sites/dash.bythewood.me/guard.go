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

	"dash.bythewood.me/guard"
)

// Every upstream is free, keyless and someone else's, and a ban isn't fixed by
// a retry, so each gets an hourly budget, a pace and a breaker. The state is on
// disk, or a restart loop would hammer an endpoint that just answered 429.

var errGuardOpen = errors.New("circuit open")

// Budgets are per rolling hour and are ceilings to fail against, not targets.
// The pollers ask for far less: markets at 30s is 120/hr, and everything else
// is minutes apart.
type budget struct {
	perHour int
	pace    time.Duration
}

var budgets = map[string]budget{
	"yahoo":    {perHour: 900, pace: 2 * time.Second},
	"algolia":  {perHour: 120, pace: 2 * time.Second},
	"lobsters": {perHour: 120, pace: 2 * time.Second},
	// A request per comment, three on every row as it turns over and eight on
	// each row at a brief slot, which is about 120 calls in the slot's hour.
	"hnitem": {perHour: 300, pace: 250 * time.Millisecond},
	// The pages behind the feed rows and TODAY's sources, read at each brief
	// slot for the TL;DRs, about sixty and every one a different site.
	"articles":  {perHour: 150, pace: 300 * time.Millisecond},
	"openmeteo": {perHour: 120, pace: 2 * time.Second},
	"npr":       {perHour: 120, pace: 2 * time.Second},
	"bbc":       {perHour: 60, pace: 2 * time.Second},
	// The brief feeds are read four times a day, a few requests each.
	"googlenews": {perHour: 30, pace: 2 * time.Second},
	"dowjones":   {perHour: 30, pace: 2 * time.Second},
	"thehill":    {perHour: 30, pace: 2 * time.Second},
	"newsnation": {perHour: 30, pace: 2 * time.Second},
	"pbs":        {perHour: 30, pace: 2 * time.Second},
	"cbs":        {perHour: 30, pace: 2 * time.Second},
	"fox":        {perHour: 30, pace: 2 * time.Second},
	"examiner":   {perHour: 30, pace: 2 * time.Second},
	"cnbc":       {perHour: 30, pace: 2 * time.Second},
	"bls":        {perHour: 10, pace: 2 * time.Second},
	"bea":        {perHour: 10, pace: 2 * time.Second},
	"nasdaq":     {perHour: 120, pace: 2 * time.Second},
	"nws":        {perHour: 120, pace: 2 * time.Second},
	"pollen":     {perHour: 60, pace: 2 * time.Second},
	"steam":      {perHour: 240, pace: time.Second},
	"justwatch":  {perHour: 60, pace: 2 * time.Second},
	"youtube":    {perHour: 90, pace: 2 * time.Second},
	"twitch":     {perHour: 90, pace: 2 * time.Second},
	// The same Twitch endpoint as above, on its own key so the hourly archive
	// read cannot spend or trip what the live banner needs.
	"twitchvods": {perHour: 30, pace: 2 * time.Second},
	// No pacing on these. They go to Isaac's own machine and the strip fires
	// every probe at once, so a pause between them would only refuse all but
	// the first.
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

// policy is an upstream's budget and pace, with the same breaker for every
// one. A name with no budget still gets the breaker, and Take refuses it.
func policy(name string) (guard.Policy, bool) {
	b, ok := budgets[name]
	return guard.Policy{
		Gap:       b.pace,
		Budget:    b.perHour,
		Window:    time.Hour,
		TripAfter: failsToTrip,
		OpenFor:   minBackoff,
		MaxOpen:   maxBackoff,
	}, ok
}

// guardEntry is one upstream in guard.json. Its shape is what the last deploy
// wrote, so it can't change without a restart forgetting every open breaker.
type guardEntry struct {
	WindowStart time.Time     `json:"window_start"`
	Count       int           `json:"count"`
	Fails       int           `json:"fails"`
	LastCall    time.Time     `json:"last_call"`
	OpenUntil   time.Time     `json:"open_until"`
	Backoff     time.Duration `json:"backoff"`
}

// fileStore holds every upstream's state in memory and writes it to disk on a
// timer, so a 30 second poll isn't a write per tick.
type fileStore struct {
	mu      sync.Mutex
	entries map[string]*guardEntry
	dirty   bool
}

func (f *fileStore) entry(name string) *guardEntry {
	e, ok := f.entries[name]
	if !ok {
		e = &guardEntry{}
		f.entries[name] = e
	}
	return e
}

// Load makes an entry for a name it hasn't seen, so the panel shows an upstream
// that was asked about as ok instead of idle.
func (f *fileStore) Load(name string) (guard.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.entry(name)
	return guard.State{
		LastCall:    e.LastCall,
		WindowStart: e.WindowStart,
		Count:       e.Count,
		Fails:       e.Fails,
		Trips:       tripsFor(e.Backoff),
		OpenUntil:   e.OpenUntil,
	}, nil
}

func (f *fileStore) Save(name string, s guard.State, _ string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := guardEntry{
		WindowStart: s.WindowStart,
		Count:       s.Count,
		Fails:       s.Fails,
		LastCall:    s.LastCall,
		OpenUntil:   s.OpenUntil,
	}
	if s.Trips > 0 {
		next.Backoff = guard.Backoff(minBackoff, maxBackoff, s.Trips)
	}
	e := f.entry(name)
	// A window rolling over with nothing in it waits for the next real write.
	rolled := next.Count == 0 && next.Fails == e.Fails && next.Backoff == e.Backoff &&
		next.LastCall.Equal(e.LastCall) && next.OpenUntil.Equal(e.OpenUntil)
	if !rolled {
		f.dirty = true
	}
	*e = next
}

// tripsFor turns a backoff on disk back into the opens that doubled it there.
func tripsFor(backoff time.Duration) int {
	if backoff <= 0 {
		return 0
	}
	n := 1
	for d := minBackoff; d < backoff && d < maxBackoff; d = guard.Backoff(minBackoff, maxBackoff, n) {
		n++
	}
	return n
}

// Guard is safe for concurrent use. Every poller shares one.
type Guard struct {
	guard *guard.Guard
	file  *fileStore
	path  string
}

func NewGuard(dataDir string) *Guard {
	f := &fileStore{entries: map[string]*guardEntry{}}
	g := &Guard{
		guard: guard.New(f, policy),
		file:  f,
		path:  filepath.Join(dataDir, "guard.json"),
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
	if err := json.Unmarshal(b, &f.entries); err != nil {
		slog.Warn("guard state unparseable, starting closed",
			slog.String("component", "guard"), slog.Any("err", err))
		f.entries = map[string]*guardEntry{}
	}
	return g
}

// Allow reports whether a call may go out now and reserves a slot when it may.
// A pace is a refusal, which suits a probe, and anything on a timer calls
// Reserve, or pollers sharing an endpoint at boot each lose a whole tick.
func (g *Guard) Allow(name string) error {
	wait, err := g.take(name)
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
		wait, err := g.take(name)
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

// take words the guard's refusals the way the rest of dash reads them.
func (g *Guard) take(name string) (time.Duration, error) {
	wait, err := g.guard.Take(name, time.Now())
	var open *guard.OpenError
	var spent *guard.SpentError
	switch {
	case errors.As(err, &open):
		return 0, fmt.Errorf("%s: %w for another %s", name, errGuardOpen, open.Left.Round(time.Second))
	case errors.As(err, &spent):
		return 0, fmt.Errorf("%s: hourly budget of %d spent", name, spent.Budget)
	case errors.Is(err, guard.ErrUnknown):
		return 0, fmt.Errorf("%s: no budget defined", name)
	}
	return wait, err
}

// Succeed closes the breaker and clears the backoff.
func (g *Guard) Succeed(name string) { g.guard.Succeed(name) }

// Fail records an unsuccessful call. status is the HTTP status, or 0 for a
// transport error. 429 and 503 open the breaker at once, since they're the
// endpoint saying so instead of the network being bad.
func (g *Guard) Fail(name string, status int, retryAfter time.Duration) {
	kind := guard.Failed
	if status == 429 || status == 503 {
		kind = guard.Refused
	}
	wait := g.guard.Fail(name, time.Now(), guard.Failure{Kind: kind, RetryAfter: retryAfter})
	if wait == 0 {
		return
	}
	slog.Warn("upstream breaker opened",
		slog.String("component", "guard"),
		slog.String("endpoint", name),
		slog.Int("status", status),
		slog.String("for", wait.String()))
}

// Flush writes the state out when it has changed.
func (g *Guard) Flush() {
	g.file.mu.Lock()
	if !g.file.dirty {
		g.file.mu.Unlock()
		return
	}
	b, err := json.Marshal(g.file.entries)
	g.file.dirty = false
	g.file.mu.Unlock()

	if err != nil {
		slog.Warn("guard state unmarshalable", slog.String("component", "guard"), slog.Any("err", err))
		return
	}
	// Rename onto the real path so a crash mid-write leaves the old state
	// instead of a truncated file the next boot would refuse to parse.
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
	// panel says whether anything here is being hammered.
	Used   int `json:"used"`
	Budget int `json:"budget"`
	Load   int `json:"load"`
}

// Feeds reports every upstream this site reads, in a fixed order so the panel
// does not reshuffle between polls the way a map range would.
var feedOrder = []struct{ key, label string }{
	{"yahoo", "YAHOO"},
	{"algolia", "HN"},
	{"hnitem", "HN THREADS"},
	{"lobsters", "LOBSTERS"},
	{"articles", "ARTICLES"},
	{"npr", "NPR"},
	{"bbc", "BBC"},
	{"googlenews", "AP / REUTERS"},
	{"dowjones", "WSJ / MW"},
	{"thehill", "THE HILL"},
	{"newsnation", "NEWSNATION"},
	{"pbs", "PBS"},
	{"cbs", "CBS"},
	{"fox", "FOX"},
	{"examiner", "EXAMINER"},
	{"cnbc", "CNBC"},
	{"bls", "BLS"},
	{"bea", "BEA"},
	{"nasdaq", "NASDAQ"},
	{"openmeteo", "OPEN-METEO"},
	{"nws", "NWS"},
	{"pollen", "POLLEN"},
	{"steam", "STEAM"},
	{"justwatch", "JUSTWATCH"},
	{"youtube", "YOUTUBE"},
	{"twitch", "TWITCH"},
	{"twitchvods", "TWITCH VODS"},
	{"logging", "LOGGING"},
}

func (g *Guard) Feeds(now time.Time) []Feed {
	order := feedOrder

	g.file.mu.Lock()
	defer g.file.mu.Unlock()

	feeds := make([]Feed, 0, len(order))
	for _, o := range order {
		f := Feed{Name: o.label, State: "idle", Age: "--"}
		if b, ok := budgets[o.key]; ok {
			f.Budget = b.perHour
		}
		if e, ok := g.file.entries[o.key]; ok {
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

	g.file.mu.Lock()
	defer g.file.mu.Unlock()

	var open []string
	for name, e := range g.file.entries {
		if now.Before(e.OpenUntil) {
			open = append(open, name)
		}
	}
	return open
}
