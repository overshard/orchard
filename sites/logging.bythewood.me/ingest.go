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

// Every record arrives on one channel and one goroutine drains it into batched
// transactions: SQLite takes a database-wide write lock, so a second writer buys
// SQLITE_BUSY rather than throughput.

const (
	// Queue depth between the HTTP handlers and the flusher.
	writeQueue = 16384

	// Flush on whichever comes first.
	writeBatch = 500
	writeEvery = 250 * time.Millisecond

	// maxBody is the second fence behind Caddy's own; maxRecords stops one
	// well-formed body from claiming the whole queue.
	maxBody    = 4 << 20
	maxRecords = 2000
)

// Writer owns the ingest queue and the goroutine that drains it. Safe for
// concurrent use; Close may be called more than once.
type Writer struct {
	db *sql.DB
	ch chan row

	quit chan struct{}
	done chan struct{}
	stop sync.Once

	// Written by the flusher and the handlers, read by a page render.
	mu       sync.Mutex
	written  int64
	rejected int64
	failed   int64
}

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
	// container health check, so self-probes stay out of the latency
	// percentiles and the busiest-paths ranking.
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

// Stats is what the dashboard reports about the ingest path itself.
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

// enqueue takes a whole batch or none of it, and reports whether it did. The
// capacity check is not atomic with the sends, so two callers can both see room;
// any drop must still report failure so the caller answers 429 and not 202.
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
			// The common cause is transient: a long dashboard read, or the
			// retention pass holding the write lock.
			select {
			case <-time.After(250 * time.Millisecond):
			}
			err = w.commit(batch)
		}
		if err != nil {
			// Straight to stderr: logging this through slog would enqueue a
			// record about a failing writer onto the queue it drains.
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

// Close drains the queue and commits what is left.
func (w *Writer) Close() {
	w.stop.Do(func() { close(w.quit) })
	<-w.done
}

// commit writes one batch and its rollup deltas in a single transaction, so a
// rolled-back insert cannot leave a counter that nothing can reconcile.
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

	// Aggregated in memory first so a batch from one site in one hour is one
	// upsert rather than one per record.
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
		// A record with no duration is a subsystem message, not a request;
		// counting it as zero would drag every mean down.
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

// isBusy reports whether an error is SQLite lock contention, matched on the
// message because the driver exports no typed error for it.
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

// ingest is the endpoint every other site posts to. There is no token because
// Caddy refuses /ingest on the public hostname, so the only address that answers
// is the container name on the orchard-edge bridge.
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

	// Backpressure rather than a stall: blocking here would let this site hold
	// a request open on every site it watches.
	if !s.writer.enqueue(rows) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "ingest queue is full", http.StatusTooManyRequests)
		return
	}

	// After the enqueue: a refused batch is not evidence the source is healthy.
	s.watchdog.Observe(source, batch.Records)

	w.WriteHeader(http.StatusAccepted)
}

// normalizeSource constrains the source to a short label; it arrives from
// another process and is rendered in URLs.
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
			// A caller passing a full hostname lands in the first-label bucket.
			return b.String()
		}
	}
	return b.String()
}

// toRow lifts the attributes that get a column out of the JSON bag and leaves
// the rest in it. The hot set is what web.Logged already emits.
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
	// A response the server held open is a connection lifetime, not a request
	// duration, and averaging the two together makes every mean meaningless.
	// The elapsed time moves to the bag rather than being thrown away.
	if out.msg == "request" && (out.status == 101 || out.durationMS > streamingAfterMS) {
		rest["stream_ms"] = out.durationMS
		out.durationMS = 0
	}

	if len(rest) > 0 {
		if buf, err := json.Marshal(rest); err == nil {
			out.attrs = truncate(string(buf), maxAttrsLen)
		}
	}

	// A health check is a health check whoever asked. Keying this on the caller
	// only held while each container was the sole prober of itself, and it
	// stopped holding the moment anything else polled across the bridge.
	if out.msg == "request" && out.path == "/healthz" {
		out.component = "healthz"
		out.rollupOnly = true
	}
	return out
}

func isLoopback(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1"
}

const (
	// Caps on the free-text fields: a legal 4MB message would be a 4MB row forever.
	maxFieldLen = 4096
	maxAttrsLen = 8192

	// streamingAfterMS is web's own WriteTimeout. Nothing here can serve an
	// ordinary request for longer, so anything past it was held open on purpose.
	streamingAfterMS = 60_000
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

// normalizeLevel constrains the level to the four slog uses. It is a rollups
// key column, so a novel level would mint permanent rows nobody can filter for.
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

// normalizeKey constrains component, the other free-text rollups key column.
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

// saneStatus keeps the status in range; it is a rollups key column, so an
// arbitrary int64 is an arbitrary number of permanent rows.
func saneStatus(v float64) int64 {
	n := int64(v)
	if n < 0 || n > 599 {
		return 0
	}
	return n
}

// sanetimestampSkew bounds how far ahead of now a record may claim to be. The
// sweep deletes ts < cutoff, so a future-dated row would never be reclaimed.
const sanetimestampSkew = 5 * time.Minute

// sanetimestampFloor catches a unit mistake: seconds sent where milliseconds
// were meant lands in 1970 and would be swept immediately and silently.
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

// asFloat handles JSON's float64-only numbers alongside the native types.
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

// LocalSink records this site's own logs straight onto the writer's queue. It
// drops its /ingest request lines: every flush from every site is one POST here,
// so keeping them would make this site's loudest source itself.
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

	s.watchdog.Observe(selfSource, records)
}
