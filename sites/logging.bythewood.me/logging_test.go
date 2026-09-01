package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"logging.bythewood.me/web"
)

// A temporary database per test, through the real openDB, so the schema and the
// pragmas under test are the ones that ship. An in-memory DSN would not be:
// auto_vacuum behaves differently and these tests are about the
// storage behaviour.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openDB(t.TempDir() + "/db.sqlite3")
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// mustTemplates is the embedded template directory, the same one main uses, so
// a template that only fails at execute time fails here rather than in a
// browser.
func mustTemplates(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	return sub
}

func testSite(t *testing.T, db *sql.DB) *site {
	t.Helper()
	w := NewWriter(db)
	t.Cleanup(w.Close)
	return &site{db: db, writer: w, auth: stubAuth(t, true)}
}

// stubAuth stands in for auth.bythewood.me, which the real Authenticator asks
// on every request. Signing in itself is that site's to test.
func stubAuth(t *testing.T, signedIn bool) *web.Authenticator {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !signedIn {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"ok":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"username":"isaac"}`))
	}))
	t.Cleanup(srv.Close)
	return web.NewAuthenticatorAt(srv.URL)
}

// The hot attributes are lifted into columns and everything else stays in the
// JSON bag. Getting this wrong is invisible: no error, just a dashboard where
// every latency is zero because "ms" stayed in the blob.
func TestToRowLiftsHotAttributes(t *testing.T) {
	// Values arrive through JSON, where every number is a float64 no matter
	// how it was logged. Encoding and decoding for real rather than building
	// the map by hand, because building it by hand is what would let an
	// int64 pass a test the wire never produces.
	raw := `{"t":1756500000000,"l":"info","m":"request","a":{
		"status":404,"method":"GET","path":"/nope","host":"blog.bythewood.me",
		"ip":"203.0.113.7","cf_ray":"9a1f-IAD","ms":1.25,"bytes":512}}`
	var rec web.Record
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := toRow("blog", rec)

	if got.source != "blog" || got.ts != 1756500000000 {
		t.Errorf("source/ts = %q/%d", got.source, got.ts)
	}
	// Uppercased, so a level logged as "info" and one logged as "INFO" land
	// in the same rollup bucket rather than two.
	if got.level != "INFO" {
		t.Errorf("level = %q, want INFO", got.level)
	}
	if got.status != 404 {
		t.Errorf("status = %d, want 404", got.status)
	}
	if got.durationMS != 1.25 {
		t.Errorf("durationMS = %v, want 1.25", got.durationMS)
	}
	if got.path != "/nope" || got.method != "GET" || got.ip != "203.0.113.7" || got.cfRay != "9a1f-IAD" {
		t.Errorf("lifted columns wrong: %+v", got)
	}

	var rest map[string]any
	if err := json.Unmarshal([]byte(got.attrs), &rest); err != nil {
		t.Fatalf("attrs is not json: %q", got.attrs)
	}
	if len(rest) != 1 || rest["bytes"] != float64(512) {
		t.Errorf("attrs = %v, want only bytes", rest)
	}
}

func TestToRowDefaults(t *testing.T) {
	before := time.Now().UTC().UnixMilli()
	got := toRow("blog", web.Record{Msg: "hello"})
	if got.level != "INFO" {
		t.Errorf("level = %q, want INFO for an unset level", got.level)
	}
	// A record with no timestamp is stamped on arrival rather than filed at
	// the epoch, where it would be swept the moment retention next ran.
	if got.ts < before {
		t.Errorf("ts = %d, want at least %d", got.ts, before)
	}
	if got.attrs != "{}" {
		t.Errorf("attrs = %q, want {}", got.attrs)
	}
}

func TestNormalizeSource(t *testing.T) {
	tests := map[string]string{
		"blog":               "blog",
		"BLOG":               "blog",
		"  status  ":         "status",
		"blog.bythewood.me":  "blog",
		"iSaacByThewood.com": "isaacbythewood",
		// Everything from the first dot on is dropped, because a caller
		// passing a hostname should land in the same bucket as one passing
		// a label rather than opening a second one nobody looks at. A
		// traversal attempt loses its slashes on the way there.
		"weird/../path":         "weird",
		"logging/../../etc":     "logging",
		"":                      "",
		"with spaces and !!":    "withspacesand",
		strings.Repeat("a", 60): strings.Repeat("a", 40),
	}
	for in, want := range tests {
		if got := normalizeSource(in); got != want {
			t.Errorf("normalizeSource(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIngestWritesRecordsAndRollups(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	// Two records in the same hour, same source, same level, same status, so
	// they must collapse into exactly one rollup row rather than two.
	hour := time.Date(2026, 8, 29, 14, 0, 0, 0, time.UTC).UnixMilli()
	body := web.Batch{Source: "blog", Records: []web.Record{
		{Time: hour + 1000, Level: "INFO", Msg: "request",
			Attrs: map[string]any{"status": 200, "ms": 2.0, "path": "/"}},
		{Time: hour + 2000, Level: "INFO", Msg: "request",
			Attrs: map[string]any{"status": 200, "ms": 4.0, "path": "/"}},
	}}
	buf, _ := json.Marshal(body)

	rr := httptest.NewRecorder()
	s.ingest(rr, httptest.NewRequest(http.MethodPost, "/ingest", strings.NewReader(string(buf))))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("ingest = %d, want 202", rr.Code)
	}

	// The writer flushes on a timer, so the assertion has to wait for it
	// rather than assume it. Close is the deterministic way to make it
	// happen, and it is what shutdown does anyway.
	s.writer.Close()

	var records int
	if err := db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 2 {
		t.Errorf("records = %d, want 2", records)
	}

	var rows int
	var count, durCount int64
	var durSum, durMax float64
	if err := db.QueryRow(`SELECT COUNT(*) FROM rollups`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("rollup rows = %d, want 1: two records in one hour must collapse", rows)
	}
	if err := db.QueryRow(
		`SELECT count, dur_count, dur_sum, dur_max FROM rollups`).
		Scan(&count, &durCount, &durSum, &durMax); err != nil {
		t.Fatal(err)
	}
	if count != 2 || durCount != 2 || durSum != 6 || durMax != 4 {
		t.Errorf("rollup = count %d, durCount %d, sum %v, max %v; want 2, 2, 6, 4",
			count, durCount, durSum, durMax)
	}
}

// A record with no duration is a subsystem message, not an instant request.
// Counting it as a zero would drag every mean toward zero, which is the kind of
// wrong that looks like good news.
func TestRollupIgnoresZeroDurations(t *testing.T) {
	db := testDB(t)
	w := &Writer{db: db}

	hour := time.Date(2026, 8, 29, 14, 0, 0, 0, time.UTC).UnixMilli()
	err := w.commit([]row{
		{source: "status", ts: hour, level: "INFO", msg: "cycle complete", component: "scheduler"},
		{source: "status", ts: hour, level: "INFO", msg: "cycle complete", component: "scheduler"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var count, durCount int64
	var durSum float64
	if err := db.QueryRow(
		`SELECT count, dur_count, dur_sum FROM rollups`).Scan(&count, &durCount, &durSum); err != nil {
		t.Fatal(err)
	}
	if count != 2 || durCount != 0 || durSum != 0 {
		t.Errorf("count %d, durCount %d, sum %v; want 2, 0, 0", count, durCount, durSum)
	}
}

func TestIngestRejectsBadInput(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"not json", `{`, http.StatusBadRequest},
		{"no source", `{"source":"","records":[{"m":"x"}]}`, http.StatusBadRequest},
		{"source is punctuation only", `{"source":"!!!","records":[{"m":"x"}]}`, http.StatusBadRequest},
		{"empty batch", `{"source":"blog","records":[]}`, http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			s.ingest(rr, httptest.NewRequest(http.MethodPost, "/ingest", strings.NewReader(tc.body)))
			if rr.Code != tc.want {
				t.Errorf("status = %d, want %d", rr.Code, tc.want)
			}
		})
	}
}

// Backpressure has to be a real answer rather than a stall: a logging site that
// can hold a request open on every site it watches is worse than no logging
// site. The whole batch is refused rather than half-written.
func TestIngestSheds429WhenFull(t *testing.T) {
	db := testDB(t)
	// A writer with no goroutine draining it, so the queue fills.
	w := &Writer{db: db, ch: make(chan row, 2), quit: make(chan struct{}), done: make(chan struct{})}
	s := &site{db: db, writer: w}

	body := `{"source":"blog","records":[{"m":"a"},{"m":"b"},{"m":"c"}]}`
	rr := httptest.NewRecorder()
	s.ingest(rr, httptest.NewRequest(http.MethodPost, "/ingest", strings.NewReader(body)))

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("429 with no Retry-After: the shipper is being told to back off with no idea for how long")
	}
	if len(w.ch) != 0 {
		t.Errorf("queued %d records from a refused batch, want 0: a refused batch must be all or nothing", len(w.ch))
	}
	if got := w.Stats().Rejected; got != 3 {
		t.Errorf("rejected = %d, want 3", got)
	}
}

// The local sink is how this site records itself. It must drop its own ingest
// request lines: every flush from every site is one POST here, so keeping them
// would make this site's loudest source itself, describing the act of being
// written to. Health checks need no special case here any more; toRow demotes
// them for every source alike.
func TestLocalSinkDropsSelfChatter(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	s.LocalSink("logging", []web.Record{
		{Level: "INFO", Msg: "request", Attrs: map[string]any{"path": "/ingest", "ip": "127.0.0.1"}},
		{Level: "INFO", Msg: "request", Attrs: map[string]any{"path": "/healthz", "ip": "127.0.0.1"}},
		{Level: "INFO", Msg: "request", Attrs: map[string]any{"path": "/overview", "ip": "203.0.113.7"}},
		{Level: "INFO", Msg: "retention sweep", Attrs: map[string]any{"component": "retention"}},
	})
	s.writer.Close()

	var paths []string
	rows, err := db.Query(`SELECT path FROM records ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if len(paths) != 2 || paths[0] != "/overview" || paths[1] != "" {
		t.Errorf("stored paths = %q, want [/overview, \"\"]", paths)
	}

	// The health check left no raw row but must still be counted, forever,
	// under its own component, which is why they are demoted rather
	// than dropping.
	var healthz int64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(count), 0) FROM rollups WHERE component = 'healthz'`).Scan(&healthz); err != nil {
		t.Fatal(err)
	}
	if healthz != 1 {
		t.Errorf("healthz rollup count = %d, want 1: demoted, not discarded", healthz)
	}
}

// Health checks are the process probing itself over loopback every thirty
// seconds. Counted as traffic they made the p50 read 0.042ms against a real
// 0.537ms and put /healthz at the top of the busiest paths, which is roughly
// 66% of every raw scan spent on the fleet talking to itself.
func TestHealthChecksAreRollupOnly(t *testing.T) {
	db := testDB(t)
	base := time.Now().UTC().Add(-time.Minute).UnixMilli()

	probes := make([]row, 0, 20)
	for i := 0; i < 20; i++ {
		probes = append(probes, toRow("blog", web.Record{
			Time: base, Level: "INFO", Msg: "request",
			Attrs: map[string]any{
				"path": "/healthz", "status": 200, "ms": 0.01,
				"ip": "127.0.0.1", "host": "127.0.0.1:8000",
			},
		}))
	}
	real := toRow("blog", web.Record{
		Time: base, Level: "INFO", Msg: "request",
		Attrs: map[string]any{"path": "/", "status": 200, "ms": 5.0, "ip": "203.0.113.7"},
	})
	seedRows(t, db, append(probes, real))

	var raw int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != 1 {
		t.Errorf("raw rows = %d, want 1: only the real request is stored", raw)
	}

	f := filter{StartMS: base - 1000, EndMS: base + 1000}

	// The latency the dashboard shows must be the real request's, not one
	// diluted by twenty loopback probes.
	if got := latency(context.Background(), db, f); got.P50 != 5 {
		t.Errorf("p50 = %v, want 5: probes are polluting the percentiles", got.P50)
	}
	if got := topPaths(context.Background(), db, f, 10); len(got) != 1 || got[0].Label != "/" {
		t.Errorf("busiest paths = %+v, want only /", got)
	}
	// And the tiles agree: one request, not twenty-one.
	if got := totals(context.Background(), db, f); got.Requests != 1 {
		t.Errorf("Requests = %d, want 1", got.Requests)
	}

	// The probes are still counted forever, and still visible as a component.
	var healthz int64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(count), 0) FROM rollups WHERE component = 'healthz'`).Scan(&healthz); err != nil {
		t.Fatal(err)
	}
	if healthz != 20 {
		t.Errorf("healthz rollup count = %d, want 20", healthz)
	}
}

