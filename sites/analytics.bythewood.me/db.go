package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// schema is the Rust version's migrations/0001_initial.sql, byte for byte in
// intent and column for column in fact.
//
// That is the requirement, not a convenience. There is a production database
// at /srv/data/analytics/db.sqlite3 with years of events in it, and the whole
// point of keeping the schema identical is that the cutover is a container
// swap rather than a data migration. Every statement is IF NOT EXISTS so
// pointing this at the existing file is a no-op.
//
// sqlx's migration table is deliberately absent. It recorded that exactly one
// migration had ever run, which is not a fact worth a table; if a second
// schema change ever lands it can be a numbered block here with its own
// guard.
const schema = `
CREATE TABLE IF NOT EXISTS properties (
    id            BLOB PRIMARY KEY,
    name          TEXT NOT NULL,
    custom_cards  TEXT NOT NULL DEFAULT '[]',
    is_protected  INTEGER NOT NULL DEFAULT 0,
    is_public     INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    property_id     BLOB NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    event           TEXT NOT NULL,
    created_at      INTEGER NOT NULL,
    user_id         TEXT,
    url             TEXT,
    title           TEXT,
    referrer        TEXT,
    user_agent      TEXT,
    platform        TEXT,
    browser         TEXT,
    device          TEXT,
    screen_width    INTEGER,
    screen_height   INTEGER,
    country         TEXT,
    region          TEXT,
    city            TEXT,
    lat             REAL,
    lon             REAL,
    utm_source      TEXT,
    utm_medium      TEXT,
    utm_campaign    TEXT,
    utm_term        TEXT,
    utm_content     TEXT,
    time_on_page_ms INTEGER,
    extra           TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS events_property_created       ON events(property_id, created_at);
CREATE INDEX IF NOT EXISTS events_property_event_created ON events(property_id, event, created_at);

CREATE TABLE IF NOT EXISTS bot_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    property_id  BLOB NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    event        TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    bot_name     TEXT,
    url          TEXT,
    user_agent   TEXT,
    country      TEXT,
    extra        TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS bot_events_property_created ON bot_events(property_id, created_at);

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// openDB opens the SQLite database and applies the schema.
//
// The driver is modernc.org/sqlite, which is SQLite transpiled to Go rather
// than bound to it. That is what keeps CGO_ENABLED=0 available, and CGO off is
// what decisions/0008 identified as the property that had to survive the move
// off Rust: a static binary. It is slower than mattn/go-sqlite3 on a
// benchmark, and this app writes a few hundred rows a day.
//
// The pragmas are the Rust connect options, restated. They are passed in the
// DSN because they are per connection, and database/sql opens connections
// lazily and silently: setting them once after Open would apply them to
// whichever connection happened to serve that call and to no other.
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

	// SQLite takes a database-wide write lock, so concurrent writers do not
	// buy throughput, they buy SQLITE_BUSY. Readers are the reason the pool is
	// larger than one: WAL lets them run while a write is in flight.
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

// ensureProprium returns the id of the property this app uses to track itself,
// creating it on first boot.
//
// The id is stored in meta rather than derived, because it is baked into a
// collector snippet: regenerating it would orphan every event already
// recorded against the old one. The existence check against properties covers
// the case where meta survived a database the events did not.
func ensureProprium(ctx context.Context, db *sql.DB) (uuid.UUID, error) {
	var stored string
	err := db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'proprium_id'").Scan(&stored)
	if err != nil && err != sql.ErrNoRows {
		return uuid.Nil, err
	}
	if err == nil {
		if id, perr := uuid.Parse(stored); perr == nil {
			var found []byte
			ferr := db.QueryRowContext(ctx,
				"SELECT id FROM properties WHERE id = ?", id[:]).Scan(&found)
			if ferr == nil {
				return id, nil
			}
			if ferr != sql.ErrNoRows {
				return uuid.Nil, ferr
			}
		}
	}

	id := uuid.New()
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO properties (id, name, custom_cards, is_protected, is_public, created_at, updated_at)
		 VALUES (?, 'Proprium', '[]', 1, 0, ?, ?)`,
		id[:], now, now); err != nil {
		return uuid.Nil, err
	}
	if _, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO meta (key, value) VALUES ('proprium_id', ?)",
		id.String()); err != nil {
		return uuid.Nil, err
	}
	log.Printf("created Proprium property %s", id)
	return id, nil
}

// Property is one tracked site.
type Property struct {
	ID          uuid.UUID
	Name        string
	CustomCards []CustomCard
	IsProtected bool
	IsPublic    bool
	CreatedAt   int64
	UpdatedAt   int64
}

// CustomCard is a custom event the operator has pinned to the dashboard as a
// metric card. Value is the pinned/unpinned flag; the name is historical and
// is what the stored JSON already uses, so it stays.
type CustomCard struct {
	Event string `json:"event"`
	Value bool   `json:"value"`
}

const propertyColumns = `id, name, custom_cards, is_protected, is_public, created_at, updated_at`

// scanProperty reads one row of propertyColumns.
//
// Malformed custom_cards JSON degrades to no cards rather than failing the
// request: it is dashboard decoration written by a form, and a dashboard that
// refuses to load because one card is corrupt is worse than one missing a
// card.
func scanProperty(scan func(...any) error) (*Property, error) {
	var (
		p     Property
		idRaw []byte
		cards string
		prot  int64
		pub   int64
	)
	if err := scan(&idRaw, &p.Name, &cards, &prot, &pub, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	id, err := uuid.FromBytes(idRaw)
	if err != nil {
		return nil, fmt.Errorf("property id is not a uuid: %w", err)
	}
	p.ID = id
	p.IsProtected = prot != 0
	p.IsPublic = pub != 0
	if err := json.Unmarshal([]byte(cards), &p.CustomCards); err != nil {
		p.CustomCards = nil
	}
	return &p, nil
}

// lookupProperty returns the property with this id, or nil when there is none.
func lookupProperty(ctx context.Context, db *sql.DB, id uuid.UUID) (*Property, error) {
	row := db.QueryRowContext(ctx,
		"SELECT "+propertyColumns+" FROM properties WHERE id = ?", id[:])
	p, err := scanProperty(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}
