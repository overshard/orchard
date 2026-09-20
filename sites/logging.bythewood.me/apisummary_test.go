package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// The reason an error happened lives in the attrs bag, because a slog call
// names the operation in the message and puts the error itself in an attribute.
// The dashboard has always rendered that bag and this endpoint used to drop it,
// so chat could say a site was erroring and never say what went wrong. That is
// what this pins.
func TestAPIErrorsCarryTheReason(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	seedErrors(t, db, []row{{
		source: "search", ts: now.Add(-5 * time.Minute).UnixMilli(),
		level: "ERROR", msg: "shipping a batch failed", component: "shipper",
		attrs: `{"err":"dial tcp 172.18.0.9:8000: connect: connection refused","dropped":41}`,
	}})

	s := &site{db: db}
	got := s.apiRecentErrors(context.Background(), since(now, 24), 20, "", "")
	if len(got) != 1 {
		t.Fatalf("want one group, got %d", len(got))
	}
	if !strings.Contains(got[0].Details, "connection refused") {
		t.Errorf("details lost the reason: %q", got[0].Details)
	}
	if got[0].Component != "shipper" {
		t.Errorf("component = %q, want shipper", got[0].Component)
	}
}

// Twenty rows of the same failure is one problem, and reporting it twenty times
// spends the whole answer saying so without ever saying how long it has run.
func TestAPIErrorsGroupByKind(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	var rows []row
	for i := 0; i < 12; i++ {
		rows = append(rows, row{
			source: "repos", ts: now.Add(-time.Duration(i) * time.Minute).UnixMilli(),
			level: "ERROR", msg: "git subprocess failed", path: "/orchard.git/info/refs",
			attrs: `{"err":"exit status 128"}`,
		})
	}
	rows = append(rows, row{
		source: "blog", ts: now.Add(-time.Minute).UnixMilli(),
		level: "ERROR", msg: "render failed", attrs: `{"err":"template: no such file"}`,
	})
	seedErrors(t, db, rows)

	s := &site{db: db}
	got := s.apiRecentErrors(context.Background(), since(now, 24), 20, "", "")
	if len(got) != 2 {
		t.Fatalf("want two groups, got %d: %+v", len(got), got)
	}
	var git *apiError
	for i := range got {
		if got[i].Message == "git subprocess failed" {
			git = &got[i]
		}
	}
	if git == nil {
		t.Fatal("the repeated error did not come back")
	}
	if git.Count != 12 {
		t.Errorf("count = %d, want 12", git.Count)
	}
	if git.FirstSeen == git.LastSeen {
		t.Error("a group spanning eleven minutes reported no span")
	}
	// One path across the group, so naming it is right rather than misleading.
	if git.Path != "/orchard.git/info/refs" {
		t.Errorf("path = %q, want the one path the group shares", git.Path)
	}
}

// Narrowing is what makes a second look worth taking. Without it the follow up
// to "search is erroring" is the same twenty lines again.
func TestAPIErrorsFilter(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	seedErrors(t, db, []row{
		{source: "search", ts: now.UnixMilli(), level: "ERROR", msg: "upstream timeout",
			attrs: `{"err":"context deadline exceeded"}`},
		{source: "repos", ts: now.UnixMilli(), level: "ERROR", msg: "git subprocess failed",
			attrs: `{"err":"exit status 128"}`},
	})
	s := &site{db: db}
	ctx := context.Background()

	if got := s.apiRecentErrors(ctx, since(now, 24), 20, "search", ""); len(got) != 1 || got[0].Source != "search" {
		t.Errorf("source filter returned %+v", got)
	}
	// The bag is searched too, since the reason is the part worth grepping and
	// it is never in the message.
	if got := s.apiRecentErrors(ctx, since(now, 24), 20, "", "deadline"); len(got) != 1 || got[0].Source != "search" {
		t.Errorf("contains filter did not reach the attrs bag: %+v", got)
	}
	if got := s.apiRecentErrors(ctx, since(now, 24), 20, "", "nothing matches this"); len(got) != 0 {
		t.Errorf("want no rows, got %+v", got)
	}
}

// A stack trace pasted into an attribute must not become most of the answer.
func TestAPIErrorDetailsAreCapped(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	seedErrors(t, db, []row{{
		source: "chat", ts: now.UnixMilli(), level: "ERROR", msg: "panic recovered",
		attrs: `{"err":"` + strings.Repeat("goroutine 1 stack ", 400) + `"}`,
	}})
	s := &site{db: db}
	got := s.apiRecentErrors(context.Background(), since(now, 24), 20, "", "")
	if len(got) != 1 {
		t.Fatalf("want one group, got %d", len(got))
	}
	if len(got[0].Details) > maxDetailLen+3 {
		t.Errorf("details ran to %d bytes, cap is %d", len(got[0].Details), maxDetailLen)
	}
}

