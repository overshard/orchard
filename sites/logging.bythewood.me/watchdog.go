package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"logging.bythewood.me/web"
)

// Two rules: a source going silent, and a start with no preceding shutdown.
// An error rate threshold is not one of them, because these sites log almost no
// ERROR or WARN records and a rule that never fires cannot be told from a broken one.

const (
	// A healthy source is seen roughly every thirty-five seconds, from its own
	// health check plus the shipper flush, so this is about nine missed beats.
	silenceAfter = 5 * time.Minute

	watchInterval = 30 * time.Second

	// Nothing alerts for this long after start. While this site is restarting,
	// every other site's shipper is dropping into a refused connection, so both
	// rules read false either side of a deploy.
	startupGrace = 5 * time.Minute

	// How far back Bootstrap looks for sources. A source that stops existing
	// ages out of this window rather than alerting forever.
	knownSourceWindow = 24 * time.Hour

	// Lifecycle events between the ingest path and the watchdog goroutine.
	lifecycleQueue = 64

	// Matched against web/server.go by exact text: renaming either string there
	// silently disables restart detection. A test asserts they still match.
	msgListening = "listening"
	msgShutdown  = "shutting down"
)

type lifecycleEvent struct {
	source string
	up     bool
	at     time.Time
}

// Watchdog tracks liveness in memory at ingest time rather than by query:
// health check lines are rollup-only and never reach records, and rollups are
// hour-granular, so a query could not notice anything less than an hour late.
type Watchdog struct {
	db     *sql.DB
	notify func(kind string, ctx AlertContext)
	now    func() time.Time

	events chan lifecycleEvent

	mu sync.Mutex
	// Last time anything arrived from a source.
	seen map[string]time.Time
	// Sources currently reported silent, and when they went quiet, so the
	// recovery message can state the gap.
	quietSince map[string]time.Time

	startedAt time.Time
}

func NewWatchdog(db *sql.DB, notify func(kind string, ctx AlertContext)) *Watchdog {
	return &Watchdog{
		db:         db,
		notify:     notify,
		now:        time.Now,
		events:     make(chan lifecycleEvent, lifecycleQueue),
		seen:       map[string]time.Time{},
		quietSince: map[string]time.Time{},
		startedAt:  time.Now(),
	}
}

// Observe records that a batch arrived from source. It never blocks, never
// touches the database, and is safe on a nil Watchdog. Called per batch rather
// than per record so it costs one mutex acquisition per POST.
func (w *Watchdog) Observe(source string, records []web.Record) {
	if w == nil || len(records) == 0 {
		return
	}
	now := w.now()

	w.mu.Lock()
	w.seen[source] = now
	w.mu.Unlock()

	for _, rec := range records {
		switch rec.Msg {
		case msgListening:
			w.enqueue(lifecycleEvent{source: source, up: true, at: recordTime(rec, now)})
		case msgShutdown:
			w.enqueue(lifecycleEvent{source: source, up: false, at: recordTime(rec, now)})
		}
	}
}

// recordTime falls back to now, so a record with a broken clock still counts as
// a transition rather than being ignored.
func recordTime(rec web.Record, fallback time.Time) time.Time {
	if rec.Time <= 0 {
		return fallback
	}
	return time.UnixMilli(rec.Time)
}

// enqueue drops rather than blocks: losing a lifecycle event costs one alert,
// blocking the ingest path costs the sites this one watches.
func (w *Watchdog) enqueue(ev lifecycleEvent) {
	select {
	case w.events <- ev:
	default:
	}
}

