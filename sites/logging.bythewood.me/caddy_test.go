package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// One real line off the wire, with the Caddyfile's filter already applied, so
// this is the shape the parser actually receives rather than an idealised one.
const caddyLine = `{"level":"info","ts":1788121034.4924123,"logger":"http.log.access.ship",` +
	`"msg":"handled request","request":{"remote_ip":"172.18.0.9","remote_port":"47644",` +
	`"client_ip":"203.0.113.7","proto":"HTTP/1.1","method":"GET","host":"blog.bythewood.me",` +
	`"uri":"/posts/hello?utm=x"},"bytes_read":0,"user_id":"","duration":0.144336934,` +
	`"size":5457,"status":200,"cf_ray":"a336674f1eecc825-ATL"}`

// The attribute names have to be the ones web.Logged uses, or toRow leaves them
// in the JSON bag and every Caddy row reads as a zero-latency request to "".
func TestParseCaddyLineMatchesLoggedShape(t *testing.T) {
	rec, ok := parseCaddyLine([]byte(caddyLine))
	if !ok {
		t.Fatal("parseCaddyLine rejected a real access log line")
	}

	r := toRow(caddySource, rec)
	if r.source != "caddy" {
		t.Errorf("source = %q, want caddy", r.source)
	}
	if r.msg != "request" {
		t.Errorf("msg = %q, want request: the panels group every source on this one string", r.msg)
	}
	if r.method != "GET" {
		t.Errorf("method = %q, want GET", r.method)
	}
	// The query string is dropped, like web.Logged's r.URL.Path, or a path
	// with a tracking parameter is its own row in every ranking.
	if r.path != "/posts/hello" {
		t.Errorf("path = %q, want /posts/hello", r.path)
	}
	if r.host != "blog.bythewood.me" {
		t.Errorf("host = %q", r.host)
	}
	// client_ip and not remote_ip, which is only cloudflared on the bridge.
	if r.ip != "203.0.113.7" {
		t.Errorf("ip = %q, want the client address and not the bridge one", r.ip)
	}
	if r.status != 200 {
		t.Errorf("status = %d, want 200", r.status)
	}
	if r.cfRay != "a336674f1eecc825-ATL" {
		t.Errorf("cf_ray = %q: log_append puts it back after the filter drops the headers", r.cfRay)
	}
	// Seconds on the wire, milliseconds in the column.
	if r.durationMS < 144.3 || r.durationMS > 144.4 {
		t.Errorf("durationMS = %v, want about 144.34", r.durationMS)
	}
	if want := time.UnixMilli(1788121034492); !time.UnixMilli(r.ts).Equal(want) {
		t.Errorf("ts = %v, want %v", time.UnixMilli(r.ts), want)
	}
}

// A 502 is the case no site can log, since nothing reached one, and Caddy
// records it at error level. Losing that would keep the one interesting edge
// event out of the problems panel.
func TestParseCaddyLineKeepsErrorLevel(t *testing.T) {
	line := `{"level":"error","ts":1788121034.5,"msg":"handled request",` +
		`"request":{"client_ip":"203.0.113.7","method":"GET","host":"blog.bythewood.me","uri":"/"},` +
		`"duration":0.01,"size":0,"status":502}`

	rec, ok := parseCaddyLine([]byte(line))
	if !ok {
		t.Fatal("rejected a 502 line")
	}
	r := toRow(caddySource, rec)
	if r.level != "ERROR" {
		t.Errorf("level = %q, want ERROR", r.level)
	}
	if r.status != 502 {
		t.Errorf("status = %d, want 502", r.status)
	}
}

// Only access entries carry a request object. Anything else on the socket is
// not a request and must not become a row claiming to be one.
func TestParseCaddyLineRejectsNonRequests(t *testing.T) {
	for name, line := range map[string]string{
		"runtime line": `{"level":"info","ts":1788121034.5,"logger":"http","msg":"server running"}`,
		"not json":     `handled request GET / 200`,
		"empty":        ``,
		"null request": `{"level":"info","ts":1,"msg":"x","request":null}`,
	} {
		if _, ok := parseCaddyLine([]byte(line)); ok {
			t.Errorf("%s: parsed as a request", name)
		}
	}
}

