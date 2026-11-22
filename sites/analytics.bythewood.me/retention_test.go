package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func retentionTestDB(t *testing.T) (*sql.DB, uuid.UUID) {
	t.Helper()

	db, err := openDB(filepath.Join(t.TempDir(), "db.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	id := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO properties (id, name, created_at, updated_at) VALUES (?,?,?,?)`,
		id[:], "test", time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	return db, id
}

func seedEvent(t *testing.T, db *sql.DB, table string, id uuid.UUID, at int64) {
	t.Helper()
	stmt := `INSERT INTO ` + table + ` (property_id, event, created_at) VALUES (?,'page_view',?)`
	switch table {
	case "vitals":
		stmt = `INSERT INTO vitals (property_id, created_at) VALUES (?,?)`
	case "page_errors":
		stmt = `INSERT INTO page_errors (property_id, created_at, kind, message) VALUES (?,?,'js','x')`
	}
	if _, err := db.Exec(stmt, id[:], at); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSweepDropsEventsPastRetention(t *testing.T) {
	db, id := retentionTestDB(t)
	now := time.Now().UTC()

	// A day either side of the boundary, so an off-by-a-window error shows up
	// instead of hiding behind a generous margin.
	stale := now.Add(-eventRetention - 24*time.Hour).UnixMilli()
	keep := now.Add(-eventRetention + 24*time.Hour).UnixMilli()

	for _, table := range sweptTables {
		seedEvent(t, db, table, id, stale)
		seedEvent(t, db, table, id, stale)
		seedEvent(t, db, table, id, keep)
	}

	NewSweeper(db).sweep(context.Background())

	for _, table := range sweptTables {
		if got := count(t, db, table); got != 1 {
			t.Errorf("%s = %d rows, want 1: only the row inside the window survives", table, got)
		}
	}

	// The sweep is about traffic tables, and deleting the property row instead
	// would take every dashboard with it.
	if got := count(t, db, "properties"); got != 1 {
		t.Errorf("properties = %d, want 1: properties are never swept", got)
	}
}

func TestSweepKeepsEverythingInsideRetention(t *testing.T) {
	db, id := retentionTestDB(t)

	for _, table := range sweptTables {
		seedEvent(t, db, table, id, time.Now().Add(-time.Hour).UnixMilli())
	}

	NewSweeper(db).sweep(context.Background())

	for _, table := range sweptTables {
		if got := count(t, db, table); got != 1 {
			t.Errorf("%s = %d rows, want 1", table, got)
		}
	}
}

// A fresh database gets auto_vacuum INCREMENTAL so the sweep can hand pages
// back instead of only stopping the file from growing.
func TestFreshDatabaseCanReclaim(t *testing.T) {
	db, _ := retentionTestDB(t)

	var mode int
	if err := db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != 2 {
		t.Fatalf("auto_vacuum = %d, want 2 (INCREMENTAL)", mode)
	}
	if got := NewSweeper(db).reclaim(context.Background()); got != "reclaimed" {
		t.Errorf("reclaim() = %q, want %q", got, "reclaimed")
	}
}

func seedView(t *testing.T, db *sql.DB, id uuid.UUID, at int64, user string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO events (property_id, event, created_at, user_id) VALUES (?,?,?,?)`,
		id[:], "page_view", at, user); err != nil {
		t.Fatal(err)
	}
}

// One visitor a day for 90 days has to chart as full weeks, with no cliff at
// the end where the newest bucket ran past the last day.
func TestAWeeklyGraphCountsEveryWeekInFull(t *testing.T) {
	db, id := retentionTestDB(t)
	end := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for i := range 90 {
		seedView(t, db, id, end.AddDate(0, 0, -i).Add(12*time.Hour).UnixMilli(), fmt.Sprint("u", i))
	}
	start := end.AddDate(0, 0, -89)
	b := newBuckets(end, 90)
	p := readPeriod(context.Background(), db, newScope(id, start.UnixMilli(), end.Add(24*time.Hour).UnixMilli()-1, nil), b)

	if len(b.labels) != 12 {
		t.Fatalf("%d buckets, want 12 weeks", len(b.labels))
	}
	for i, v := range p.visitors {
		if v != 7 {
			t.Errorf("%s counted %v, want 7", b.labels[i], v)
		}
	}
	if last := b.labels[len(b.labels)-1]; last != "Sep 30" {
		t.Errorf("the newest week is labelled %q, want Sep 30", last)
	}
}

// 9pm Eastern is already tomorrow in UTC, and it has to count on his day.
func TestAnEveningVisitCountsOnTheEasternDay(t *testing.T) {
	db, id := retentionTestDB(t)
	evening := time.Date(2026, 9, 29, 21, 0, 0, 0, eastern)
	seedView(t, db, id, evening.UnixMilli(), "u")

	end := time.Date(2026, 9, 30, 0, 0, 0, 0, eastern)
	startMS, _ := parseDateToMS("2026-09-29", false)
	endMS, _ := parseDateToMS("2026-09-30", true)
	b := newBuckets(end, 2)
	p := readPeriod(context.Background(), db, newScope(id, startMS, endMS, nil), b)

	if len(b.labels) != 2 || b.labels[0] != "Sep 29" || p.visitors[0] != 1 || p.visitors[1] != 0 {
		t.Errorf("got %v %v, want the visit on Sep 29", b.labels, p.visitors)
	}
}

// A new connection must be able to read while the sweep holds the write lock,
// which a write-taking pragma in the DSN breaks.
func TestNewConnectionReadsUnderWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM properties`); err != nil {
		t.Fatal(err)
	}

	fresh, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fresh.Close() })

	start := time.Now()
	var n int
	if err := fresh.QueryRow(`SELECT COUNT(*) FROM properties`).Scan(&n); err != nil {
		t.Fatalf("read on a new connection: %v", err)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("read waited %v for the writer", waited)
	}
}