func seedRows(t *testing.T, db *sql.DB, rows []row) {
	t.Helper()
	if err := (&Writer{db: db}).commit(rows); err != nil {
		t.Fatal(err)
	}
}

func TestTotalsAndErrorRate(t *testing.T) {
	db := testDB(t)
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC).UnixMilli()

	seedRows(t, db, []row{
		{source: "blog", ts: base, level: "INFO", msg: "request", status: 200, durationMS: 1, cfRay: "x"},
		{source: "blog", ts: base, level: "INFO", msg: "request", status: 200, durationMS: 3, cfRay: "x"},
		{source: "blog", ts: base, level: "INFO", msg: "request", status: 404, durationMS: 1, cfRay: "x"},
		{source: "blog", ts: base, level: "ERROR", msg: "request", status: 500, durationMS: 9},
		{source: "blog", ts: base, level: "WARN", msg: "slow crawl", component: "crawler"},
	})

	f := filter{StartMS: base - 1, EndMS: base + 1}
	got := totals(context.Background(), db, f)

	if got.Records != 5 {
		t.Errorf("Records = %d, want 5", got.Records)
	}
	if got.Requests != 4 {
		t.Errorf("Requests = %d, want 4: the crawler line has no status", got.Requests)
	}
	if got.Errors != 1 || got.Warnings != 1 {
		t.Errorf("Errors/Warnings = %d/%d, want 1/1", got.Errors, got.Warnings)
	}
	if got.Server5xx != 1 || got.Client4xx != 1 {
		t.Errorf("5xx/4xx = %d/%d, want 1/1", got.Server5xx, got.Client4xx)
	}
	// 1 of 4 requests, not 1 of 5 records. Mixing the two is the mistake
	// this pins: a scheduler complaining is not a visitor being told no.
	if got.ErrorRate != 25 {
		t.Errorf("ErrorRate = %v, want 25", got.ErrorRate)
	}
	// The one request record with no cf_ray. The crawler line is not a
	// request and must not count.
	if got.DirectHits != 1 {
		t.Errorf("DirectHits = %d, want 1", got.DirectHits)
	}
}

