package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"logging.bythewood.me/web"
)

// The rules are a source going silent, a start with no preceding shutdown, a
// burst of ERROR records from one source and the data disk running low. A steady
// error rate isn't one, since these sites log almost no ERROR records at all.

const (
	// A healthy source is seen roughly every thirty-five seconds, from its own
	// health check plus the shipper flush, so this is about nine missed beats.
	silenceAfter = 5 * time.Minute

	// caddy has no self probe, and its heartbeat is status checking the public
	// hostnames every three minutes. Five minutes there is under two beats, so
	// one missed cycle during a status deploy would fire.
	caddySilenceAfter = 15 * time.Minute

	watchInterval = 30 * time.Second

	// A burst is errorBurst ERROR records from one source inside errorWindow,
	// told at most once per errorRepeat so a stuck loop isn't a page a minute.
	errorBurst  = 10
	errorWindow = 5 * time.Minute
	errorRepeat = time.Hour

	// The volume shares the host filesystem with Docker's data root, so this is
	// also the room every other container has to write in.
	diskLowFraction = 0.15
	diskLowBytes    = 3 << 30

	// Nothing alerts for this long after start. While this site is restarting,
	// every other site's shipper is dropping into a refused connection, so both
	// rules read false either side of a deploy.
	startupGrace = 5 * time.Minute

	// How far back Bootstrap looks for sources. A source that stops existing
	// ages out of this window instead of alerting forever.
	knownSourceWindow = 24 * time.Hour

	// Lifecycle events between the ingest path and the watchdog goroutine.
	lifecycleQueue = 64

	// Matched against web/server.go by exact text, so renaming either string
	// there silently disables restart detection. A test asserts they still match.
	msgListening = "listening"
	msgShutdown  = "shutting down"
)

// heartbeats are sources that report a job finishing rather than a process
// running, each with how long it may go quiet. A backup runs once a day.
var heartbeats = map[string]time.Duration{
	"backup": 26 * time.Hour,
}

type lifecycleEvent struct {
	source string
	up     bool
	at     time.Time
}

// Watchdog tracks liveness in memory at ingest time, since health check lines
// never reach records and rollups are hour-granular, so a query couldn't notice
// anything less than an hour late.
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
	// The arrival times of each source's latest ERROR records, at most errorBurst.
	errs      map[string][]time.Time
	burstSent map[string]time.Time

	// diskPath is statted every tick, and an empty one skips the disk rule.
	diskPath string
	statDisk func(path string) (free, total uint64, err error)
	diskLow  bool

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
		errs:       map[string][]time.Time{},
		burstSent:  map[string]time.Time{},
		statDisk:   statfs,
		startedAt:  time.Now(),
	}
}

// Observe records that a batch arrived from source. It never blocks, never
// touches the database, and is safe on a nil Watchdog. Called per batch instead
// of per record so it costs one mutex acquisition per POST.
func (w *Watchdog) Observe(source string, records []web.Record) {
	if w == nil || len(records) == 0 {
		return
	}
	now := w.now()

	w.mu.Lock()
	w.seen[source] = now
	for _, rec := range records {
		if rec.Level == "ERROR" {
			e := append(w.errs[source], now)
			if len(e) > errorBurst {
				e = e[len(e)-errorBurst:]
			}
			w.errs[source] = e
		}
	}
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

// Shipping is whether a source has sent anything inside the silence window.
func (w *Watchdog) Shipping(source string) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	at, ok := w.seen[source]
	return ok && w.now().Sub(at) < silenceAfter
}

// recordTime falls back to now, so a record with a broken clock still counts as
// a transition.
func recordTime(rec web.Record, fallback time.Time) time.Time {
	if rec.Time <= 0 {
		return fallback
	}
	return time.UnixMilli(rec.Time)
}

// enqueue drops instead of blocking, since losing a lifecycle event costs one
// alert and blocking the ingest path costs the sites this one watches.
func (w *Watchdog) enqueue(ev lifecycleEvent) {
	select {
	case w.events <- ev:
	default:
	}
}

// Bootstrap seeds the source list, so a site already down when this process
// starts is still noticed. It reads rollups instead of records because health
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

	beats, err := w.lastHeartbeats(ctx, start)
	if err != nil {
		return err
	}

	w.mu.Lock()
	for _, source := range found {
		// Seeded as seen now, since this process can't tell a site that's down
		// from one it wasn't running to receive. silenceAfter settles it.
		w.seen[source] = start
	}
	for source, at := range beats {
		w.seen[source] = at
	}
	w.mu.Unlock()

	slog.Info("watchdog watching",
		slog.String("component", "watchdog"),
		slog.Int("sources", len(found)),
		slog.String("silence_after", silenceAfter.String()))
	return nil
}

// lastHeartbeats reads when each heartbeat source last arrived, since one that
// pings once a day is older than knownSourceWindow for most of it. A heartbeat
// never heard is seeded as now, so a job that never runs is reported too.
func (w *Watchdog) lastHeartbeats(ctx context.Context, start time.Time) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for source, limit := range heartbeats {
		var hour sql.NullInt64
		err := w.db.QueryRowContext(ctx,
			`SELECT MAX(hour) FROM rollups WHERE source = ? AND hour >= ?`,
			source, hourFloor(start.Add(-2*limit).UnixMilli())).Scan(&hour)
		if err != nil {
			return nil, err
		}
		at := start
		if hour.Valid {
			// The end of the rollup hour, which can only report it late, never early.
			at = time.UnixMilli(hour.Int64).Add(time.Hour)
			if at.After(start) {
				at = start
			}
		}
		out[source] = at
	}
	return out, nil
}