// The unit of Caddy's ts follows time_format, which nothing here pins, so the
// scale is read off the magnitude. Getting it wrong dates every row to 1970,
// where the retention sweep deletes it on the next pass.
func TestCaddyMillis(t *testing.T) {
	const wantMS = 1788121034492
	for name, ts := range map[string]float64{
		"seconds":      1788121034.4924123,
		"milliseconds": 1788121034492,
		"microseconds": 1788121034492000,
		"nanoseconds":  1788121034492000000,
	} {
		got := caddyMillis(ts)
		// Float64 cannot hold nanosecond precision at this magnitude.
		if got < wantMS-2 || got > wantMS+2 {
			t.Errorf("%s: caddyMillis = %d, want about %d", name, got, wantMS)
		}
	}
	if got := caddyMillis(0); got != 0 {
		t.Errorf("caddyMillis(0) = %d, want 0 so saneTimestamp substitutes now", got)
	}
}

// End to end over a real socket, because the parser being right is only half of
// it: the read loop has to frame lines and flush them into the writer.
func TestCaddyLogSocketWritesRows(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.serveCaddyLogs(ctx, ln) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// Three good lines and one that is not JSON, which must be skipped
	// without taking the two after it with it.
	for _, line := range []string{caddyLine, "{not json", caddyLine, caddyLine} {
		if _, err := fmt.Fprintln(conn, line); err != nil {
			t.Fatal(err)
		}
	}
	conn.Close()

	// The reader owns its own goroutine and the writer commits on a ticker,
	// so the rows arrive after the close rather than because of it.
	if got := waitForCaddyRows(t, db, 3); got != 3 {
		t.Errorf("records = %d, want 3: the malformed line must be skipped, not fatal", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("serveCaddyLogs returned %v on a cancelled context, want nil", err)
	}
}

// waitForCaddyRows polls until the count settles or the deadline passes, and
// returns whatever it last saw so the caller reports the real number.
func waitForCaddyRows(t *testing.T, db *sql.DB, want int) int {
	t.Helper()
	var n int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM records WHERE source = 'caddy'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= want {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
	return n
}

// A line past the buffer has to cost that line only. Closing the connection
// instead would have Caddy reconnect and send it again, forever.
func TestCaddyLogSkipsOversizedLine(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.serveCaddyLogs(ctx, ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	huge := `{"request":{"uri":"/` + strings.Repeat("a", caddyMaxLine+1024) + `"}}`
	for _, line := range []string{huge, caddyLine} {
		if _, err := fmt.Fprintln(conn, line); err != nil {
			t.Fatal(err)
		}
	}
	conn.Close()

	records := waitForCaddyRows(t, db, 1)

	var path string
	if err := db.QueryRow(
		`SELECT COALESCE(MAX(path), '') FROM records WHERE source = 'caddy'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if records != 1 || path != "/posts/hello" {
		t.Errorf("records = %d, path = %q; want 1 and /posts/hello, so the line after the oversized one survived",
			records, path)
	}
}

// caddy is the one source with no self probe, so it cannot share the threshold
// that nine missed health checks set.
func TestCaddySilenceThresholdIsLonger(t *testing.T) {
	if silenceFor(caddySource) <= silenceFor("blog") {
		t.Errorf("silenceFor(caddy) = %v, must exceed %v: its heartbeat is a three minute probe, not a thirty second check",
			silenceFor(caddySource), silenceFor("blog"))
	}
	// Room for several missed probe cycles, or a status deploy fires it.
	if silenceFor(caddySource) < 3*checkCadence {
		t.Errorf("silenceFor(caddy) = %v, want at least %v", silenceFor(caddySource), 3*checkCadence)
	}
}

// What status waits between probes of one hostname, and so how often caddy is
// guaranteed to see a request with nobody visiting.
const checkCadence = 3 * time.Minute

// Every app names the kind of route it served. An edge row that named nothing
// left a hole in the one dimension the rollups group by.
func TestCaddyRowsCarryAComponent(t *testing.T) {
	const line = `{"level":"info","ts":1788220000.5,"status":200,"size":4058,"duration":0.0016,
	  "cf_ray":"a33f-IAD","component":"edge",
	  "request":{"method":"GET","host":"repos.bythewood.me","uri":"/","client_ip":"203.0.113.7"}}`

	rec, ok := parseCaddyLine([]byte(line))
	if !ok {
		t.Fatal("did not parse")
	}
	if got := rec.Attrs["component"]; got != "edge" {
		t.Errorf("component = %v, want edge", got)
	}

	// A Caddy that has not picked up the log_append yet still lands somewhere.
	const bare = `{"level":"info","ts":1788220000.5,"status":200,"size":10,"duration":0.001,
	  "request":{"method":"GET","host":"blog.bythewood.me","uri":"/","client_ip":"203.0.113.7"}}`
	rec, ok = parseCaddyLine([]byte(bare))
	if !ok {
		t.Fatal("did not parse")
	}
	if got := rec.Attrs["component"]; got != "edge" {
		t.Errorf("component = %v, want edge by default", got)
	}
}
