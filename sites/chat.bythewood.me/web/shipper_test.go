package web

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// collector is a Sink that remembers what it was handed.
type collector struct {
	mu      sync.Mutex
	records []Record
	flushes int
}

func (c *collector) sink(source string, records []Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, records...)
	c.flushes++
}

func (c *collector) all() []Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Record{}, c.records...)
}

// withShipper installs a shipper over a buffer-backed JSON handler and restores
// the process logger afterwards. ShipLogs replaces slog's default, which is
// global state, so a test that did not restore it would silently change every
// test that ran after it.
func withShipper(t *testing.T, source string, sink Sink) (*bytes.Buffer, *Shipper) {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))

	s := ShipLogs(source, sink)
	t.Cleanup(func() {
		s.Close()
		slog.SetDefault(previous)
	})
	return &buf, s
}

// The whole design is a tee, not a replacement. stdout stays the source of
// truth, so a record has to appear in both places.
func TestShipLogsTeesRatherThanReplaces(t *testing.T) {
	c := &collector{}
	buf, shipper := withShipper(t, "blog", c.sink)

	slog.Info("request",
		slog.Int("status", 200),
		slog.String("path", "/feed.atom"),
		slog.Float64("ms", 1.25))

	// Close is the deterministic flush. The ticker would get there in five
	// seconds, which is not a thing to put in a test.
	shipper.Close()

	if !bytes.Contains(buf.Bytes(), []byte(`"path":"/feed.atom"`)) {
		t.Errorf("the record did not reach the original handler:\n%s", buf.String())
	}

	got := c.all()
	if len(got) != 1 {
		t.Fatalf("shipped %d records, want 1", len(got))
	}
	r := got[0]
	if r.Msg != "request" || r.Level != "INFO" {
		t.Errorf("msg/level = %q/%q", r.Msg, r.Level)
	}
	if r.Attrs["status"] != int64(200) {
		t.Errorf("status = %#v, want int64(200)", r.Attrs["status"])
	}
	if r.Attrs["ms"] != 1.25 {
		t.Errorf("ms = %#v, want 1.25", r.Attrs["ms"])
	}
	// Milliseconds, not nanoseconds and not seconds. The ingest side stores
	// this straight into a column and a wrong unit here would put every
	// record either in 1970 or fifty thousand years out.
	if delta := time.Now().UTC().UnixMilli() - r.Time; delta < 0 || delta > 60_000 {
		t.Errorf("Time = %d, which is not unix milliseconds near now", r.Time)
	}
}

// Attribute kinds have to survive the crossing intact: a duration that became
// the string "1.042ms" is exactly what the hardening pass moved away from.
func TestShipLogsFlattensAttributeKinds(t *testing.T) {
	c := &collector{}
	_, shipper := withShipper(t, "blog", c.sink)

	slog.Info("mixed",
		slog.Bool("ok", true),
		slog.Int64("n", 42),
		slog.Float64("f", 0.5),
		slog.String("s", "text"),
		slog.Duration("d", 1500*time.Millisecond),
		slog.Any("err", context.Canceled),
	)
	shipper.Close()

	got := c.all()
	if len(got) != 1 {
		t.Fatalf("shipped %d records, want 1", len(got))
	}
	a := got[0].Attrs
	if a["ok"] != true || a["n"] != int64(42) || a["f"] != 0.5 || a["s"] != "text" {
		t.Errorf("scalar attributes did not survive: %#v", a)
	}
	if a["d"] != "1.5s" {
		t.Errorf("duration = %#v, want \"1.5s\"", a["d"])
	}
	// An arbitrary value is formatted rather than marshalled, because one
	// value that cannot marshal must not cost the whole batch.
	if a["err"] != "context canceled" {
		t.Errorf("error = %#v, want its message", a["err"])
	}
}

// A group is flattened to dotted keys. JSON has no notion of a slog group, and
// a nested object would mean the ingest side could not pull "status" out with
// one lookup.
func TestShipLogsFlattensGroups(t *testing.T) {
	c := &collector{}
	_, shipper := withShipper(t, "blog", c.sink)

	slog.Info("grouped", slog.Group("http", slog.Int("status", 503)))
	shipper.Close()

	got := c.all()
	if len(got) != 1 {
		t.Fatalf("shipped %d records, want 1", len(got))
	}
	if got[0].Attrs["http.status"] != int64(503) {
		t.Errorf("attrs = %#v, want http.status", got[0].Attrs)
	}
}

// WithAttrs and WithGroup have to return something that still tees. An embedded
// handler's versions return the inner handler, and the failure mode is logs
// that quietly stop being shipped from whichever subsystem used a child logger.
func TestShipLogsSurvivesWithAttrsAndWithGroup(t *testing.T) {
	c := &collector{}
	_, shipper := withShipper(t, "status", c.sink)

	child := slog.Default().With(slog.String("component", "crawler"))
	child.Warn("crawl slow")

	nested := slog.Default().WithGroup("job").With(slog.Int("attempt", 2))
	nested.Info("retrying")

	shipper.Close()

	got := c.all()
	if len(got) != 2 {
		t.Fatalf("shipped %d records, want 2: a child logger stopped teeing", len(got))
	}
	if got[0].Attrs["component"] != "crawler" {
		t.Errorf("With attrs lost: %#v", got[0].Attrs)
	}
	if got[1].Attrs["job.attempt"] != int64(2) {
		t.Errorf("WithGroup attrs lost: %#v", got[1].Attrs)
	}
}

