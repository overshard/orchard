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
