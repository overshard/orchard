package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"logging.bythewood.me/web"
)

// The write path.
//
// Every record arrives on one channel and one goroutine drains it into batched
// transactions. That is the whole concurrency story, and it is deliberate:
// SQLite takes a database-wide write lock, so a second writer buys SQLITE_BUSY
// rather than throughput. One writer means the lock is never contended and a
// batch of five hundred rows costs one fsync instead of five hundred.

const (
	// Queue depth between the HTTP handlers and the flusher. Deep enough to
	// absorb five sites flushing at the same instant, shallow enough that a
	// wedged writer is noticed in seconds rather than after eating a
	// gigabyte.
	writeQueue = 16384

	// Flush on whichever comes first. A quarter second is short enough that
	// the dashboard is never visibly behind and long enough that a busy
	// second is a handful of transactions.
	writeBatch = 500
	writeEvery = 250 * time.Millisecond

	// Caps on one request. maxBody is the second fence behind Caddy's own;
	// maxRecords stops a single well-formed body from claiming the whole
	// queue.
	maxBody    = 4 << 20
	maxRecords = 2000
)

// Writer owns the queue and the goroutine that drains it.
type Writer struct {
	db *sql.DB
	ch chan row

	quit chan struct{}
	done chan struct{}
	stop sync.Once

	// Counters for the dashboard's own health tiles. Written by the flusher
	// and the handlers, read by a page render, so they take the mutex rather
	// than being racy ints that mostly work.
	mu       sync.Mutex
	written  int64
	rejected int64
	failed   int64
}

// row is one record flattened into the columns it will be stored in. The split
// happens once, on the way in, rather than in every query later.
type row struct {
	source     string
	ts         int64
	level      string
	msg        string
	component  string
	method     string
	path       string
	host       string
	status     int64
	durationMS float64
	ip         string
	cfRay      string
	attrs      string
	// rollupOnly writes the hourly counter but not the raw line. Set for the
	// container health check, which is the binary probing itself over loopback
	// every thirty seconds: roughly 11,500 records a day per fleet that say
	// only "still running", and which were being counted as real traffic by
	// the latency percentiles and the busiest-paths ranking. The counter still
	// proves each site answered its probe, with count, sum and max duration,
	// and it proves it forever rather than for thirty days.
	rollupOnly bool
}

func NewWriter(db *sql.DB) *Writer {
	w := &Writer{
		db:   db,
		ch:   make(chan row, writeQueue),
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}
	go w.run()
	return w
}

// Stats is what the dashboard reports about the ingest path itself. A logging
// site that cannot say whether it is dropping records is asking to be trusted
// on the one thing it should be able to prove.
type Stats struct {
	Queued   int
	Capacity int
	Written  int64
	Rejected int64
	Failed   int64
}

func (w *Writer) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Stats{
		Queued:   len(w.ch),
		Capacity: cap(w.ch),
		Written:  w.written,
		Rejected: w.rejected,
		Failed:   w.failed,
	}
}

// enqueue takes a whole batch or none of it, and reports whether it did.
//
// All-or-nothing rather than filling until full, because a half-written batch
// is a gap in the middle of one site's history with nothing marking it, while a
// refused one is a 429 the shipper counts.
//
// The capacity check and the sends cannot be made atomic without a lock on the
// hot path, so two callers can both see room and only one can have it. What
// matters is that the loser says so: it used to fall through to a non-blocking
// send, drop, and still return true, which answered 202 Accepted for records
// that were never queued. The sender then counted the batch as delivered and
// never retried. Now any drop makes the whole call report failure, so the
// answer is a 429 the shipper can see.
func (w *Writer) enqueue(rows []row) bool {
	if len(rows) > cap(w.ch)-len(w.ch) {
		w.mu.Lock()
		w.rejected += int64(len(rows))
		w.mu.Unlock()
		return false
	}
	dropped := 0
	for _, r := range rows {
		select {
		case w.ch <- r:
		default:
			dropped++
		}
	}
	if dropped > 0 {
		w.mu.Lock()
		w.rejected += int64(dropped)
		w.mu.Unlock()
		return false
	}
	return true
}

