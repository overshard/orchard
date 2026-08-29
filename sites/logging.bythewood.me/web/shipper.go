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
// while leaving the existing stdout handler untouched.
//
// Tee rather than replace is the whole design. stdout stays the source of
// truth, Docker's json-file driver keeps rotating it, and the worst thing a
// broken logging site can do is lose lines from a dashboard. Nothing here ever
// blocks the caller: a full queue drops, and a failed POST drops. A logging
// site that can stall the sites it watches is worse than no logging site.
//
// Shipping is per site rather than a sidecar reading Docker's json files,
// because a sidecar needs the Docker socket (root-equivalent on the host) and
// because it would mean serializing typed attributes to JSON, letting Docker
// wrap that in a second JSON envelope, and parsing both back out. A handler
// here sees the records directly, attributes intact.
//
// See code/memory/decisions/0015-logging-site-and-per-site-shipping.md.

// ShipEndpoint is a container name on the orchard-edge bridge, never the public
// hostname. The same reasoning the ntfy alert path uses: anything able to reach
// this address is already inside the network, so there is no token and no new
// environment variable, and the two FROM scratch sites keep reading zero of
// them.
const ShipEndpoint = "http://orchard-logging:8000/ingest"

const (
	// A bounded queue is what makes dropping the failure mode rather than
	// blocking. 4096 records is a couple of minutes of traffic on the
	// busiest site here and about a megabyte of memory.
	shipQueue = 4096
	// Flush on whichever comes first. Five seconds rather than sub-second
	// because each flush is one POST that the logging site itself logs, and
	// a quarter-second cadence across five sites would make the ingest path
	// the loudest thing in the database.
	shipBatch   = 500
	shipEvery   = 5 * time.Second
	shipTimeout = 10 * time.Second

	// The hard ceiling on Close, and the number that keeps a broken logging
	// site from reaching the sites it watches.
	//
	// The drain flushes the queue in batches, so a wedged sink (one that
	// accepts the connection and never answers) costs shipTimeout per flush:
	// a full 4096-deep queue is nine flushes, which measured at 100 seconds.
	// Docker's default stop grace is 10 seconds, so every site would burn its
	// whole grace here and take a SIGKILL, skipping its own deferred
	// db.Close(). One hung container would cause an unclean shutdown of four
	// healthy ones, which is precisely what this design promises cannot
	// happen.
	//
	// Waiting on a timer rather than on the drain bounds the bad case without
	// spoiling the good one: a healthy sink drains in milliseconds and Close
	// returns immediately. If the timer wins, the goroutine is abandoned and
	// the process exits anyway; those records were already on stdout.
	closeTimeout = 2 * time.Second
)

// Record is one slog record on the wire, and the shape the ingest endpoint
// parses. Short keys because this is machine to machine and the volume is the
// point.
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

// Sink consumes a flush. HTTPSink posts to the logging site; the logging site
// passes its own database writer instead, so it never posts to itself and no
// amount of ingest traffic can feed itself.
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
// when Close is called, which is worth doing on shutdown: a deploy kills these
// processes constantly and the last few seconds of records are the ones that
// say why.
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

// enqueue never blocks. A full queue means the sink is down or the process is
// producing faster than it can ship, and in both cases the record is already
// safely on stdout.
//
// The channel is deliberately never closed. Handle can run after Close, on a
// shutdown path where something logs from a later defer, and a send on a closed
// channel panics: the one way a log shipper could take a site down with it.
// After Close the buffer simply fills and every record drops.
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
			// Drain what is already queued, then flush once. Anything
			// enqueued after this point is dropped, which is the same
			// answer this gives to a full queue.
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

// Close drains what is queued and waits for the last flush, but never for
// longer than closeTimeout. See that constant for why the bound is the load
// bearing part.
func (s *Shipper) Close() {
	s.stop.Do(func() { close(s.quit) })

	t := time.NewTimer(closeTimeout)
	defer t.Stop()
	select {
	case <-s.done:
	case <-t.C:
		// The sink is not answering. The records it still holds are on
		// stdout, and the process is on its way out.
		fmt.Fprintf(os.Stderr, "log shipping: gave up draining after %s\n", closeTimeout)
	}
}

// teeHandler writes through to the real handler and copies to the queue.
//
// It cannot embed the next handler: WithAttrs and WithGroup have to return
// something that still tees, and an embedded handler's versions return the
// inner handler and silently lose it. Nothing in this repo calls either today;
// they are implemented properly anyway, because the failure mode of getting
// this wrong is logs that quietly stop being shipped.
type teeHandler struct {
	next slog.Handler
	ship *Shipper
	// Attributes are stored with the group prefix that was in force when they
	// were added, not with the current one. Keeping a flat []slog.Attr and
	// applying today's prefix to all of it retroactively re-prefixes
	// attributes attached before a later WithGroup: .With(component=crawler)
	// followed by .WithGroup("http") would ship "http.component", which slog
	// says must stay "component". The ingest side matches hot keys by exact
	// name, so that record would land with an empty component and a zero
	// status, and its hourly rollup would be keyed wrong, while stdout showed
	// it correctly. Two views of one line, disagreeing.
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
	// An empty name is a no-op, matching slog.Logger.WithGroup. Without this
	// the prefix becomes "group." and every key under it gains a double dot.
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
	// stdout first and unconditionally. Whatever happens after this, the
	// record has been written where it has always been written.
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
// group into dotted keys. JSON has no notion of a slog group, and a nested
// object would mean the ingest side could not pull "status" out with one
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
		// Any and errors. Formatted rather than marshalled, because an
		// arbitrary value can fail to marshal and one bad attribute must
		// not cost the whole batch.
		dst[key] = fmt.Sprint(a.Value.Any())
	}
}

// HTTPSink posts batches to the logging site.
//
// It never calls slog. A shipper that logged its own failures would enqueue a
// record about failing to ship, and a logging site that had just come back
// would then be handed a backlog of complaints about itself. State changes go
// straight to stderr instead, one line each, which is where a person looking at
// `docker logs` will see them.
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
		// Drained as well as closed: an undrained body leaks the
		// connection out of the pool, and this one is reused every
		// five seconds forever.
		_, _ = io.Copy(io.Discard, resp.Body)

		// 429 is the logging site shedding load on purpose. It is a
		// healthy answer from a healthy service, so it drops the batch
		// without claiming the sink is down.
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
