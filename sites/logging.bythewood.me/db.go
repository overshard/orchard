package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// The schema, written for one workload: append constantly, read in ranges, and
// stay bounded without anybody remembering to prune it.
//
// Two tables carry the data and they answer different questions. `records` is
// the raw line, kept for rawRetention and searchable; `rollups` is an hourly
// counter kept forever, which is what makes a year-long graph a few hundred row
// reads instead of a scan over everything ever logged. Analytics inserts one
// row at a time, keeps all 130,207 of them and has no retention story because
// it never needed one; one site's request log beats a hundred rows in an
// afternoon, and there are five sites behind this. So both halves are here in
// the first commit rather than after the first crisis.
//
// Hot attributes get real columns and everything else lands in `attrs` as JSON,
// which is the same split analytics made between typed columns and its `extra`
// blob. The hot set is exactly what the request log already emits, so latency
// percentiles, status distributions and error rates cost nothing extra.
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

-- Every dashboard query is "this window, optionally this source, optionally
-- this level", so the leading column is always ts and the two filters get a
-- composite each. Nothing indexes msg or path: those are searched with LIKE
-- over an already time-bounded set, which is a scan of a few thousand rows
-- rather than of the table.
CREATE INDEX IF NOT EXISTS records_ts           ON records(ts);
CREATE INDEX IF NOT EXISTS records_source_ts    ON records(source, ts);
CREATE INDEX IF NOT EXISTS records_level_ts     ON records(level, ts);
CREATE INDEX IF NOT EXISTS records_status_ts    ON records(status, ts) WHERE status >= 400;

-- Hourly counters, kept forever. Written in the same transaction as the raw
-- rows rather than by a nightly job, so a rollup is never behind and a raw row
-- ageing out never takes its count with it.
--
-- duration is sum, count and max rather than percentiles: an exact percentile
-- needs the distribution, which is what the raw rows are for. Inside
-- rawRetention the dashboard computes p50/p95/p99 from records; beyond it the
-- honest answer is a mean and a worst case, and saying so beats a number that
-- looks precise and is not.
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

// openDB opens the database and applies the schema.
//
// The pragmas go in the DSN rather than in statements after Open, because they
// are per connection and database/sql opens connections lazily: setting them
// once afterwards applies them to whichever connection served that call and to
// no other. Analytics paid for that lesson; this one inherits it.
//
// auto_vacuum is the one pragma here that analytics does not set, and it is the
// difference between a database that shrinks and one that only ever grows. It
// takes effect only on an empty file, so it has to be right on the first boot;
// on an existing database it is silently ignored and a full VACUUM would be the
// only way back. Retention deleting 30 days of rows out of a file that never
// gives the space back is a bounded row count and an unbounded disk file.
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

	// One writer, several readers. SQLite takes a database-wide write lock, so
	// a second writing connection buys SQLITE_BUSY rather than throughput, and
	// here there is genuinely one writer by construction: the flusher.
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