func (w *Writer) run() {
	defer close(w.done)

	tick := time.NewTicker(writeEvery)
	defer tick.Stop()

	batch := make([]row, 0, writeBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		err := w.commit(batch)
		if err != nil && isBusy(err) {
			// One retry, because the common cause is transient: a long read
			// on the dashboard, or the retention pass holding the write lock.
			// Discarding 500 records the first time a reader was slow is a
			// poor trade for the one line of code this costs.
			select {
			case <-time.After(250 * time.Millisecond):
			}
			err = w.commit(batch)
		}
		if err != nil {
			// Straight to the stdout handler. Logging this through slog
			// would enqueue a record about a failing writer onto the
			// queue that writer drains.
			w.mu.Lock()
			w.failed += int64(len(batch))
			w.mu.Unlock()
			fmt.Fprintf(os.Stderr, "logging: dropped %d records: %v\n", len(batch), err)
		} else {
			w.mu.Lock()
			w.written += int64(len(batch))
			w.mu.Unlock()
		}
		batch = batch[:0]
	}

	for {
		select {
		case r := <-w.ch:
			batch = append(batch, r)
			if len(batch) >= writeBatch {
				flush()
			}
		case <-tick.C:
			flush()
		case <-w.quit:
			for {
				select {
				case r := <-w.ch:
					batch = append(batch, r)
					if len(batch) >= writeBatch {
						flush()
					}
					continue
				default:
				}
				break
			}
			flush()
			return
		}
	}
}

// Close drains the queue and commits what is left, so a deploy does not throw
// away the last quarter second of every site's logs.
func (w *Writer) Close() {
	w.stop.Do(func() { close(w.quit) })
	<-w.done
}