// Every container health check is the binary probing itself over loopback, so
// it carries no CF-Ray and would otherwise read as "something reached the
// origin without crossing the tunnel". At one every thirty seconds per site
// that buries the real signal within an hour, which is how this was found: the
// tile read 18 on a system with no external traffic at all.
func TestDirectHitsIgnoresLoopback(t *testing.T) {
	db := testDB(t)
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC).UnixMilli()

	seedRows(t, db, []row{
		// The health check, twice. Not a direct hit.
		{source: "blog", ts: base, level: "INFO", msg: "request", path: "/healthz", status: 200, durationMS: 0.01, ip: "127.0.0.1"},
		{source: "blog", ts: base, level: "INFO", msg: "request", path: "/healthz", status: 200, durationMS: 0.01, ip: "::1"},
		// A real request that reached the origin without a CF-Ray. This is
		// the thing the tile exists to surface.
		{source: "blog", ts: base, level: "INFO", msg: "request", path: "/", status: 200, durationMS: 1, ip: "203.0.113.7"},
		// And one that came through the tunnel normally.
		{source: "blog", ts: base, level: "INFO", msg: "request", path: "/", status: 200, durationMS: 1, ip: "198.51.100.4", cfRay: "9a1f-IAD"},
	})

	got := totals(context.Background(), db, filter{StartMS: base - 1, EndMS: base + 1})
	if got.DirectHits != 1 {
		t.Errorf("DirectHits = %d, want 1: loopback health checks are not direct hits", got.DirectHits)
	}
	// The health checks are still stored and still findable; they are only
	// excluded from this one count.
	if got.Requests != 4 {
		t.Errorf("Requests = %d, want 4: health checks are still records", got.Requests)
	}
}

func TestLatencyPercentiles(t *testing.T) {
	db := testDB(t)
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC).UnixMilli()

	var rows []row
	// 1..100ms, so the percentiles are arithmetic somebody can check by
	// hand rather than a number that only agrees with itself.
	for i := 1; i <= 100; i++ {
		rows = append(rows, row{
			source: "blog", ts: base, level: "INFO", msg: "request",
			status: 200, durationMS: float64(i),
		})
	}
	seedRows(t, db, rows)

	got := latency(context.Background(), db, filter{StartMS: base - 1, EndMS: base + 1})
	if got.Count != 100 {
		t.Fatalf("Count = %d, want 100", got.Count)
	}
	// Nearest rank: the pth percentile of 1..100 is the ceil(p*100)th value.
	// The earlier offset-based implementation returned 51/96/100, one row high
	// whenever count*p landed exactly on an integer.
	if got.P50 != 50 || got.P95 != 95 || got.P99 != 99 {
		t.Errorf("p50/p95/p99 = %v/%v/%v, want 50/95/99", got.P50, got.P95, got.P99)
	}
	if got.Max != 100 {
		t.Errorf("Max = %v, want 100", got.Max)
	}
}

// A percentile offset past the end of a short set would read nothing and render
// as "instant", which is the worst possible way to be wrong about latency.
func TestLatencyClampsOnTinySets(t *testing.T) {
	db := testDB(t)
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC).UnixMilli()
	seedRows(t, db, []row{
		{source: "blog", ts: base, level: "INFO", msg: "request", status: 200, durationMS: 7},
	})

	got := latency(context.Background(), db, filter{StartMS: base - 1, EndMS: base + 1})
	if got.P50 != 7 || got.P95 != 7 || got.P99 != 7 {
		t.Errorf("p50/p95/p99 = %v/%v/%v, want 7 for a single sample", got.P50, got.P95, got.P99)
	}
}

// A quiet bucket has no rollup row. Skipping it would draw a straight line
// between its neighbours and hide an outage, so the series is filled.
func TestVolumeGraphFillsEmptyBuckets(t *testing.T) {
	db := testDB(t)
	start := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)

	seedRows(t, db, []row{
		{source: "blog", ts: start.UnixMilli(), level: "INFO", msg: "request", status: 200, durationMS: 1},
		{source: "blog", ts: start.Add(3 * time.Hour).UnixMilli(), level: "ERROR", msg: "request", status: 500, durationMS: 1},
	})

	f := filter{StartMS: start.UnixMilli(), EndMS: start.Add(5 * time.Hour).UnixMilli()}
	got := volumeGraph(context.Background(), db, f)

	if len(got) != 5 {
		t.Fatalf("buckets = %d, want 5 hourly buckets", len(got))
	}
	if got[0].Count != 1 || got[1].Count != 0 || got[2].Count != 0 || got[3].Count != 1 {
		t.Errorf("counts = %v", []int64{got[0].Count, got[1].Count, got[2].Count, got[3].Count})
	}
	if got[3].Errors != 1 {
		t.Errorf("errors in bucket 3 = %d, want 1", got[3].Errors)
	}
	if got[0].Label == "" {
		t.Error("bucket has no label")
	}
}

// The group-by column is chosen from a fixed set. A caller passing a query
// parameter through would otherwise be writing SQL, so the refusal is asserted
// rather than assumed.
func TestBreakdownRefusesUnknownColumn(t *testing.T) {
	db := testDB(t)
	got := breakdown(context.Background(), db, filter{EndMS: 1}, "msg; DROP TABLE records", 5)
	if got != nil {
		t.Errorf("got %v, want nil for a column outside the fixed set", got)
	}
	// And the table is still there.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&n); err != nil {
		t.Fatalf("records table is gone: %v", err)
	}
}

func TestSearchFilterMatchesMessageOrPath(t *testing.T) {
	db := testDB(t)
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC).UnixMilli()
	seedRows(t, db, []row{
		{source: "blog", ts: base, level: "INFO", msg: "request", path: "/feed.atom", status: 200, durationMS: 1},
		{source: "blog", ts: base, level: "INFO", msg: "request", path: "/", status: 200, durationMS: 1},
		{source: "status", ts: base, level: "WARN", msg: "feed fetch failed", component: "crawler"},
	})

	f := filter{StartMS: base - 1, EndMS: base + 1, Q: "feed"}
	got := recentRecords(context.Background(), db, f, 10, 0)
	if len(got) != 2 {
		t.Fatalf("matches = %d, want 2 (one path, one message)", len(got))
	}

	// A LIKE wildcard in the search term is a literal, not a wildcard.
	// Without ESCAPE, searching for "%" matches every row.
	f.Q = "%"
	if got := recentRecords(context.Background(), db, f, 10, 0); len(got) != 0 {
		t.Errorf("searching for %%%% matched %d rows, want 0: the wildcard is not escaped", len(got))
	}
}