// The failure this pins has no symptom until somebody uses a child logger, and
// then it is silent: an attribute added before a WithGroup must NOT gain that
// group's prefix. If it does, the ingest side stops recognising "component" and
// "status" as hot columns, the record stores an empty component and a zero
// status, and its hourly rollup is keyed wrong, while stdout shows the same
// line correctly.
func TestWithGroupDoesNotRePrefixEarlierAttrs(t *testing.T) {
	c := &collector{}
	_, shipper := withShipper(t, "status", c.sink)

	logger := slog.Default().
		With(slog.String("component", "crawler")).
		WithGroup("http")
	logger.Info("request", slog.Int("status", 503))

	shipper.Close()

	got := c.all()
	if len(got) != 1 {
		t.Fatalf("shipped %d records, want 1", len(got))
	}
	a := got[0].Attrs
	if a["component"] != "crawler" {
		t.Errorf("component = %#v, want \"crawler\" unprefixed; got attrs %#v", a["component"], a)
	}
	// The record's own attribute is inside the group, so it
	// correctly gains the prefix. Only the earlier one must not.
	if a["http.status"] != int64(503) {
		t.Errorf("http.status = %#v, want int64(503); got attrs %#v", a["http.status"], a)
	}
	if _, bad := a["http.component"]; bad {
		t.Errorf("component was retroactively moved into the group: %#v", a)
	}
}

// An empty group name is a no-op in slog. Without the short-circuit the prefix
// becomes "g." and every key under it gains a double dot.
func TestWithGroupEmptyNameIsNoOp(t *testing.T) {
	c := &collector{}
	_, shipper := withShipper(t, "blog", c.sink)

	h := slog.Default().Handler().WithGroup("").WithAttrs([]slog.Attr{slog.Int("n", 1)})
	slog.New(h).Info("x")
	shipper.Close()

	got := c.all()
	if len(got) != 1 {
		t.Fatalf("shipped %d records, want 1", len(got))
	}
	if got[0].Attrs["n"] != int64(1) {
		t.Errorf("attrs = %#v, want a bare \"n\"", got[0].Attrs)
	}
}

// Nothing here may ever block the caller. A full queue drops, because the
// record is already safely on stdout and a stalled site is a worse outcome than
// a gap in a dashboard.
func TestEnqueueDropsRatherThanBlocking(t *testing.T) {
	s := &Shipper{ch: make(chan Record, 2)}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			s.enqueue(Record{Msg: "x"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue blocked on a full queue")
	}

	if len(s.ch) != 2 {
		t.Errorf("queue holds %d, want 2", len(s.ch))
	}
}

// Handle can run after Close, on a shutdown path where something logs from a
// later defer. A send on a closed channel panics, which would be the one way a
// log shipper could take a site down with it, so the channel is never closed.
func TestLoggingAfterCloseDoesNotPanic(t *testing.T) {
	c := &collector{}
	_, shipper := withShipper(t, "blog", c.sink)

	shipper.Close()
	shipper.Close() // idempotent, because a defer plus an explicit call happens

	slog.Info("logged during shutdown")
	// And a queue's worth more, to reach the point a closed channel would
	// have been written to rather than merely selected on.
	for i := 0; i < shipQueue+10; i++ {
		slog.Info("more")
	}
}

// A wedged sink must not hold shutdown open. Before this bound, a full queue
// against a sink that accepts and never answers cost nine flushes at ten
// seconds each: 100 seconds, against a Docker stop grace of ten, so every site
// shipping to a hung logging container took a SIGKILL.
func TestCloseIsBoundedWhenTheSinkHangs(t *testing.T) {
	wedged := func(source string, records []Record) {
		// Longer than any plausible Close budget, and longer than the whole
		// test would tolerate if the bound were missing.
		time.Sleep(30 * time.Second)
	}
	_, shipper := withShipper(t, "blog", wedged)

	for i := 0; i < shipBatch*3; i++ {
		slog.Info("burst")
	}

	start := time.Now()
	shipper.Close()
	elapsed := time.Since(start)

	if elapsed > closeTimeout*3 {
		t.Errorf("Close took %v against a wedged sink, want bounded near %v", elapsed, closeTimeout)
	}
}

// Close flushes what is queued. A deploy kills these processes constantly and
// the last few seconds of records are the ones that say why.
func TestCloseFlushesPendingRecords(t *testing.T) {
	c := &collector{}
	_, shipper := withShipper(t, "blog", c.sink)

	for i := 0; i < 10; i++ {
		slog.Info("pending")
	}
	shipper.Close()

	if got := len(c.all()); got != 10 {
		t.Errorf("flushed %d records on close, want 10", got)
	}
}

// The batch cap has to actually cap. Without it a burst would be handed to the
// sink as one enormous POST that the ingest side would refuse with 413.
func TestFlushesAtBatchSize(t *testing.T) {
	c := &collector{}
	_, shipper := withShipper(t, "blog", c.sink)

	for i := 0; i < shipBatch+5; i++ {
		slog.Info("burst")
	}
	shipper.Close()

	c.mu.Lock()
	flushes, total := c.flushes, len(c.records)
	c.mu.Unlock()

	if flushes < 2 {
		t.Errorf("flushes = %d, want at least 2 past a batch of %d", flushes, shipBatch)
	}
	if total != shipBatch+5 {
		t.Errorf("shipped %d records, want %d", total, shipBatch+5)
	}
}
