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

// The two rules this site alerts on, and the reasoning for why they are these
// two and not the obvious one.
//
// The obvious rule is an error rate threshold, and it is the wrong rule here.
// The database holds zero ERROR and zero WARN records across five sites: at
// roughly a thousand views a month any threshold either fires constantly or
// never fires at all, and a rule that never fires is indistinguishable from a
// rule that is broken.
//
// A third candidate was considered and rejected outright. `cf_ray` being absent
// means a request reached the origin without passing Cloudflare, which sounds
// like exactly the alert this data supports and is already fully instrumented.
// It cannot be used: every container health check and every ingest POST is a
// loopback or bridge request with no cf_ray, which is about 11,500 records a
// day. The signal is real and the rule would be pure noise.
//
// What is left is what these sites can actually go wrong at:
//
//  1. Silence. A source stops shipping. Every site here logs its own health
//     check every thirty seconds, so a healthy source is never quiet for long
//     regardless of how little public traffic it gets, and that heartbeat is
//     what makes silence a reliable signal at this traffic rather than a
//     reflection of how boring a Tuesday was.
//
//  2. An unclean restart. web/server.go logs "listening" on start and
//     "shutting down" on a signal, so a start with no preceding stop means the
//     previous process did not exit on a signal. status cannot see this from
//     outside, and by the time anyone looks `docker logs` has rotated it away.
//
// This deliberately overlaps with status rather than avoiding it. status says
// the site is unreachable from the internet; this says the process stopped
// talking. A container that is up, answering its probes and silently failing to
// ship is invisible to status and loud here.

const (
	// How long a source may be quiet before it is called silent. The heartbeat
	// is one health check every thirty seconds plus a shipper that flushes
	// every five, so a healthy source is seen roughly every thirty-five
	// seconds and this is about nine missed beats. Short enough to be useful,
	// long enough that a slow deploy is not an outage.
	silenceAfter = 5 * time.Minute

	// How often the rule is evaluated. Cheap: it walks a map of five entries.
	watchInterval = 30 * time.Second

	// Nothing alerts for this long after the process starts, and the reason is
	// specific rather than defensive. When this site restarts, every other
	// site's shipper is dropping records into a refused connection, so the
	// record stream has a genuine hole in it either side of a deploy. Both
	// rules read false in that window: a source looks silent because nothing
	// could be delivered, and a site restarted during the hole arrives with
	// its "shutting down" record lost and its "listening" looking unclean.
	// `make up` restarts this site and then the other four in sequence, which
	// is exactly that shape.
	startupGrace = 5 * time.Minute

	// How far back to look for sources at startup, so a fresh process knows
	// what it is meant to be hearing from before anything has shipped to it
	// yet. A source that stops existing ages out of this window rather than
	// alerting forever.
	knownSourceWindow = 24 * time.Hour

	// Lifecycle events between the ingest path and the watchdog goroutine.
	// Deep enough for every site restarting at once several times over, and a
	// non-blocking send either way: nothing on the write path waits on an
	// alert.
	lifecycleQueue = 64

	// The two messages web/server.go logs, matched by exact text. They are the
	// contract this rule reads, which is worth saying out loud: renaming
	// either string in web/server.go silently disables restart detection, and
	// there is a test asserting these constants match what that file logs.
	msgListening = "listening"
	msgShutdown  = "shutting down"
)

// lifecycleEvent is one process transition observed in the record stream.
type lifecycleEvent struct {
	source string
	up     bool
	at     time.Time
}

// Watchdog holds what has been heard from whom, and evaluates the two rules.
//
// Liveness is tracked in memory at ingest time rather than queried out of the
// database, and that is the load-bearing decision in this file. A query would
// have to read `records`, and health check lines are demoted to rollup-only and
// never land there (see toRow); at this traffic a perfectly healthy site would
// look silent for hours at a stretch. `rollups` does count them, but only to
// the hour, so the earliest a query could notice anything is an hour late. A
// map updated as the batch arrives is exact, costs a mutex, and notices in
// thirty seconds.
type Watchdog struct {
	db     *sql.DB
	notify func(kind string, ctx AlertContext)
	now    func() time.Time

	events chan lifecycleEvent

	mu sync.Mutex
	// Last time anything at all arrived from a source.
	seen map[string]time.Time
	// Sources currently being reported as silent, and the moment they went
	// quiet, so the recovery message can say how long the gap actually was.
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

// Observe is called with every batch that reaches the writer, from the HTTP
// endpoint and from this site's own local sink alike. It never blocks and never
// touches the database.
//
// It is deliberately called with the batch that was accepted rather than with
// each row: one mutex acquisition per POST, not per record.
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

// recordTime prefers the timestamp the record carries and falls back to now,
// because a record with a nonsense clock should still count as a transition
// rather than being silently ignored.
func recordTime(rec web.Record, fallback time.Time) time.Time {
	if rec.Time <= 0 {
		return fallback
	}
	return time.UnixMilli(rec.Time)
}

// enqueue drops rather than blocks, matching every other queue in this repo.
// Losing a lifecycle event costs one alert; blocking the ingest path costs the
// sites this one is meant to be watching.
func (w *Watchdog) enqueue(ev lifecycleEvent) {
	select {
	case w.events <- ev:
	default:
	}
}

// Bootstrap seeds the source list from what has shipped recently, so a process
// that has just started knows who it is waiting to hear from before anything
// has arrived. Without it, a site that is already down when this one starts is
// never noticed, because a source nobody has heard from is not in the map.
//
// `rollups` rather than `records`, for the same reason the rest of this file
// reads memory rather than the raw table: health checks are counted there and
// only there, so a quiet site is still present.
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
		// Seeded as seen now rather than at its real last-seen time: this
		// process has only just started and cannot tell the difference between
		// a site that is down and one whose records it simply was not running
		// to receive. Whichever it is, silenceAfter settles it.
		w.seen[source] = start
	}
	w.mu.Unlock()

	slog.Info("watchdog watching",
		slog.String("component", "watchdog"),
		slog.Int("sources", len(found)),
		slog.String("silence_after", silenceAfter.String()))
	return nil
}

// Run evaluates the silence rule on a ticker and handles lifecycle events as
// they arrive, in one goroutine so neither needs a lock against the other.
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
// An alert that repeats every thirty seconds for a site that is down overnight
// teaches its own reader to swipe it away.
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

	// Published outside the lock. A publish takes up to ntfyTimeout, and
	// holding the mutex that the ingest path takes for that long would be a
	// logging site pausing the sites it watches to complain about one of them.
	for _, f := range fire {
		w.notify(f.kind, AlertContext{Source: f.source, Silent: f.gap})
	}
}

// handleLifecycle applies the restart rule. State lives in the `meta` table
// rather than in memory because the question it answers spans restarts of this
// process: a crash noticed only after a deploy is still a crash.
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
	// The first "listening" this site ever sees from a source has nothing to
	// compare against and says nothing.
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

// The meta value is "up|<unix millis>" or "down|<unix millis>": the state, and
// when it was recorded. One column, parsed in one place, so a lifecycle row
// stays as readable in a sqlite3 shell as it is here.
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