// A path at the sample floor used to report its 4th of 5 values under a column
// headed p95, because PERCENT_RANK gives the largest row of every partition the
// value 1.0 and the filter was `pr <= 0.95`. CUME_DIST reaches 1.0 at the max,
// so the slowest sample is selectable and a small set reports honestly.
func TestSlowestPathsP95IncludesTheSlowestSample(t *testing.T) {
	db := testDB(t)
	base := time.Now().UTC().Add(-time.Minute).UnixMilli()

	var rows []row
	for _, ms := range []float64{10, 20, 30, 40, 1000} {
		rows = append(rows, row{
			source: "blog", ts: base, level: "INFO", msg: "request",
			path: "/slow", status: 200, durationMS: ms,
		})
	}
	seedRows(t, db, rows)

	got := slowestPaths(context.Background(), db, filter{StartMS: base - 1000, EndMS: base + 1000}, 10)
	if len(got) != 1 {
		t.Fatalf("paths = %d, want 1", len(got))
	}
	if got[0].Max != 1000 {
		t.Fatalf("max = %v, want 1000", got[0].Max)
	}
	// Nearest rank over five samples puts p95 at the 5th, which is the 1000ms
	// outlier. Reporting 40 here is the old behaviour and hides the one
	// request worth finding.
	if got[0].P95 != 1000 {
		t.Errorf("p95 = %v, want 1000: the slowest sample must be reachable", got[0].P95)
	}
}

