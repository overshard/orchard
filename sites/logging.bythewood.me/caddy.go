package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"logging.bythewood.me/web"
)

// Caddy's access log intake. Caddy cannot carry web/shipper.go, so it ships
// over its own net writer instead: newline delimited JSON on a TCP connection
// it opens and holds. Each line becomes the same web.Record every other source
// posts to /ingest, so storage, rollups, retention and the watchdog treat a
// Caddy row exactly like a site's own.
//
// Only Caddy's access log arrives here, never its runtime log. The runtime log
// is where a failing net writer reports itself, so shipping it over the
// connection that is failing would be a feedback loop. The access log is driven
// by requests instead.

const (
	// No token, and no fence beyond the bridge, on the same reasoning as
	// /ingest: Caddy answers nothing on this port for the public hostname,
	// so anything that can reach it is already inside the network.
	caddyListenAddr = ":9001"

	// Matches the container name, like every other source here.
	caddySource = "caddy"

	// A filtered line runs about 400 bytes, so this is the cap that stops a
	// stream with no newline in it from being read into memory forever.
	caddyMaxLine = 64 << 10

	caddyBatch = 200

	// Caddy holds one connection. The cap is only so a peer that opens them
	// and never writes cannot leak goroutines.
	caddyMaxConns = 8

	caddyKeepAlive = 30 * time.Second
)

// ServeCaddyLogs listens on addr and accepts connections until ctx is cancelled.
func (s *site) ServeCaddyLogs(ctx context.Context, addr string) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return s.serveCaddyLogs(ctx, ln)
}

// serveCaddyLogs is split from the listen so a test can supply its own port.
func (s *site) serveCaddyLogs(ctx context.Context, ln net.Listener) error {
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	var live atomic.Int64
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if live.Load() >= caddyMaxConns {
			conn.Close()
			continue
		}
		live.Add(1)
		go func() {
			defer live.Add(-1)
			defer conn.Close()
			s.readCaddyLog(conn)
		}()
	}
}

// readCaddyLog drains one connection until it closes.
func (s *site) readCaddyLog(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(caddyKeepAlive)
	}

	br := bufio.NewReaderSize(conn, caddyMaxLine)

	rows := make([]row, 0, caddyBatch)
	recs := make([]web.Record, 0, caddyBatch)
	flush := func() {
		if len(rows) == 0 {
			return
		}
		s.writer.enqueue(rows)
		// After the enqueue, like the ingest path: a batch that was refused
		// is not evidence the source is healthy.
		s.watchdog.Observe(caddySource, recs)
		rows, recs = rows[:0], recs[:0]
	}

	for {
		// ReadSlice returns a view into the reader's own buffer, which the
		// next read invalidates. Nothing outlives the loop body, since
		// json.Unmarshal copies every string it keeps.
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// Drop the rest of an oversized line rather than the whole
			// connection, which Caddy would reopen and resend into.
			discardCaddyLine(br)
			continue
		}
		if err != nil {
			flush()
			return
		}

		if rec, ok := parseCaddyLine(line); ok {
			rows = append(rows, toRow(caddySource, rec))
			recs = append(recs, rec)
		}
		// Flushing once nothing else is buffered keeps a burst to one
		// enqueue and still holds a quiet stream to one line of latency.
		if len(rows) >= caddyBatch || br.Buffered() == 0 {
			flush()
		}
	}
}

func discardCaddyLine(br *bufio.Reader) {
	for {
		if _, err := br.ReadSlice('\n'); err != bufio.ErrBufferFull {
			return
		}
	}
}

// caddyEntry is the part of an access log entry this reads. The Caddyfile
// filters the header maps out, so what is left is small and fixed.
type caddyEntry struct {
	Level    string  `json:"level"`
	TS       float64 `json:"ts"`
	Status   int     `json:"status"`
	Size     int64   `json:"size"`
	Duration float64 `json:"duration"`
	// Appended by log_append, since the filter drops the headers it comes from.
	CFRay   string `json:"cf_ray"`
	Request *struct {
		Method   string `json:"method"`
		Host     string `json:"host"`
		URI      string `json:"uri"`
		ClientIP string `json:"client_ip"`
		RemoteIP string `json:"remote_ip"`
	} `json:"request"`
}

// parseCaddyLine converts one entry into the record web.Logged would have
// written for the same request, so the attribute names the storage lifts into
// columns are the ones it already knows.
func parseCaddyLine(line []byte) (web.Record, bool) {
	var e caddyEntry
	if err := json.Unmarshal(line, &e); err != nil || e.Request == nil {
		return web.Record{}, false
	}

	// client_ip is what trusted_proxies resolved, so it already agrees with
	// web.ClientIP. remote_ip is the bridge address and only a fallback.
	ip := e.Request.ClientIP
	if ip == "" {
		ip = e.Request.RemoteIP
	}

	path := e.Request.URI
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}

	attrs := map[string]any{
		"status": e.Status,
		"method": e.Request.Method,
		"path":   path,
		"host":   e.Request.Host,
		"ip":     ip,
		"bytes":  e.Size,
		"ms":     e.Duration * 1000,
	}
	// Absent means the request never crossed the tunnel, same as everywhere else.
	if e.CFRay != "" {
		attrs["cf_ray"] = e.CFRay
	}

	return web.Record{
		Time: caddyMillis(e.TS),
		// Caddy logs a handled request at error level when the handler
		// failed, which is how a 502 from a dead site arrives as one.
		Level: e.Level,
		// "request" and not Caddy's "handled request", so one message names
		// the same event across every source and the panels group them.
		Msg:   "request",
		Attrs: attrs,
	}, true
}

// caddyMillis scales by magnitude rather than pinning time_format in the
// Caddyfile, so changing that there cannot silently date every row to 1970.
// saneTimestamp replaces anything still out of range with now.
func caddyMillis(ts float64) int64 {
	switch {
	case ts <= 0:
		return 0
	case ts < 1e11:
		return int64(ts * 1000)
	case ts < 1e14:
		return int64(ts)
	case ts < 1e17:
		return int64(ts / 1e3)
	default:
		return int64(ts / 1e6)
	}
}