// Bootstrap seeds the source list, so a site already down when this process
// starts is still noticed. It reads rollups rather than records because health
// checks are counted there and only there.
func (w *Watchdog) Bootstrap(ctx context.Context) error {
	cutoff := hourFloor(w.now().Add(-knownSourceWindow).UnixMilli())
	rows, err := w.db.QueryContext(ctx,
		`SELECT DISTINCT source FROM rollups WHERE hour >= ?`, cutoff)
	if err != nil {
		return err
	}
	defer rows.Close()

	start := w.now()
	var found []string
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return err
		}
		found = append(found, source)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	w.mu.Lock()
	for _, source := range found {
		// Seeded as seen now: this process cannot tell a site that is down
		// from one it was not running to receive. silenceAfter settles it.
		w.seen[source] = start
	}
	w.mu.Unlock()

	slog.Info("watchdog watching",
		slog.String("component", "watchdog"),
		slog.Int("sources", len(found)),
		slog.String("silence_after", silenceAfter.String()))
	return nil
}

// Run evaluates the silence rule on a ticker and handles lifecycle events in
// one goroutine, and returns when ctx is cancelled.
func (w *Watchdog) Run(ctx context.Context) {
	t := time.NewTicker(watchInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-w.events:
			w.handleLifecycle(ctx, ev)
		case <-t.C:
			w.checkSilence()
		}
	}
}

// checkSilence fires once per transition in each direction, never repeatedly.
func (w *Watchdog) checkSilence() {
	now := w.now()
	if now.Sub(w.startedAt) < startupGrace {
		return
	}

	type transition struct {
		kind   string
		source string
		gap    time.Duration
	}
	var fire []transition

	w.mu.Lock()
	for source, last := range w.seen {
		gap := now.Sub(last)
		quiet, isQuiet := w.quietSince[source]

		switch {
		case gap > silenceAfter && !isQuiet:
			w.quietSince[source] = last
			fire = append(fire, transition{"silence", source, gap})
		case gap <= silenceAfter && isQuiet:
			delete(w.quietSince, source)
			fire = append(fire, transition{"resumed", source, last.Sub(quiet)})
		}
	}
	w.mu.Unlock()

	// Outside the lock: a publish takes up to ntfyTimeout, and the ingest path
	// takes the same mutex.
	for _, f := range fire {
		w.notify(f.kind, AlertContext{Source: f.source, Silent: f.gap})
	}
}

// handleLifecycle applies the restart rule. State lives in meta rather than in
// memory because the question spans restarts of this process.
func (w *Watchdog) handleLifecycle(ctx context.Context, ev lifecycleEvent) {
	prevUp, prevAt, known := w.readLifecycle(ctx, ev.source)

	if err := w.writeLifecycle(ctx, ev); err != nil {
		slog.Error("watchdog: recording lifecycle state failed",
			slog.String("component", "watchdog"),
			slog.String("source", ev.source),
			slog.Any("err", err))
		return
	}

	// Only a start can be unclean, and only against a known previous start.
	if !ev.up || !known || !prevUp {
		return
	}
	if w.now().Sub(w.startedAt) < startupGrace {
		return
	}

	detail := "No previous start recorded."
	if !prevAt.IsZero() {
		detail = fmt.Sprintf("Previous start was %s ago.", humanDuration(ev.at.Sub(prevAt)))
	}
	w.notify("restart", AlertContext{Source: ev.source, Detail: detail})
}

// The meta value is "up|<unix millis>" or "down|<unix millis>".
func lifecycleKey(source string) string { return "lifecycle:" + source }

func (w *Watchdog) readLifecycle(ctx context.Context, source string) (up bool, at time.Time, known bool) {
	var value string
	err := w.db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key = ?`, lifecycleKey(source)).Scan(&value)
	if err != nil {
		return false, time.Time{}, false
	}

	state, stamp, _ := strings.Cut(value, "|")
	if ms, err := strconv.ParseInt(stamp, 10, 64); err == nil && ms > 0 {
		at = time.UnixMilli(ms)
	}
	return state == "up", at, true
}

func (w *Watchdog) writeLifecycle(ctx context.Context, ev lifecycleEvent) error {
	state := "down"
	if ev.up {
		state = "up"
	}
	_, err := w.db.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		lifecycleKey(ev.source), state+"|"+strconv.FormatInt(ev.at.UnixMilli(), 10))
	return err
}
