package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// Log shipping: a slog.Handler that tees every record to logging.bythewood.me
// on top of the existing stdout handler. stdout stays the source of truth, so
// nothing here ever blocks the caller. A full queue drops and a failed POST
// drops.

// ShipEndpoint is a container name on the orchard-edge bridge, never the public
// hostname. Anything that can reach it is already inside the network, so there
// is no token to configure.
const ShipEndpoint = "http://orchard-logging:8000/ingest"

const (
	// Bounded, so the failure mode is dropping rather than blocking.
	shipQueue = 4096
	// Whichever comes first. Each flush is one POST that the logging site
	// itself logs, so a faster cadence would make ingest the loudest thing
	// in the database.
	shipBatch   = 500
	shipEvery   = 5 * time.Second
	shipTimeout = 10 * time.Second

	// The hard ceiling on Close, and what keeps a wedged logging site from
	// reaching the sites it watches. A sink that accepts the connection and
	// never answers costs shipTimeout per flush, which unbounded runs past
	// Docker's stop grace and turns one hung container into a SIGKILL for
	// every other site, skipping their db.Close(). A healthy sink drains in
	// milliseconds and Close returns immediately either way.
	closeTimeout = 2 * time.Second
)

// Record is one slog record on the wire, and the shape the ingest endpoint
// parses. Short keys because this is machine to machine at volume.
type Record struct {
	// Unix milliseconds, UTC.
	Time  int64          `json:"t"`
	Level string         `json:"l"`
	Msg   string         `json:"m"`
	Attrs map[string]any `json:"a,omitempty"`
}

// Batch is one POST body.
type Batch struct {
	Source  string   `json:"source"`
	Records []Record `json:"records"`
}

// Sink consumes a flush. HTTPSink posts to the logging site, which passes its
// own database writer instead so it never posts to itself.
type Sink func(source string, records []Record)

// Shipper owns the queue and the goroutine that drains it.
type Shipper struct {
	source string
	sink   Sink
	ch     chan Record

	quit chan struct{}
	done chan struct{}
	stop sync.Once
}

