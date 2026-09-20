package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// No migration table. Every statement is IF NOT EXISTS, so this runs against an
// existing database as a no-op and a schema change is a new guarded block.
//
// History is why several of these tables exist. A listing that has sat for ninety
// days and dropped twice is a different house to one that came on yesterday at
// the same price, and only keeping every snapshot tells you that.
const schema = `
CREATE TABLE IF NOT EXISTS listings (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    mls           TEXT,
    source        TEXT NOT NULL,
    address       TEXT NOT NULL,
    city          TEXT,
    state         TEXT,
    zip           TEXT,
    county        TEXT,
    lat           REAL,
    lon           REAL,
    price         INTEGER NOT NULL,
    beds          REAL,
    baths         REAL,
    sqft          INTEGER,
    acres         REAL,
    year_built    INTEGER,
    style         TEXT,
    property_type TEXT,
    hoa_monthly   REAL,
    tax_annual    REAL,
    days_on_market INTEGER,
    status        TEXT,
    listing_url   TEXT,
    remarks       TEXT,
    first_seen    INTEGER NOT NULL,
    last_seen     INTEGER NOT NULL,
    gone_at       INTEGER,
    -- What a URL says. The row id stays the integer every foreign key here
    -- points at, and this is the only thing a link ever carries, so a report can
    -- be handed to somebody without also handing them every other report by
    -- counting up from one.
    public_id     TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS listings_mls ON listings(mls) WHERE mls IS NOT NULL AND mls != '';
CREATE INDEX IF NOT EXISTS listings_addr ON listings(address, zip);
CREATE INDEX IF NOT EXISTS listings_latlon ON listings(lat, lon);

CREATE TABLE IF NOT EXISTS price_history (
    listing_id  INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    price       INTEGER NOT NULL,
    seen_at     INTEGER NOT NULL,
    PRIMARY KEY (listing_id, seen_at)
);

CREATE TABLE IF NOT EXISTS photos (
    listing_id  INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    idx         INTEGER NOT NULL,
    url         TEXT NOT NULL,
    local       TEXT,
    PRIMARY KEY (listing_id, idx)
);

-- One row per listing, rewritten by a refresh. Columns rather than a JSON blob
-- because the grid sorts on most of them.
CREATE TABLE IF NOT EXISTS facts (
    listing_id        INTEGER PRIMARY KEY REFERENCES listings(id) ON DELETE CASCADE,
    computed_at       INTEGER NOT NULL,

    flood_zone        TEXT,
    flood_sfha        INTEGER,
    flood_subtype     TEXT,
    water_feet        REAL,
    water_name        TEXT,
    water_kind        TEXT,
    perennial_feet    REAL,
    perennial_name    TEXT,
    ditch_feet        REAL,

    market_value      REAL,
    land_value        REAL,
    acres_from        TEXT,
    parcel_address    TEXT,
    value_json        TEXT,
    parcel_lat        REAL,
    parcel_lon        REAL,
    zoning            TEXT,
    area_json         TEXT NOT NULL DEFAULT '{}',

    relief_feet       REAL,
    mean_slope_pct    REAL,
    flat_share        REAL,
    fence             TEXT,
    outings_json      TEXT NOT NULL DEFAULT '{}',
    street_json       TEXT NOT NULL DEFAULT '{}',

    aadt              INTEGER,
    aadt_route        TEXT,
    aadt_feet         REAL,
    road_class        TEXT,
    road_name         TEXT,
    road_class_feet   REAL,
    corner            INTEGER NOT NULL DEFAULT 0,
    corner_roads      TEXT NOT NULL DEFAULT '',
    ramp_feet         REAL,
    ramp_name         TEXT,
    highway_feet      REAL,
    highway_name      TEXT,

    elem_name         TEXT,
    middle_name       TEXT,
    high_name         TEXT,
    zoning_source     TEXT,
    zoning_verified   INTEGER NOT NULL DEFAULT 0,
    zoning_nearest    INTEGER NOT NULL DEFAULT 0,
    elem_miles        REAL,
    middle_miles      REAL,
    high_miles        REAL,
    elem_grade        TEXT,
    middle_grade      TEXT,
    high_grade        TEXT,

    commute_min       REAL,
    commute_miles     REAL,
    dropoff_min       REAL,
    dropoff_miles     REAL,
    detour_min        REAL,
    middle_detour_min REAL,
    high_detour_min   REAL,
    opposite_ways     INTEGER NOT NULL DEFAULT 0,
    bearing           REAL,

    monthly_total     REAL,
    monthly_pi        REAL,
    monthly_tax       REAL,
    monthly_ins       REAL,
    monthly_pmi       REAL,
    monthly_hoa       REAL,
    monthly_util      REAL,
    monthly_dpa       REAL,

    score             REAL,
    breakdown         TEXT NOT NULL DEFAULT '{}',
    excluded          INTEGER NOT NULL DEFAULT 0,
    exclude_reasons   TEXT NOT NULL DEFAULT '[]',
    penalty_total     REAL NOT NULL DEFAULT 0,
    stretch           INTEGER NOT NULL DEFAULT 0,
    notes_json        TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS facts_score ON facts(excluded, score DESC);

-- Drive times to each configured destination, one row per listing per place.
CREATE TABLE IF NOT EXISTS drives (
    listing_id  INTEGER NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    place_key   TEXT NOT NULL,
    minutes     REAL,
    miles       REAL,
    PRIMARY KEY (listing_id, place_key)
);

-- The reader's own verdict, which outranks every computed number here and is the
-- one thing in this database a refresh must never overwrite.
CREATE TABLE IF NOT EXISTS verdicts (
    listing_id  INTEGER PRIMARY KEY REFERENCES listings(id) ON DELETE CASCADE,
    rating      INTEGER NOT NULL DEFAULT 0,
    note        TEXT NOT NULL DEFAULT '',
    updated_at  INTEGER NOT NULL
);

-- Caches. Every one of these answers is a request to somebody else's server for
-- a fact that does not change, so asking twice is rude as well as slow.
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

CREATE TABLE IF NOT EXISTS lookups (
    kind        TEXT NOT NULL,
    key         TEXT NOT NULL,
    payload     TEXT NOT NULL,
    fetched_at  INTEGER NOT NULL,
    PRIMARY KEY (kind, key)
);

-- The circuit breaker state, here rather than in memory so a restart does not
-- hand a tripped upstream a fresh round of requests.
CREATE TABLE IF NOT EXISTS guards (
    endpoint     TEXT PRIMARY KEY,
    failures     INTEGER NOT NULL DEFAULT 0,
    opened_at    INTEGER NOT NULL DEFAULT 0,
    window_start INTEGER NOT NULL DEFAULT 0,
    window_count INTEGER NOT NULL DEFAULT 0,
    trips        INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at  INTEGER NOT NULL,
    finished_at INTEGER,
    source      TEXT,
    seen        INTEGER NOT NULL DEFAULT 0,
    added       INTEGER NOT NULL DEFAULT 0,
    changed     INTEGER NOT NULL DEFAULT 0,
    note        TEXT NOT NULL DEFAULT ''
);
`