// commit writes one batch and its rollup deltas in a single transaction.
//
// One transaction for both is what keeps them consistent: a rollup that
// counted rows the insert then rolled back would be a graph that disagrees
// with its own search results forever, with nothing to reconcile it against.
func (w *Writer) commit(batch []row) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	insert, err := tx.PrepareContext(ctx, `
		INSERT INTO records
		  (source, ts, level, msg, component, method, path, host, status, duration_ms, ip, cf_ray, attrs)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insert.Close()

	// Aggregated in memory first so a batch of five hundred request logs from
	// one site in one hour is one upsert rather than five hundred.
	deltas := make(map[rollupKey]*rollupDelta, 16)

	for _, r := range batch {
		if !r.rollupOnly {
			if _, err := insert.ExecContext(ctx,
				r.source, r.ts, r.level, r.msg, r.component, r.method, r.path,
				r.host, r.status, r.durationMS, r.ip, r.cfRay, r.attrs); err != nil {
				return err
			}
		}

		k := rollupKey{
			hour:      hourFloor(r.ts),
			source:    r.source,
			level:     r.level,
			component: r.component,
			status:    r.status,
		}
		d := deltas[k]
		if d == nil {
			d = &rollupDelta{}
			deltas[k] = d
		}
		d.count++
		// A record with no duration is a subsystem message rather than a
		// request. Counting it as a zero would drag every mean toward zero
		// and make the busiest site look like the fastest.
		if r.durationMS > 0 {
			d.durCount++
			d.durSum += r.durationMS
			if r.durationMS > d.durMax {
				d.durMax = r.durationMS
			}
		}
	}

	upsert, err := tx.PrepareContext(ctx, `
		INSERT INTO rollups (hour, source, level, component, status, count, dur_count, dur_sum, dur_max)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(hour, source, level, component, status) DO UPDATE SET
		  count     = count     + excluded.count,
		  dur_count = dur_count + excluded.dur_count,
		  dur_sum   = dur_sum   + excluded.dur_sum,
		  dur_max   = MAX(dur_max, excluded.dur_max)`)
	if err != nil {
		return err
	}
	defer upsert.Close()

	for k, d := range deltas {
		if _, err := upsert.ExecContext(ctx,
			k.hour, k.source, k.level, k.component, k.status,
			d.count, d.durCount, d.durSum, d.durMax); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// isBusy reports whether an error is SQLite's lock contention, which is worth
// retrying, rather than a schema or disk error, which is not. Matched on the
// message because the driver does not export a typed error for it.
func isBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "busy")
}

type rollupKey struct {
	hour      int64
	source    string
	level     string
	component string
	status    int64
}

type rollupDelta struct {
	count    int64
	durCount int64
	durSum   float64
	durMax   float64
}

// ingest is the endpoint every other site posts to.
//
// There is no token, because there is no public path to it: Caddy refuses
// /ingest on logging.bythewood.me and the only address that answers is the
// container name on the orchard-edge bridge. Anything able to reach it is
// already inside the network. That is the same reasoning the ntfy alert path
// uses, and it is what keeps the two FROM scratch sites reading zero
// environment variables.
func (s *site) ingest(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxBody)
	defer body.Close()

	var batch web.Batch
	if err := json.NewDecoder(body).Decode(&batch); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	source := normalizeSource(batch.Source)
	if source == "" {
		http.Error(w, "source is required", http.StatusBadRequest)
		return
	}
	if len(batch.Records) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(batch.Records) > maxRecords {
		http.Error(w, "too many records in one batch", http.StatusRequestEntityTooLarge)
		return
	}

	rows := make([]row, 0, len(batch.Records))
	for _, rec := range batch.Records {
		rows = append(rows, toRow(source, rec))
	}

	// Backpressure as a real answer rather than a stall. The shipper drops a
	// 429'd batch and carries on, and the records are still on that site's
	// stdout. Blocking here would mean the logging site could hold a request
	// open on every site it watches.
	if !s.writer.enqueue(rows) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "ingest queue is full", http.StatusTooManyRequests)
		return
	}

	// After the enqueue, not before. A batch that was refused is not evidence
	// the source is healthy, and counting it would mean a site whose records
	// are all being shed looks perfectly alive to the silence rule.
	s.watchdog.Observe(source, batch.Records)

	w.WriteHeader(http.StatusAccepted)
}

// normalizeSource keeps the source a short, safe label. It is rendered in URLs
// and used as a query filter, and it comes from another process rather than
// from a person, so it is constrained rather than trusted.
func normalizeSource(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > 40 {
		s = s[:40]
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == '.':
			// Sites name themselves by first label, but a caller passing a
			// hostname should land in the same bucket rather than a second
			// one nobody looks at.
			return b.String()
		}
	}
	return b.String()
}

// toRow lifts the attributes that earn a column out of the JSON bag and leaves
// the rest in it.
//
// The hot set is exactly what web.Logged already emits, so nothing here asks
// the sites to log anything new: the hardening pass that converted 129 calls to
// slog is the reason this is nearly free.
func toRow(source string, rec web.Record) row {
	out := row{
		source: source,
		ts:     saneTimestamp(rec.Time),
		level:  normalizeLevel(rec.Level),
		msg:    truncate(rec.Msg, maxFieldLen),
		attrs:  "{}",
	}

	rest := make(map[string]any, len(rec.Attrs))
	for k, v := range rec.Attrs {
		switch k {
		case "component":
			out.component = normalizeKey(asString(v))
		case "method":
			out.method = truncate(asString(v), 16)
		case "path":
			out.path = truncate(asString(v), maxFieldLen)
		case "host":
			out.host = truncate(asString(v), 255)
		case "ip":
			out.ip = asString(v)
		case "cf_ray":
			out.cfRay = asString(v)
		case "status":
			out.status = saneStatus(asFloat(v))
		case "ms":
			out.durationMS = asFloat(v)
		default:
			rest[k] = v
		}
	}
	if len(rest) > 0 {
		if buf, err := json.Marshal(rest); err == nil {
			out.attrs = truncate(string(buf), maxAttrsLen)
		}
	}

	// The container health check, recognised and demoted to a counter. It is
	// the only request in the system that is the process talking to itself, so
	// it is the only one that can be identified without guessing: loopback
	// client, and the path the healthcheck flag probes.
	if out.msg == "request" && out.path == "/healthz" && (isLoopback(out.ip) || isLoopbackHost(out.host)) {
		out.component = "healthz"
		out.rollupOnly = true
	}
	return out
}

func isLoopback(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1"
}

// isLoopbackHost matches the Host header the self-probe sends, which is
// "127.0.0.1:8000" because that is the URL the -healthcheck flag requests. Both
// are checked because a record is identified by whichever the site recorded.
func isLoopbackHost(host string) bool {
	h, _, found := strings.Cut(host, ":")
	if !found {
		h = host
	}
	return h == "127.0.0.1" || h == "localhost" || h == "[::1]" || h == "::1"
}

const (
	// Caps on the two free-text fields. A single 4MB message is a legal body
	// today and would be one 4MB row forever.
	maxFieldLen = 4096
	maxAttrsLen = 8192
)

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Cut on a rune boundary so the stored value is still valid UTF-8.
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// normalizeLevel constrains the level to the four slog uses.
//
// It is a rollups primary key column, and the search page already allowlists it
// on the way out. Anything reaching ingest with a novel level would mint rollup
// rows nobody can ever filter for, in a table that is never swept.
func normalizeLevel(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		return "DEBUG"
	case "WARN", "WARNING":
		return "WARN"
	case "ERROR":
		return "ERROR"
	default:
		return "INFO"
	}
}

// normalizeKey constrains component, which is the other free-text rollups key
// column. Same reasoning as the level, plus it is rendered in the UI.
func normalizeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > 40 {
		s = s[:40]
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// saneStatus keeps the status inside the range HTTP defines. It is a rollups
// key column, so an arbitrary int64 is an arbitrary number of permanent rows.
func saneStatus(v float64) int64 {
	n := int64(v)
	if n < 0 || n > 599 {
		return 0
	}
	return n
}

// sanetimestampSkew is how far ahead of now a record may claim to be.
//
// The retention sweep deletes `ts < cutoff`, so a record dated in the future is
// never reclaimed: not by the sweep, not ever. That is reachable without an
// attacker, by one peer container with a skewed clock. Anything beyond the
// allowance is stamped with arrival time instead, which is the same thing
// already done for a record carrying no timestamp at all.
const sanetimestampSkew = 5 * time.Minute

// sanetimestampFloor rejects timestamps from before this project existed. It
// exists mostly to catch a unit mistake: seconds sent where milliseconds were
// meant lands in 1970 and would be swept immediately and silently.
var sanetimestampFloor = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

func saneTimestamp(ts int64) int64 {
	now := time.Now().UTC().UnixMilli()
	if ts < sanetimestampFloor || ts > now+sanetimestampSkew.Milliseconds() {
		return now
	}
	return ts
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

// asFloat exists because these values arrive through JSON, where every number
// is a float64 no matter how it was logged.
func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	case int:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

// LocalSink is how this site records its own logs: straight onto the writer's
// queue, with no HTTP hop to itself.
//
// It drops its own ingest request lines. Every flush from every site is one
// POST here, so keeping them would mean this site's loudest source being itself
// describing the act of being written to. Health checks need no special case
// any more: toRow demotes them to rollup-only for every source alike, including
// this one. Both are still on stdout.
func (s *site) LocalSink(source string, records []web.Record) {
	rows := make([]row, 0, len(records))
	for _, rec := range records {
		if rec.Msg == "request" && asString(rec.Attrs["path"]) == "/ingest" {
			continue
		}
		rows = append(rows, toRow(source, rec))
	}
	if len(rows) == 0 {
		return
	}
	s.writer.enqueue(rows)

	// This site watches itself too. It can only ever fire if the process is
	// serving while its own logging path has stopped, which is a narrow case
	// and still one worth hearing about; a wedged process cannot report on
	// itself and is what status is for.
	s.watchdog.Observe(selfSource, records)
}