// Run evaluates the silence rule on a ticker and handles lifecycle events in
// one goroutine, and returns when ctx is cancelled.
func (w *Watchdog) Run(ctx context.Context) {
	t := time.NewTicker(watchInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			w.drain(context.WithoutCancel(ctx))
			return
		case ev := <-w.events:
			w.handleLifecycle(context.WithoutCancel(ctx), ev)
		case <-t.C:
			w.checkSilence()
			w.checkErrors()
			w.checkDisk()
		}
	}
}

// drain records the lifecycle events already queued. On shutdown that includes
// this site's own, or the next start finds it still marked up.
func (w *Watchdog) drain(ctx context.Context) {
	for {
		select {
		case ev := <-w.events:
			w.handleLifecycle(ctx, ev)
		default:
			return
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
		limit := silenceFor(source)
		quiet, isQuiet := w.quietSince[source]

		switch {
		case gap > limit && !isQuiet:
			w.quietSince[source] = last
			fire = append(fire, transition{"silence", source, gap})
		case gap <= limit && isQuiet:
			delete(w.quietSince, source)
			fire = append(fire, transition{"resumed", source, last.Sub(quiet)})
		}
	}
	w.mu.Unlock()

	// Outside the lock, since a publish takes up to ntfy.Timeout and the ingest
	// path takes the same mutex.
	for _, f := range fire {
		w.notify(f.kind, AlertContext{Source: f.source, Silent: f.gap})
	}
}

// silenceFor is how long a source may go quiet before it is reported, which
// depends on what its heartbeat is and not on the source itself.
func silenceFor(source string) time.Duration {
	if limit, ok := heartbeats[source]; ok {
		return limit
	}
	if source == caddySource {
		return caddySilenceAfter
	}
	return silenceAfter
}

// checkErrors reports a source whose last errorBurst ERROR records all arrived
// inside errorWindow.
func (w *Watchdog) checkErrors() {
	now := w.now()
	if now.Sub(w.startedAt) < startupGrace {
		return
	}

	var fire []string
	w.mu.Lock()
	for source, e := range w.errs {
		if len(e) < errorBurst || now.Sub(e[0]) > errorWindow {
			continue
		}
		if sent, ok := w.burstSent[source]; ok && now.Sub(sent) < errorRepeat {
			continue
		}
		w.burstSent[source] = now
		fire = append(fire, source)
	}
	w.mu.Unlock()

	for _, source := range fire {
		w.notify("errors", AlertContext{Source: source,
			Detail: fmt.Sprintf("%d or more ERROR records in %s.", errorBurst, humanDuration(errorWindow))})
	}
}

// checkDisk reports once when free space drops under the floor and re-arms
// once it's back above it.
func (w *Watchdog) checkDisk() {
	if w.diskPath == "" {
		return
	}
	free, total, err := w.statDisk(w.diskPath)
	if err != nil || total == 0 {
		return
	}
	low := diskIsLow(free, total)
	if low == w.diskLow {
		return
	}
	w.diskLow = low
	if low {
		w.notify("disk", AlertContext{Source: selfSource,
			Detail: fmt.Sprintf("%s free of %s (%.0f%%).", humanBytes(free), humanBytes(total), 100*float64(free)/float64(total))})
	}
}

func diskIsLow(free, total uint64) bool {
	return free < diskLowBytes || float64(free) < diskLowFraction*float64(total)
}

func statfs(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}

func humanBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%dM", n>>20)
	default:
		return fmt.Sprintf("%dK", n>>10)
	}
}

// Disk is the data volume's free and total bytes, zero when it can't be read.
func (w *Watchdog) Disk() (free, total uint64) {
	if w == nil || w.diskPath == "" {
		return 0, 0
	}
	free, total, err := w.statDisk(w.diskPath)
	if err != nil {
		return 0, 0
	}
	return free, total
}

// handleLifecycle applies the restart rule. State lives in meta instead of in
// memory because the question spans restarts of this process. caddy logs
// neither message, so only the silence rule ever covers the edge.
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
	// This site dying uncleanly almost always means the whole host went down,
	// which is exactly the restart the grace below would hide.
	if ev.source == selfSource {
		w.notify("crash", AlertContext{Source: ev.source, Silent: w.outage(ctx, ev.at)})
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

// outage is how long nothing was written before start, zero when unknown.
func (w *Watchdog) outage(ctx context.Context, start time.Time) time.Duration {
	var last sql.NullInt64
	err := w.db.QueryRowContext(ctx,
		`SELECT MAX(ts) FROM records WHERE ts < ?`, start.UnixMilli()).Scan(&last)
	if err != nil || !last.Valid {
		return 0
	}
	return start.Sub(time.UnixMilli(last.Int64))
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

// HeardWithin reports whether anything arrived from source in the last d. It
// counts caddy's rollup only lines, which never reach the records table.
func (w *Watchdog) HeardWithin(source string, d time.Duration) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	last, ok := w.seen[source]
	return ok && w.now().Sub(last) <= d
}

func (s *site) markHeard(rows []SourceRow) {
	for i := range rows {
		if !rows[i].IsLive && s.watchdog.HeardWithin(rows[i].Source, max(15*time.Minute, silenceFor(rows[i].Source))) {
			rows[i].IsLive = true
		}
	}
}