func TestSweepDeletesRawButKeepsRollups(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC()

	old := now.Add(-rawRetention - 48*time.Hour).UnixMilli()
	fresh := now.Add(-time.Hour).UnixMilli()
	seedRows(t, db, []row{
		{source: "blog", ts: old, level: "INFO", msg: "request", status: 200, durationMS: 1},
		{source: "blog", ts: old, level: "INFO", msg: "request", status: 200, durationMS: 1},
		{source: "blog", ts: fresh, level: "INFO", msg: "request", status: 200, durationMS: 1},
	})

	NewSweeper(db).sweep(context.Background())

	var records, rollups int
	if err := db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM rollups`).Scan(&rollups); err != nil {
		t.Fatal(err)
	}
	if records != 1 {
		t.Errorf("records = %d, want 1: only the fresh row survives", records)
	}
	// This is the whole design: a graph over a year still works after the
	// raw lines behind it are gone.
	if rollups != 2 {
		t.Errorf("rollups = %d, want 2: rollups are kept forever", rollups)
	}

	var total int64
	if err := db.QueryRow(`SELECT SUM(count) FROM rollups`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("rollup total = %d, want 3: the deleted rows are still counted", total)
	}
}

// The bug this pins cost every rollup-backed number on the page: `rollups.hour`
// is hour-floored, so a clause of `hour >= start` against a now-relative start
// dropped the bucket containing the start, whole. The tiles disagreed with the
// raw panels beside them, by up to a full hour of data.
//
// Every other test here uses an hour-aligned base with a base-1..base+1 window,
// which is the one shape where the floor and the bound coincide, which is
// which is why none of them caught it. This one does not.
func TestRollupsAndRawAgreeOverNamedWindows(t *testing.T) {
	db := testDB(t)

	// One record a minute for three hours, ending a minute ago. Nothing here
	// is hour-aligned relative to "now".
	now := time.Now().UTC()
	var rows []row
	for i := 1; i <= 180; i++ {
		rows = append(rows, row{
			source: "blog", ts: now.Add(-time.Duration(i) * time.Minute).UnixMilli(),
			level: "INFO", msg: "request", path: "/", status: 200, durationMS: 1,
		})
	}
	seedRows(t, db, rows)

	for _, key := range []string{"1h", "24h", "7d"} {
		t.Run(key, func(t *testing.T) {
			_, _, startMS, endMS := resolveWindow(map[string][]string{"range": {key}})
			f := filter{StartMS: startMS, EndMS: endMS}

			// What the tiles report, from rollups.
			fromRollups := totals(context.Background(), db, f).Records

			// What is actually in the window, from raw rows.
			var fromRaw int64
			w, args := f.where("ts")
			if err := db.QueryRow(`SELECT COUNT(*) FROM records WHERE `+w, args...).Scan(&fromRaw); err != nil {
				t.Fatal(err)
			}

			if fromRollups != fromRaw {
				t.Errorf("rollups report %d, raw window holds %d: the tiles and the "+
					"panels beside them disagree", fromRollups, fromRaw)
			}
			if fromRaw == 0 {
				t.Fatal("window matched nothing, so the comparison proves nothing")
			}
		})
	}
}

// The graph must not open with a phantom empty bucket, which is what happened
// when the fill loop started below the window the WHERE clause enforced.
func TestVolumeGraphHasNoPhantomLeadingBucket(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC()

	var rows []row
	for i := 1; i <= 120; i++ {
		rows = append(rows, row{
			source: "blog", ts: now.Add(-time.Duration(i) * time.Minute).UnixMilli(),
			level: "INFO", msg: "request", path: "/", status: 200, durationMS: 1,
		})
	}
	seedRows(t, db, rows)

	_, _, startMS, endMS := resolveWindow(map[string][]string{"range": {"24h"}})
	got := volumeGraph(context.Background(), db, filter{StartMS: startMS, EndMS: endMS})
	if len(got) == 0 {
		t.Fatal("no buckets")
	}

	var total int64
	for _, p := range got {
		total += p.Count
	}
	if total != 120 {
		t.Errorf("graph totals %d, want 120: buckets are being dropped or invented", total)
	}
}

// An unbounded custom range used to build one bucket per day across five
// thousand years: millions of formatted labels, joined into a polyline and
// marshalled into the page, from a single authenticated GET.
func TestCustomRangeIsClamped(t *testing.T) {
	key, _, startMS, endMS := resolveWindow(map[string][]string{
		"start": {"0001-01-01"}, "end": {"9999-12-31"},
	})
	if key == "custom" {
		t.Fatalf("accepted a 5000 year window (%d ms)", endMS-startMS)
	}
	if span := time.Duration(endMS-startMS) * time.Millisecond; span > maxWindow {
		t.Errorf("fallback window is %v, want at most %v", span, maxWindow)
	}

	// An end date in the future is pulled back to now rather than refused, so
	// a lazily typed end still works.
	key, _, _, endMS = resolveWindow(map[string][]string{
		"start": {time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")},
		"end":   {"2099-01-01"},
	})
	if key != "custom" {
		t.Errorf("key = %q, want custom for a recent start with a lazy end", key)
	}
	if endMS > time.Now().UTC().UnixMilli()+1000 {
		t.Errorf("end is in the future: %v", time.UnixMilli(endMS).UTC())
	}
}

// Reclaiming has to actually shrink the file, not merely not error. The row
// count being bounded while the file grows forever is the exact failure
// auto_vacuum exists to prevent, and it is invisible from inside SQL.
func TestSweepReclaimsDiskSpace(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/db.sqlite3"
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	old := time.Now().Add(-rawRetention - 48*time.Hour).UnixMilli()
	w := &Writer{db: db}
	for b := 0; b < 40; b++ {
		batch := make([]row, 0, writeBatch)
		for i := 0; i < writeBatch; i++ {
			batch = append(batch, row{
				source: "blog", ts: old, level: "INFO", msg: "request",
				path: "/some/reasonably/long/path/for/bulk", status: 200,
				durationMS: 1.5, ip: "203.0.113.7",
				attrs: `{"bytes":12345,"note":"padding to make rows real sized"}`,
			})
		}
		if err := w.commit(batch); err != nil {
			t.Fatal(err)
		}
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	NewSweeper(db).sweep(context.Background())
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	var left int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("records left = %d, want 0", left)
	}
	if after.Size() >= before.Size() {
		t.Errorf("file did not shrink: %d -> %d bytes; auto_vacuum is not taking effect",
			before.Size(), after.Size())
	}
}

func TestResolveWindow(t *testing.T) {
	key, _, start, end := resolveWindow(map[string][]string{"range": {"7d"}})
	if key != "7d" {
		t.Errorf("key = %q, want 7d", key)
	}
	if d := time.Duration(end-start) * time.Millisecond; d < 6*24*time.Hour || d > 8*24*time.Hour {
		t.Errorf("span = %v, want about 7 days", d)
	}

	// An unknown range falls back rather than producing a zero window, which
	// would render every panel empty and look like a data loss.
	key, _, start, end = resolveWindow(map[string][]string{"range": {"nonsense"}})
	if key != defaultRange || end <= start {
		t.Errorf("fallback = %q, span %d", key, end-start)
	}

	// Explicit dates win when both are present and sane.
	key, _, start, end = resolveWindow(map[string][]string{
		"start": {"2026-08-01"}, "end": {"2026-08-02"},
	})
	if key != "custom" {
		t.Fatalf("key = %q, want custom", key)
	}
	if end-start != 2*24*60*60*1000 {
		t.Errorf("span = %dms; end is exclusive at the start of the day after end", end-start)
	}

	// A reversed pair is ignored rather than producing a negative window.
	if key, _, _, _ := resolveWindow(map[string][]string{
		"start": {"2026-08-09"}, "end": {"2026-08-01"},
	}); key == "custom" {
		t.Error("accepted a reversed date range")
	}
}

func TestHourFloor(t *testing.T) {
	at := time.Date(2026, 8, 29, 14, 37, 12, 0, time.UTC).UnixMilli()
	want := time.Date(2026, 8, 29, 14, 0, 0, 0, time.UTC).UnixMilli()
	if got := hourFloor(at); got != want {
		t.Errorf("hourFloor = %d, want %d", got, want)
	}
}

func TestFormatMS(t *testing.T) {
	tests := map[float64]string{
		0:      "—",
		0.412:  "0.41ms",
		1.5:    "1.5ms",
		1234.5: "1.23s",
	}
	for in, want := range tests {
		// Sub-millisecond work is most of what these sites do; rendering
		// 0.412 as "0ms" would have every panel claim the server is
		// instant.
		if got := formatMS(in); got != want {
			t.Errorf("formatMS(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatNum(t *testing.T) {
	tests := map[int64]string{0: "0", 999: "999", 1000: "1,000", 1284339: "1,284,339", -4321: "-4,321"}
	for in, want := range tests {
		if got := formatNum(in); got != want {
			t.Errorf("formatNum(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestQueryStringSkipsEmpty(t *testing.T) {
	if got := string(queryString("range", "7d", "source", "")); got != "?range=7d" {
		t.Errorf("queryString = %q, want ?range=7d", got)
	}
	if got := string(queryString("q", "a b&c")); got != "?q=a%20b%26c" {
		// A space as "+" is right for a form body and wrong in a query
		// value somebody reads in the address bar.
		t.Errorf("queryString = %q, want percent-encoding with %%20", got)
	}
	if got := string(queryString("odd")); got != "" {
		t.Errorf("queryString with an odd argument count = %q, want empty", got)
	}
}

func TestChartPolyline(t *testing.T) {
	if got := chartPolyline(nil); got != "" {
		t.Errorf("empty series = %q, want empty", got)
	}
	// One bucket has no horizontal span to distribute across, so it is drawn
	// at the midpoint rather than dividing by zero.
	if got := chartPolyline([]GraphPoint{{Count: 5}}); !strings.HasPrefix(got, "300.0,") {
		t.Errorf("single point = %q, want it at x=300", got)
	}
	got := chartPolyline([]GraphPoint{{Count: 0}, {Count: 10}})
	if !strings.HasPrefix(got, "0.0,96.0") || !strings.HasSuffix(got, "600.0,4.0") {
		t.Errorf("two points = %q; geometry must match the report's viewBox", got)
	}
}

// The Markdown report used to interpolate log text with no escaping at all,
// while the Typst report routed the same fields through typstMD. That gap was
// publicly reachable: web.Logged records r.URL.Path and Go percent-decodes it,
// so a request to any of these sites can store a path containing a real newline
// and a pipe, which forges rows in the operator's own report.
func TestMarkdownReportEscapesLogText(t *testing.T) {
	payload := "/x\n| INJECTED | 999 | 0 |\n" +
		"<img src=x onerror=alert(1)> [c](javascript:alert(2))"

	db := testDB(t)
	base := time.Now().UTC().Add(-time.Minute).UnixMilli()
	rows := make([]row, 0, minPathSamples)
	for i := 0; i < minPathSamples; i++ {
		rows = append(rows, row{
			source: "blog", ts: base, level: "ERROR", msg: "request",
			path: payload, status: 500, durationMS: 9,
		})
	}
	seedRows(t, db, rows)

	s := testSite(t, db)
	var err error
	s.renderer, err = web.NewRenderer(mustTemplates(t), templateFuncs,
		[]string{"base.html", "partials.html"},
		[]string{"home.html", "documentation.html",
			"overview.html", "source.html", "search.html", "notfound.html"})
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	s.overview(rr, httptest.NewRequest(http.MethodGet, "/overview?range=24h&report=md", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()

	// Row forgery is the worst of it: a newline plus a pipe invents a table
	// row carrying numbers the operator will read as real. No rendered line
	// may begin a cell the payload wrote.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| INJECTED") {
			t.Errorf("the payload forged a table row:\n%s", line)
		}
	}
	// The HTML and link syntax must arrive escaped rather than live. Checking
	// for the escaped form rather than the absence of the raw substring,
	// because "\<img" trivially contains "<img".
	if !strings.Contains(body, `\<img`) {
		t.Errorf("`<` was not escaped in the report:\n%s", body)
	}
	if strings.Contains(body, "[c](javascript:") {
		t.Errorf("a javascript: link survived unescaped:\n%s", body)
	}
	// And the payload is still legible as text rather than dropped.
	if !strings.Contains(body, "INJECTED") {
		t.Error("the path vanished entirely; it should be escaped, not discarded")
	}
}

// The collector snippet is identity, not a credential, but it is easy to ship
// a page whose tracking silently does nothing: an empty id renders a script
// that files events against no property. This pins both halves.
func TestAnalyticsSnippetIsRendered(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)
	var err error
	s.renderer, err = web.NewRenderer(mustTemplates(t), templateFuncs,
		[]string{"base.html", "partials.html"},
		[]string{"home.html", "documentation.html",
			"overview.html", "source.html", "search.html", "notfound.html"})
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	s.home(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rr.Body.String()

	if !strings.Contains(body, "collectorId") {
		t.Error("no collector snippet on the public home page")
	}
	if !strings.Contains(body, analyticsID) {
		t.Errorf("the snippet carries no property id, so it would track nothing")
	}
	if strings.Contains(body, `m.collectorId = s;`) && strings.Contains(body, `""`) {
		// Cheap guard against the id being interpolated as an empty string.
		if strings.Contains(body, `"https://analytics.bythewood.me", ""`) {
			t.Error("the property id rendered empty")
		}
	}
	// And the CSP actually permits what the snippet needs, which is the half
	// that is easy to get wrong when the two are edited separately.
	policy := csp()
	for _, need := range []string{"'unsafe-inline'", "https://analytics.bythewood.me"} {
		if !strings.Contains(policy, need) {
			t.Errorf("CSP is missing %s, so the collector would be blocked: %s", need, policy)
		}
	}
}

func TestReportFormat(t *testing.T) {
	tests := []struct {
		q     map[string][]string
		want  string
		found bool
	}{
		{map[string][]string{}, "", false},
		{map[string][]string{"report": {""}}, "pdf", true},
		{map[string][]string{"report": {"md"}}, "md", true},
		{map[string][]string{"report": {"docx"}}, "", false},
	}
	for _, tc := range tests {
		got, ok := reportFormat(tc.q)
		if got != tc.want || ok != tc.found {
			t.Errorf("reportFormat(%v) = %q,%v; want %q,%v", tc.q, got, ok, tc.want, tc.found)
		}
	}
}

// Every page renders against a real database, because a template that
// references a field that does not exist fails at execute time rather than at
// parse time and would otherwise only be found by loading the page.
func TestPagesRender(t *testing.T) {
	db := testDB(t)
	base := time.Now().UTC().Add(-time.Hour).UnixMilli()
	seedRows(t, db, []row{
		{source: "blog", ts: base, level: "INFO", msg: "request", path: "/", status: 200, durationMS: 1.5, cfRay: "x"},
		{source: "blog", ts: base, level: "ERROR", msg: "request", path: "/boom", status: 500, durationMS: 40},
		{source: "status", ts: base, level: "WARN", msg: "crawl slow", component: "crawler"},
	})

	s := testSite(t, db)
	var err error
	s.renderer, err = web.NewRenderer(
		mustTemplates(t), templateFuncs,
		[]string{"base.html", "partials.html"},
		[]string{"home.html", "documentation.html",
			"overview.html", "source.html", "search.html", "notfound.html"},
	)
	if err != nil {
		t.Fatalf("templates: %v", err)
	}

	pages := []struct {
		name    string
		path    string
		want    int
		handler http.HandlerFunc
	}{
		{"home", "/", http.StatusOK, s.home},
		{"documentation", "/documentation", http.StatusOK, s.documentation},
		{"overview", "/overview?range=24h", http.StatusOK, s.overview},
		{"search", "/search?q=boom", http.StatusOK, s.search},
		// The 404 page is a rendered page that happens to answer 404, and
		// the status is part of what is being asserted: a soft 404 that
		// answers 200 is the version search engines index.
		{"notfound", "/nope", http.StatusNotFound, s.notFound},
	}
	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			p.handler(rr, httptest.NewRequest(http.MethodGet, p.path, nil))
			if rr.Code != p.want {
				t.Fatalf("status = %d, want %d\n%s", rr.Code, p.want, rr.Body.String())
			}
			if rr.Body.Len() < 500 {
				t.Errorf("body is %d bytes, which is not a rendered page", rr.Body.Len())
			}
		})
	}

	// A source nobody has shipped from must 404, not render a convincing
	// dashboard of zeros. "Silent" and "never existed" are different answers
	// and this page's whole job is telling them apart.
	t.Run("unknown source is a 404", func(t *testing.T) {
		rr := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/sources/doesnotexist", nil)
		r.SetPathValue("source", "doesnotexist")
		s.sourceDetail(rr, r)
		if rr.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rr.Code)
		}
	})

	t.Run("source", func(t *testing.T) {
		rr := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/sources/blog?range=24h", nil)
		r.SetPathValue("source", "blog")
		s.sourceDetail(rr, r)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d\n%s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "blog") {
			t.Error("the source page does not name its source")
		}
	})

	// A log message is arbitrary text written by another program, so the one
	// escaping failure that matters is a record closing the inline JSON block
	// the charts are read from.
	t.Run("script tags in data are escaped", func(t *testing.T) {
		seedRows(t, db, []row{{
			source: "blog", ts: base, level: "ERROR",
			msg:  `</script><script>alert(1)</script>`,
			path: "/x", status: 500, durationMS: 1,
		}})
		rr := httptest.NewRecorder()
		s.overview(rr, httptest.NewRequest(http.MethodGet, "/overview?range=24h", nil))
		if strings.Contains(rr.Body.String(), "<script>alert(1)</script>") {
			t.Error("a logged message escaped into markup")
		}
	})
}

// A test watchdog with a clock this test owns, so silence can be simulated
// without waiting five real minutes, and with a notifier that records instead
// of publishing.
type recordedAlert struct {
	kind string
	ctx  AlertContext
}

func testWatchdog(t *testing.T, db *sql.DB, clock *time.Time) (*Watchdog, *[]recordedAlert) {
	t.Helper()
	var fired []recordedAlert
	w := NewWatchdog(db, func(kind string, ctx AlertContext) {
		fired = append(fired, recordedAlert{kind, ctx})
	})
	w.now = func() time.Time { return *clock }
	// Started long enough ago that the grace window is over. The tests that
	// care about the grace window set this back themselves.
	w.startedAt = clock.Add(-time.Hour)
	return w, &fired
}

// Every kind renders, and an unknown kind is refused rather than published as
// an empty notification. The click URL is the piece worth asserting: it is
// opened from a phone with no idea what the origin is, so a relative path is a
// dead link.
func TestRenderAlertKinds(t *testing.T) {
	for _, kind := range []string{"silence", "resumed", "restart"} {
		body, ok := renderAlert(kind, AlertContext{Source: "blog", Silent: 7 * time.Minute})
		if !ok {
			t.Fatalf("%s: not rendered", kind)
		}
		if body.Title == "" || body.Message == "" {
			t.Errorf("%s: empty title or message", kind)
		}
		if !strings.Contains(body.Title, "blog") {
			t.Errorf("%s: title %q does not name the source", kind, body.Title)
		}
		if want := "https://logging.bythewood.me/sources/blog"; body.Click != want {
			t.Errorf("%s: click = %q, want %q", kind, body.Click, want)
		}
	}

	if _, ok := renderAlert("catastrophe", AlertContext{Source: "blog"}); ok {
		t.Error("an unknown kind rendered; it must be refused rather than published empty")
	}
}

// Silence is the loud one and recovery is not, because an alert that
// interrupts for good news gets muted and takes the bad news with it.
func TestSilenceIsLouderThanRecovery(t *testing.T) {
	down, _ := renderAlert("silence", AlertContext{Source: "blog"})
	up, _ := renderAlert("resumed", AlertContext{Source: "blog"})
	if down.Priority != "high" {
		t.Errorf("silence priority = %q, want high", down.Priority)
	}
	if up.Priority != "default" {
		t.Errorf("resumed priority = %q, want default", up.Priority)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30 seconds"},
		{5 * time.Minute, "5 minutes"},
		{90 * time.Minute, "1h30m"},
		{2 * time.Hour, "2 hours"},
	}
	for _, c := range cases {
		if got := humanDuration(c.in); got != c.want {
			t.Errorf("humanDuration(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The whole rule, end to end: a source goes quiet, is reported once and not
// again, then comes back and is reported once.
//
// Firing once per transition is the part that matters. A rule that re-fires
// every thirty seconds through an overnight outage trains its reader to swipe
// it away, which costs the next alert too.
func TestWatchdogFiresSilenceOnceThenRecovery(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	w, fired := testWatchdog(t, db, &clock)

	w.Observe("blog", []web.Record{{Msg: "request"}})

	// Still inside the threshold: nothing has gone wrong yet.
	clock = clock.Add(silenceAfter - time.Second)
	w.checkSilence()
	if len(*fired) != 0 {
		t.Fatalf("fired %d alerts inside the threshold, want 0", len(*fired))
	}

	clock = clock.Add(2 * time.Second)
	w.checkSilence()
	if len(*fired) != 1 || (*fired)[0].kind != "silence" {
		t.Fatalf("alerts = %v, want one silence", *fired)
	}
	if (*fired)[0].ctx.Source != "blog" {
		t.Errorf("alerted about %q, want blog", (*fired)[0].ctx.Source)
	}

	// Still down an hour later, and still one alert.
	clock = clock.Add(time.Hour)
	w.checkSilence()
	w.checkSilence()
	if len(*fired) != 1 {
		t.Fatalf("silence re-fired: %d alerts, want 1", len(*fired))
	}

	// Back, and said so once.
	w.Observe("blog", []web.Record{{Msg: "request"}})
	w.checkSilence()
	if len(*fired) != 2 || (*fired)[1].kind != "resumed" {
		t.Fatalf("alerts = %v, want a resumed second", *fired)
	}
	if got := (*fired)[1].ctx.Silent; got < time.Hour {
		t.Errorf("recovery reported %s of silence, want at least an hour", got)
	}

	w.checkSilence()
	if len(*fired) != 2 {
		t.Fatalf("recovery re-fired: %d alerts, want 2", len(*fired))
	}
}

// Nothing alerts during the grace window after this process starts. When this
// site restarts, every other site's shipper is dropping into a refused
// connection, so the record stream has a real hole either side of a deploy and
// both rules read false inside it.
func TestWatchdogIsQuietDuringStartupGrace(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	w, fired := testWatchdog(t, db, &clock)
	w.startedAt = clock

	w.Observe("blog", []web.Record{{Msg: "request"}})
	clock = clock.Add(startupGrace - time.Minute)
	w.checkSilence()
	if len(*fired) != 0 {
		t.Fatalf("alerted inside the startup grace: %v", *fired)
	}

	clock = clock.Add(2 * time.Minute)
	w.checkSilence()
	if len(*fired) != 1 {
		t.Fatalf("alerts after the grace window = %d, want 1", len(*fired))
	}
}

// The restart rule, which is the one that reads across restarts of this
// process and therefore has to persist. A start with a stop before it is a
// deploy; a start with a start before it is a crash.
func TestWatchdogDetectsUncleanRestart(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	w, fired := testWatchdog(t, db, &clock)
	ctx := context.Background()

	// The first start ever seen has nothing to compare against and says
	// nothing. Alerting here would mean every new site announcing itself as a
	// crash.
	w.handleLifecycle(ctx, lifecycleEvent{source: "blog", up: true, at: clock})
	if len(*fired) != 0 {
		t.Fatalf("first listening alerted: %v", *fired)
	}

	// A clean deploy: stop, then start.
	clock = clock.Add(time.Hour)
	w.handleLifecycle(ctx, lifecycleEvent{source: "blog", up: false, at: clock})
	w.handleLifecycle(ctx, lifecycleEvent{source: "blog", up: true, at: clock.Add(time.Second)})
	if len(*fired) != 0 {
		t.Fatalf("a clean restart alerted: %v", *fired)
	}

	// A crash: a start with no stop before it.
	clock = clock.Add(time.Hour)
	w.handleLifecycle(ctx, lifecycleEvent{source: "blog", up: true, at: clock})
	if len(*fired) != 1 || (*fired)[0].kind != "restart" {
		t.Fatalf("alerts = %v, want one restart", *fired)
	}
	if !strings.Contains((*fired)[0].ctx.Detail, "Previous start") {
		t.Errorf("detail = %q, want it to say when the previous start was", (*fired)[0].ctx.Detail)
	}
}

// Lifecycle state survives a restart of this site, which is the only reason it
// is in the meta table rather than in memory: a crash noticed only after the
// next deploy is still a crash.
func TestWatchdogLifecycleSurvivesRestart(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	first, _ := testWatchdog(t, db, &clock)
	ctx := context.Background()

	first.handleLifecycle(ctx, lifecycleEvent{source: "blog", up: true, at: clock})

	// A whole new watchdog over the same database, as after a deploy.
	clock = clock.Add(time.Hour)
	second, fired := testWatchdog(t, db, &clock)
	second.handleLifecycle(ctx, lifecycleEvent{source: "blog", up: true, at: clock})

	if len(*fired) != 1 || (*fired)[0].kind != "restart" {
		t.Fatalf("alerts = %v, want the crash to be noticed across a restart", *fired)
	}
}

// Observe reads the lifecycle messages out of the record stream by exact text,
// so this asserts the strings it matches are the ones web/server.go actually
// logs. Renaming either there would silently disable restart detection: no
// error, no failing test anywhere else, just an alert that never fires again.
func TestLifecycleMessagesMatchServer(t *testing.T) {
	src, err := os.ReadFile("web/server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []string{msgListening, msgShutdown} {
		if !strings.Contains(string(src), `slog.Info("`+msg+`"`) {
			t.Errorf("web/server.go no longer logs %q; restart detection is now dead code", msg)
		}
	}
}

// A batch the writer refused is not evidence the source is healthy. Counting a
// 429 as liveness would mean a site whose records are all being shed looks
// perfectly alive to the silence rule, which is exactly backwards.
func TestRefusedBatchIsNotLiveness(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	w, _ := testWatchdog(t, db, &clock)

	writer := &Writer{db: db, ch: make(chan row, 2), quit: make(chan struct{}), done: make(chan struct{})}
	s := &site{db: db, writer: writer, watchdog: w}

	body := `{"source":"blog","records":[{"m":"a"},{"m":"b"},{"m":"c"}]}`
	rr := httptest.NewRecorder()
	s.ingest(rr, httptest.NewRequest(http.MethodPost, "/ingest", strings.NewReader(body)))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rr.Code)
	}

	w.mu.Lock()
	_, seen := w.seen["blog"]
	w.mu.Unlock()
	if seen {
		t.Error("a refused batch marked the source as alive")
	}
}

// Bootstrap reads rollups rather than records. Health check lines are demoted
// to rollup-only, so a site with no public traffic has hours of rollups and no
// raw rows at all, and reading records would leave a quiet site unwatched.
func TestWatchdogBootstrapsFromRollups(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	w, _ := testWatchdog(t, db, &clock)

	recent := hourFloor(clock.Add(-2 * time.Hour).UnixMilli())
	stale := hourFloor(clock.Add(-72 * time.Hour).UnixMilli())
	for _, r := range []struct {
		hour   int64
		source string
	}{{recent, "blog"}, {recent, "status"}, {stale, "retired"}} {
		if _, err := db.Exec(`INSERT INTO rollups (hour, source, level, component, status, count)
			VALUES (?,?,'INFO','healthz',200,1)`, r.hour, r.source); err != nil {
			t.Fatal(err)
		}
	}

	if err := w.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	for _, want := range []string{"blog", "status"} {
		if _, ok := w.seen[want]; !ok {
			t.Errorf("%s was not picked up; a site that is already down when this one starts would never be noticed", want)
		}
	}
	if _, ok := w.seen["retired"]; ok {
		t.Error("a source last heard from three days ago is still being watched; it would alert forever")
	}
}

// A peer container polling /healthz across the bridge is still a health check.
// This was keyed on the caller being loopback, so dash polling every site put
// thousands of probes into the raw table and into every site's request count.
func TestHealthChecksFromAPeerAreRollupOnly(t *testing.T) {
	db := testDB(t)
	base := time.Now().UTC().Add(-time.Minute).UnixMilli()

	bridge := toRow("blog", web.Record{
		Time: base, Level: "INFO", Msg: "request",
		Attrs: map[string]any{
			"path": "/healthz", "status": 200, "ms": 0.4,
			"ip": "172.18.0.11", "host": "orchard-blog:8000",
		},
	})
	public := toRow("blog", web.Record{
		Time: base, Level: "INFO", Msg: "request",
		Attrs: map[string]any{
			"path": "/healthz", "status": 200, "ms": 0.4,
			"ip": "71.71.122.88", "host": "blog.bythewood.me", "cf_ray": "a33f-IAD",
		},
	})
	for name, r := range map[string]row{"bridge": bridge, "public": public} {
		if !r.rollupOnly || r.component != "healthz" {
			t.Errorf("%s probe: rollupOnly=%v component=%q, want true and healthz",
				name, r.rollupOnly, r.component)
		}
	}

	real := toRow("blog", web.Record{
		Time: base, Level: "INFO", Msg: "request",
		Attrs: map[string]any{"path": "/", "status": 200, "ms": 5.0, "ip": "203.0.113.7"},
	})
	seedRows(t, db, []row{bridge, public, real})

	f := filter{StartMS: base - 1000, EndMS: base + 1000}
	if got := totals(context.Background(), db, f); got.Requests != 1 {
		t.Errorf("Requests = %d, want 1: probes are counted as traffic", got.Requests)
	}
}

// A held-open response is a connection lifetime. Recorded as a duration it
// drags the mean and the max somewhere they can never come back from, and the
// rollup carrying it is never swept.
func TestStreamedResponsesDoNotCountAsDurations(t *testing.T) {
	db := testDB(t)
	base := time.Now().UTC().Truncate(time.Hour).Add(30 * time.Minute).UnixMilli()

	sse := toRow("dash", web.Record{
		Time: base, Level: "INFO", Msg: "request",
		Attrs: map[string]any{"path": "/events", "status": 200, "ms": 22854322.0, "ip": "203.0.113.9"},
	})
	ws := toRow("caddy", web.Record{
		Time: base, Level: "INFO", Msg: "request",
		Attrs: map[string]any{"path": "/status/ws", "status": 101, "ms": 19366600.0, "ip": "203.0.113.10"},
	})
	for name, r := range map[string]row{"sse": sse, "websocket": ws} {
		if r.durationMS != 0 {
			t.Errorf("%s: durationMS = %v, want 0", name, r.durationMS)
		}
		if !strings.Contains(r.attrs, "stream_ms") {
			t.Errorf("%s: elapsed time was dropped rather than moved, attrs = %s", name, r.attrs)
		}
	}

	// A slow request that the server could actually have served stays a duration.
	slow := toRow("repos", web.Record{
		Time: base, Level: "INFO", Msg: "request",
		Attrs: map[string]any{"path": "/", "status": 200, "ms": 240.0, "ip": "203.0.113.11"},
	})
	if slow.durationMS != 240 {
		t.Errorf("slow request durationMS = %v, want 240", slow.durationMS)
	}
	seedRows(t, db, []row{sse, ws, slow})

	f := filter{StartMS: base - time.Hour.Milliseconds(), EndMS: base + time.Hour.Milliseconds()}
	if got := latency(context.Background(), db, f); got.Max != 240 || got.Mean != 240 {
		t.Errorf("latency mean=%v max=%v, want 240 and 240", got.Mean, got.Max)
	}
	// The rollup is the half that is never swept, so it has to be right too.
	var durCount int64
	var durSum, durMax float64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(dur_count),0), COALESCE(SUM(dur_sum),0), COALESCE(MAX(dur_max),0) FROM rollups`,
	).Scan(&durCount, &durSum, &durMax); err != nil {
		t.Fatal(err)
	}
	if durCount != 1 || durSum != 240 || durMax != 240 {
		t.Errorf("rollup dur_count=%d dur_sum=%v dur_max=%v, want 1, 240, 240", durCount, durSum, durMax)
	}
}

// A site that knows it is streaming says so, which catches the connections too
// short for the duration threshold to notice.
func TestStreamComponentIsNotADuration(t *testing.T) {
	r := toRow("dash", web.Record{
		Time: time.Now().UTC().UnixMilli(), Level: "INFO", Msg: "request",
		Attrs: map[string]any{
			"path": "/events", "status": 200, "ms": 4200.0,
			"ip": "203.0.113.9", "component": "stream",
		},
	})
	if r.durationMS != 0 {
		t.Errorf("durationMS = %v, want 0", r.durationMS)
	}
	if !strings.Contains(r.attrs, "stream_ms") {
		t.Errorf("elapsed time was dropped rather than moved, attrs = %s", r.attrs)
	}
}

// Every template the server asks for has to exist. NewRenderer resolves the
// list at boot rather than at build, so a page left listed after its file was
// deleted compiles, ships, and then crash-loops the container on startup, which
// is how it was found on repos.
func TestEveryListedTemplateParses(t *testing.T) {
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := web.NewRenderer(templates, templateFuncs, layoutTemplates, pageTemplates); err != nil {
		t.Fatalf("the template set does not parse: %v", err)
	}
}
