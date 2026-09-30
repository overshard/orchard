package main

import (
	"context"
	"database/sql"
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
	if _, err := db.Exec(
		`INSERT INTO `+table+` (property_id, event, created_at) VALUES (?,?,?)`,
		id[:], "page_view", at); err != nil {
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
	// rather than being hidden by a generous margin.
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

// A fresh database is created with auto_vacuum INCREMENTAL, which is what lets
// the sweep hand pages back rather than only stopping the file from growing.
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

// One event a day for 90 days has to chart as full weeks, with no cliff at the
// end where the newest bucket ran past the last day.
func TestAWeeklyGraphCountsEveryWeekInFull(t *testing.T) {
	db, id := retentionTestDB(t)
	end := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 90; i++ {
		seedEvent(t, db, "events", id, end.AddDate(0, 0, -i).Add(12*time.Hour).UnixMilli())
	}
	start := end.AddDate(0, 0, -89)
	points := eventsGraph(context.Background(), db, id, start.UnixMilli(), end.Add(24*time.Hour).UnixMilli()-1, "", end, 90)

	if len(points) != 12 {
		t.Fatalf("%d points, want 12 weeks", len(points))
	}
	for _, p := range points {
		if p.Count != 7 {
			t.Errorf("%s counted %d, want 7", p.Label, p.Count)
		}
	}
	if last := points[len(points)-1].Label; last != "Sep 30" {
		t.Errorf("the newest week is labelled %q, want Sep 30", last)
	}
}
