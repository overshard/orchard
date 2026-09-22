package property

import (
	"database/sql"
	"fmt"
)

// Every table here is a cache. None of it is a record of anything, so losing the
// lot costs a rebuild of the lookups and nothing else, which is why it lives in
// chat's own database rather than a volume of its own.
//
// Every statement is IF NOT EXISTS and this runs at startup, so a schema change
// is a new guarded block and never a migration.
const schema = `
-- What somebody else's server said about a fixed fact. A flood zone does not
-- change between Tuesdays, so asking twice is rude as well as slow.
CREATE TABLE IF NOT EXISTS lookups (
    kind        TEXT NOT NULL,
    key         TEXT NOT NULL,
    payload     TEXT NOT NULL,
    fetched_at  INTEGER NOT NULL,
    PRIMARY KEY (kind, key)
);

CREATE TABLE IF NOT EXISTS geocodes (
    query       TEXT PRIMARY KEY,
    lat         REAL,
    lon         REAL,
    matched     TEXT,
    source      TEXT,
    fetched_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS routes (
    key         TEXT PRIMARY KEY,
    seconds     REAL NOT NULL,
    meters      REAL NOT NULL,
    geometry    TEXT,
    fetched_at  INTEGER NOT NULL
);

-- The circuit breaker state, in the database rather than in memory so a restart
-- does not hand a struggling upstream a fresh round of requests.
CREATE TABLE IF NOT EXISTS guards (
    endpoint     TEXT PRIMARY KEY,
    failures     INTEGER NOT NULL DEFAULT 0,
    opened_at    INTEGER NOT NULL DEFAULT 0,
    window_start INTEGER NOT NULL DEFAULT 0,
    window_count INTEGER NOT NULL DEFAULT 0,
    trips        INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT NOT NULL DEFAULT ''
);

-- A whole assembled report, keyed on the normalized address. A follow up asking
-- about the schools at a house already looked up costs one read here, which is
-- the difference between a conversation and forty requests to other people.
CREATE TABLE IF NOT EXISTS reports (
    key         TEXT PRIMARY KEY,
    address     TEXT NOT NULL,
    lat         REAL NOT NULL,
    lon         REAL NOT NULL,
    payload     TEXT NOT NULL,
    built_at    INTEGER NOT NULL
);
`

// Migrate applies the schema. It is separate from opening the database because
// chat owns that, and this package only adds its own tables to it.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("property schema: %w", err)
	}
	return nil
}