// openDB opens the SQLite database and applies the schema. modernc.org/sqlite is
// pure Go, so CGO_ENABLED=0 and the binary needs no libc. Pragmas go in the DSN
// because they are per connection and database/sql opens connections lazily.
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
		"&_pragma=foreign_keys(ON)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := addMissingColumns(db); err != nil {
		return nil, err
	}
	// The index is created here rather than in the schema above, because that block
	// runs before the column migration and an existing database has no such column
	// yet, which fails the whole apply.
	if _, err := db.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS listings_public ON listings(public_id) WHERE public_id IS NOT NULL`); err != nil {
		return nil, err
	}
	if err := fillPublicIDs(db); err != nil {
		return nil, err
	}
	return db, nil
}

// CREATE TABLE IF NOT EXISTS does nothing to a table that already exists, so a
// column added to the schema above reaches a fresh database and no other. Every
// test starts on a fresh one, so no test can see this.
//
// SQLite has no ADD COLUMN IF NOT EXISTS, so the existing columns are read first.
// A column added here has to be added to the schema above as well.
var addedColumns = []struct{ table, column, decl string }{
	{"guards", "trips", "INTEGER NOT NULL DEFAULT 0"},
	{"facts", "penalty_total", "REAL NOT NULL DEFAULT 0"},
	{"facts", "road_class_feet", "REAL"},
	{"facts", "corner", "INTEGER NOT NULL DEFAULT 0"},
	{"facts", "corner_roads", "TEXT NOT NULL DEFAULT ''"},
	{"facts", "ramp_feet", "REAL"},
	{"facts", "ramp_name", "TEXT"},
	{"facts", "highway_feet", "REAL"},
	{"facts", "highway_name", "TEXT"},
	{"facts", "water_kind", "TEXT"},
	{"facts", "perennial_feet", "REAL"},
	{"facts", "perennial_name", "TEXT"},
	{"facts", "ditch_feet", "REAL"},
	{"facts", "relief_feet", "REAL"},
	{"facts", "mean_slope_pct", "REAL"},
	{"facts", "flat_share", "REAL"},
	{"facts", "fence", "TEXT"},
	{"facts", "outings_json", "TEXT NOT NULL DEFAULT '{}'"},
	{"facts", "street_json", "TEXT NOT NULL DEFAULT '{}'"},
	{"facts", "zoning_nearest", "INTEGER NOT NULL DEFAULT 0"},
	{"facts", "elem_miles", "REAL"},
	{"facts", "middle_miles", "REAL"},
	{"facts", "high_miles", "REAL"},
	{"facts", "high_detour_min", "REAL"},
	{"facts", "market_value", "REAL"},
	{"facts", "land_value", "REAL"},
	{"facts", "acres_from", "TEXT"},
	{"facts", "parcel_address", "TEXT"},
	{"listings", "public_id", "TEXT"},
	{"facts", "value_json", "TEXT"},
	{"facts", "parcel_lat", "REAL"},
	{"facts", "parcel_lon", "REAL"},
	{"facts", "zoning", "TEXT"},
	{"facts", "area_json", "TEXT NOT NULL DEFAULT '{}'"},
}

func addMissingColumns(db *sql.DB) error {
	for _, c := range addedColumns {
		have, err := hasColumn(db, c.table, c.column)
		if err != nil {
			return err
		}
		if have {
			continue
		}
		if _, err := db.Exec("ALTER TABLE " + c.table + " ADD COLUMN " + c.column + " " + c.decl); err != nil {
			return fmt.Errorf("add %s.%s: %w", c.table, c.column, err)
		}
	}
	return nil
}

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// newPublicID is the token a shared link carries. Sixteen random bytes as hex,
// laid out like a UUID so it reads as an opaque identifier rather than something
// worth editing. Guessing one is not a thing anybody is going to do.
func newPublicID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// Version and variant bits, so it is a well formed v4 rather than something
	// that merely looks like one.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// fillPublicIDs gives a token to every row that predates the column, so an
// existing database keeps working and its links stop being a row number.
func fillPublicIDs(db *sql.DB) error {
	rows, err := db.Query(`SELECT id FROM listings WHERE public_id IS NULL OR public_id = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range ids {
		pid, err := newPublicID()
		if err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE listings SET public_id = ? WHERE id = ?`, pid, id); err != nil {
			return err
		}
	}
	return nil
}