// ShipLogs installs the tee on top of whatever slog.Default() already is, so
// SetupLogging must have run first. The returned Shipper flushes what it holds
// on Close.
func ShipLogs(source string, sink Sink) *Shipper {
	s := &Shipper{
		source: source,
		sink:   sink,
		ch:     make(chan Record, shipQueue),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	slog.SetDefault(slog.New(&teeHandler{next: slog.Default().Handler(), ship: s}))
	go s.run()
	return s
}

// enqueue never blocks. A full queue means the record is already safely on
// stdout either way.
//
// The channel is never closed. Handle can still run after Close, from a later
// defer, and a send on a closed channel panics, which is the one way a log
// shipper could take a site down with it.
func (s *Shipper) enqueue(r Record) {
	select {
	case s.ch <- r:
	default:
	}
}

func (s *Shipper) run() {
	defer close(s.done)

	tick := time.NewTicker(shipEvery)
	defer tick.Stop()

	batch := make([]Record, 0, shipBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.sink(s.source, batch)
		batch = batch[:0]
	}

	for {
		select {
		case r := <-s.ch:
			batch = append(batch, r)
			if len(batch) >= shipBatch {
				flush()
			}
		case <-tick.C:
			flush()
		case <-s.quit:
			// Drain what is queued, then flush once. Anything enqueued
			// after this drops.
			for {
				select {
				case r := <-s.ch:
					batch = append(batch, r)
					if len(batch) >= shipBatch {
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

// Close drains what is queued and waits for the last flush, but never longer
// than closeTimeout.
func (s *Shipper) Close() {
	s.stop.Do(func() { close(s.quit) })

	t := time.NewTimer(closeTimeout)
	defer t.Stop()
	select {
	case <-s.done:
	case <-t.C:
		// Sink is not answering. Those records are on stdout anyway.
		fmt.Fprintf(os.Stderr, "log shipping: gave up draining after %s\n", closeTimeout)
	}
}

// teeHandler writes through to the real handler and copies to the queue. It
// cannot embed the next handler, because an embedded WithAttrs or WithGroup
// returns the inner handler and silently stops teeing.
type teeHandler struct {
	next slog.Handler
	ship *Shipper
	// Each attribute keeps the group prefix in force when it was added, not
	// the current one. A flat []slog.Attr would retroactively re-prefix
	// anything attached before a later WithGroup, so .With(component=crawler)
	// then .WithGroup("http") would ship "http.component" where slog says it
	// must stay "component", and the ingest side matches keys by exact name.
	attrs []groupedAttr
	group string
}

// groupedAttr is one attribute frozen with the prefix it was added under.
type groupedAttr struct {
	prefix string
	attr   slog.Attr
}

func (h *teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := &teeHandler{next: h.next.WithAttrs(attrs), ship: h.ship, group: h.group}
	out.attrs = append([]groupedAttr{}, h.attrs...)
	for _, a := range attrs {
		out.attrs = append(out.attrs, groupedAttr{prefix: h.group, attr: a})
	}
	return out
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	// An empty name is a no-op, matching slog.Logger.WithGroup. Without it
	// every key under the group gains a double dot.
	if name == "" {
		return h
	}
	prefix := name
	if h.group != "" {
		prefix = h.group + "." + name
	}
	out := &teeHandler{next: h.next.WithGroup(name), ship: h.ship, group: prefix}
	out.attrs = append([]groupedAttr{}, h.attrs...)
	return out
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	// stdout first and unconditionally.
	err := h.next.Handle(ctx, r)

	out := Record{
		Time:  r.Time.UTC().UnixMilli(),
		Level: r.Level.String(),
		Msg:   r.Message,
	}
	attrs := make(map[string]any, r.NumAttrs()+len(h.attrs))
	for _, a := range h.attrs {
		flatten(attrs, a.prefix, a.attr)
	}
	r.Attrs(func(a slog.Attr) bool {
		flatten(attrs, h.group, a)
		return true
	})
	if len(attrs) > 0 {
		out.Attrs = attrs
	}
	h.ship.enqueue(out)

	return err
}

// flatten writes one attribute into the map, resolving LogValuer and turning a
// group into dotted keys, so the ingest side can pull "status" out with one
// lookup.
func flatten(dst map[string]any, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	key := a.Key
	if prefix != "" {
		key = prefix + "." + key
	}

	switch a.Value.Kind() {
	case slog.KindGroup:
		for _, sub := range a.Value.Group() {
			flatten(dst, key, sub)
		}
	case slog.KindTime:
		dst[key] = a.Value.Time().UTC().Format(time.RFC3339Nano)
	case slog.KindDuration:
		dst[key] = a.Value.Duration().String()
	case slog.KindBool:
		dst[key] = a.Value.Bool()
	case slog.KindInt64:
		dst[key] = a.Value.Int64()
	case slog.KindUint64:
		dst[key] = a.Value.Uint64()
	case slog.KindFloat64:
		dst[key] = a.Value.Float64()
	case slog.KindString:
		dst[key] = a.Value.String()
	default:
		// Formatted rather than marshalled, because an arbitrary value can
		// fail to marshal and one bad attribute must not cost the batch.
		dst[key] = fmt.Sprint(a.Value.Any())
	}
}

// HTTPSink posts batches to the logging site. It never calls slog, because a
// shipper that logged its own failures would enqueue a record about failing to
// ship. State changes go straight to stderr instead.
func HTTPSink() Sink {
	client := &http.Client{Timeout: shipTimeout}
	var (
		mu      sync.Mutex
		healthy = true
		dropped int
	)

	note := func(ok bool, detail string, n int) {
		mu.Lock()
		defer mu.Unlock()
		if ok == healthy {
			if !ok {
				dropped += n
			}
			return
		}
		healthy = ok
		if ok {
			fmt.Fprintf(os.Stderr, "log shipping recovered after dropping %d records\n", dropped+n)
			dropped = 0
			return
		}
		dropped = n
		fmt.Fprintf(os.Stderr, "log shipping unavailable, dropping records: %s\n", detail)
	}

	return func(source string, records []Record) {
		body, err := json.Marshal(Batch{Source: source, Records: records})
		if err != nil {
			note(false, err.Error(), len(records))
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), shipTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ShipEndpoint, bytes.NewReader(body))
		if err != nil {
			note(false, err.Error(), len(records))
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			note(false, err.Error(), len(records))
			return
		}
		defer resp.Body.Close()
		// Drained as well as closed, or the connection leaks out of the
		// pool, and this one is reused every five seconds forever.
		_, _ = io.Copy(io.Discard, resp.Body)

		// 429 is the logging site shedding load, a healthy answer, so
		// drop the batch without marking the sink down.
		if resp.StatusCode == http.StatusTooManyRequests {
			return
		}
		if resp.StatusCode >= 300 {
			note(false, resp.Status, len(records))
			return
		}
		note(true, "", 0)
	}
}
