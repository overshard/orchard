package main

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// A database from before the method column must open and gain it, with its
// existing sessions left as unknown.
func TestMigrateAddsMethodToAnOldDatabase(t *testing.T) {
	path := t.TempDir() + "/db.sqlite3"
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE sessions (
        id INTEGER PRIMARY KEY AUTOINCREMENT, hash BLOB NOT NULL UNIQUE, created INTEGER NOT NULL,
        last_seen INTEGER NOT NULL, expires INTEGER NOT NULL, sudo_at INTEGER NOT NULL DEFAULT 0,
        ip TEXT NOT NULL DEFAULT '', country TEXT NOT NULL DEFAULT '', city TEXT NOT NULL DEFAULT '',
        ua TEXT NOT NULL DEFAULT '', cf_ray TEXT NOT NULL DEFAULT '', revoked INTEGER NOT NULL DEFAULT 0);
        INSERT INTO sessions (hash, created, last_seen, expires) VALUES (x'01', 1, 1, 9999999999);`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	for range 2 {
		db, err := openDB(path)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		var method string
		if err := db.QueryRow(`SELECT method FROM sessions`).Scan(&method); err != nil || method != "" {
			t.Fatalf("method %q, %v", method, err)
		}
		db.Close()
	}
}
