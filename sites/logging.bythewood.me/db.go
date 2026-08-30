package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// records holds raw lines, kept for rawRetention and searchable; rollups holds
// hourly counters kept forever, so a year-long graph is a few hundred row reads
// rather than a scan. Hot attributes get columns, the rest lands in attrs JSON.
const schema = `
CREATE TABLE IF NOT EXISTS records (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source      TEXT    NOT NULL,
    ts          INTEGER NOT NULL,
    level       TEXT    NOT NULL,
    msg         TEXT    NOT NULL,
    component   TEXT    NOT NULL DEFAULT '',
    method      TEXT    NOT NULL DEFAULT '',
    path        TEXT    NOT NULL DEFAULT '',
    host        TEXT    NOT NULL DEFAULT '',
    status      INTEGER NOT NULL DEFAULT 0,
    duration_ms REAL    NOT NULL DEFAULT 0,
    ip          TEXT    NOT NULL DEFAULT '',
    cf_ray      TEXT    NOT NULL DEFAULT '',
    attrs       TEXT    NOT NULL DEFAULT '{}'
);

-- Every dashboard query leads with a time window, so ts leads every index.
-- msg and path are unindexed: LIKE runs over an already time-bounded set.
CREATE INDEX IF NOT EXISTS records_ts           ON records(ts);
CREATE INDEX IF NOT EXISTS records_source_ts    ON records(source, ts);
CREATE INDEX IF NOT EXISTS records_level_ts     ON records(level, ts);
CREATE INDEX IF NOT EXISTS records_status_ts    ON records(status, ts) WHERE status >= 400;

-- Written in the same transaction as the raw rows, so a rollup is never behind
-- and a raw row ageing out never takes its count with it. Duration is sum,
-- count and max: a percentile needs the distribution, which is the raw rows.
CREATE TABLE IF NOT EXISTS rollups (
    hour        INTEGER NOT NULL,
    source      TEXT    NOT NULL,
    level       TEXT    NOT NULL,
    component   TEXT    NOT NULL DEFAULT '',
    status      INTEGER NOT NULL DEFAULT 0,
    count       INTEGER NOT NULL DEFAULT 0,
    dur_count   INTEGER NOT NULL DEFAULT 0,
    dur_sum     REAL    NOT NULL DEFAULT 0,
    dur_max     REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY (hour, source, level, component, status)
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS rollups_source_hour ON rollups(source, hour);

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// openDB opens the database and applies the schema. Pragmas go in the DSN
// because they are per connection and database/sql opens connections lazily;
// auto_vacuum in particular only takes effect on an empty file.
func openDB(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	dsn := path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=auto_vacuum(INCREMENTAL)" +
		"&_pragma=foreign_keys(ON)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite takes a database-wide write lock, so a second writing connection
	// buys SQLITE_BUSY rather than throughput. The flusher is the only writer.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return db, nil
}

// hourFloor truncates unix milliseconds to the hour the rollup is keyed by.
func hourFloor(ms int64) int64 {
	const hourMS = 60 * 60 * 1000
	return ms - ms%hourMS
}