func since(now time.Time, hours int) int64 {
	return now.Add(-time.Duration(hours) * time.Hour).Truncate(time.Hour).UnixMilli()
}

// Straight into the table rather than through the writer, since these tests are
// about what comes back out and not about batching.
func seedErrors(t *testing.T, db *sql.DB, rows []row) {
	t.Helper()
	for _, r := range rows {
		if r.attrs == "" {
			r.attrs = "{}"
		}
		_, err := db.Exec(`INSERT INTO records
			(source, ts, level, msg, component, method, path, host, status, duration_ms, ip, cf_ray, attrs)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.source, r.ts, r.level, r.msg, r.component, r.method, r.path, r.host,
			r.status, r.durationMS, r.ip, r.cfRay, r.attrs)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// Asked "any suspicious logs in orchard" on 2026-09-11 the summary said there
// were none, and it was telling the truth about errors on a day the edge turned
// away thousands of scanner probes. A refused request is not an ERROR anywhere,
// so a reader with only the errors list cannot answer the question at all.
func TestAPIRefusedSeesScannersWithNoErrors(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	var rows []row
	for i := 0; i < 30; i++ {
		rows = append(rows, row{
			source: "blog", ts: now.Add(-time.Duration(i) * time.Minute).UnixMilli(),
			level: "INFO", msg: "request", component: "http", method: "GET",
			path: "/userfiles", host: "blog.bythewood.me", status: 404,
			ip: "203.0.113.7", cfRay: "abc123",
		})
	}
	rows = append(rows, row{
		source: "blog", ts: now.Add(-time.Minute).UnixMilli(),
		level: "INFO", msg: "request", component: "http", method: "GET",
		path: "/", host: "blog.bythewood.me", status: 200,
		ip: "203.0.113.9", cfRay: "def456",
	})
	seedErrors(t, db, rows)

	s := &site{db: db}
	if errs := s.apiRecentErrors(context.Background(), since(now, 24), 20, "", ""); len(errs) != 0 {
		t.Fatalf("the seed has no errors, got %d", len(errs))
	}

	got := s.apiRefusedSummary(context.Background(), since(now, 24), 15)
	if got.Client4xx != 30 {
		t.Errorf("client_4xx = %d, want 30", got.Client4xx)
	}
	if got.Requests != 31 {
		t.Errorf("requests = %d, want 31", got.Requests)
	}
	if len(got.TopPaths) != 1 || got.TopPaths[0].Path != "/userfiles" {
		t.Fatalf("top 4xx paths = %+v, want one row for /userfiles", got.TopPaths)
	}
	if got.TopPaths[0].Hits != 30 || got.TopPaths[0].Clients != 1 {
		t.Errorf("top path = %+v, want 30 hits from 1 client", got.TopPaths[0])
	}
}

// Every container probes itself over loopback with no CF-Ray, and the sites
// call each other across the docker bridge all day, which together were 97% of
// this number on the first day it existed. Counting them buries the one request
// that really did skip Cloudflare, which is the only thing the number is for.
func TestAPIRefusedDirectHitsExcludePrivateAddresses(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	seedErrors(t, db, []row{
		{source: "blog", ts: now.UnixMilli(), level: "INFO", msg: "request", component: "http",
			method: "GET", path: "/", host: "blog.bythewood.me", status: 200, ip: "127.0.0.1"},
		{source: "blog", ts: now.UnixMilli(), level: "INFO", msg: "request", component: "http",
			method: "GET", path: "/", host: "blog.bythewood.me", status: 200, ip: "::1"},
		{source: "blog", ts: now.UnixMilli(), level: "INFO", msg: "request", component: "http",
			method: "GET", path: "/aggregate", host: "logging.bythewood.me", status: 200, ip: "172.18.0.4"},
		{source: "blog", ts: now.UnixMilli(), level: "INFO", msg: "request", component: "http",
			method: "GET", path: "/latest.json", host: "blog.bythewood.me", status: 200, ip: "10.0.0.9"},
		{source: "blog", ts: now.UnixMilli(), level: "INFO", msg: "request", component: "http",
			method: "GET", path: "/wp-login.php", host: "blog.bythewood.me", status: 404, ip: "198.51.100.4"},
		{source: "blog", ts: now.UnixMilli(), level: "INFO", msg: "request", component: "http",
			method: "GET", path: "/", host: "blog.bythewood.me", status: 200, ip: "198.51.100.5", cfRay: "ray"},
	})

	got := (&site{db: db}).apiRefusedSummary(context.Background(), since(now, 24), 15)
	if got.DirectHits != 1 {
		t.Errorf("direct_hits = %d, want 1: only the public address with no ray counts", got.DirectHits)
	}
}
