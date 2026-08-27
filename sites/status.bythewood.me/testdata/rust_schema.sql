-- The Rust version migrations/0001_initial.sql and 0002_phase_timings.sql,
-- concatenated verbatim. This is the shape of the production database the Go
-- port has to accept without a migration; see TestSchemaAcceptsRustDatabase.

-- A "property" is a tracked URL: the unit of monitoring. The original Django
-- schema had per-user properties; the rust port collapses to single-operator
-- so user_id is gone, but the rest of the columns line up 1:1 (preserving
-- UUIDs lets prior public status URLs keep working after migration).
CREATE TABLE properties (
    id                          BLOB PRIMARY KEY,
    url                         TEXT NOT NULL,
    is_public                   INTEGER NOT NULL DEFAULT 0,
    is_protected                INTEGER NOT NULL DEFAULT 0,

    last_run_at                 INTEGER,
    next_run_at                 INTEGER,

    last_run_at_crawler         INTEGER,
    next_run_at_crawler         INTEGER,
    crawler_insights            TEXT,
    crawl_state                 TEXT NOT NULL DEFAULT 'idle',
    crawl_started_at            INTEGER,
    last_crawl_success_at       INTEGER,
    last_crawl_error            TEXT,
    last_crawl_duration_ms      INTEGER,
    last_crawl_pages_count      INTEGER,

    lighthouse_scores           TEXT,
    lighthouse_details          TEXT,
    last_lighthouse_run_at      INTEGER,
    last_lighthouse_success_at  INTEGER,
    last_lighthouse_error       TEXT,
    last_lighthouse_duration_ms INTEGER,
    next_lighthouse_run_at      INTEGER,
    lighthouse_state            TEXT NOT NULL DEFAULT 'idle',
    lighthouse_started_at       INTEGER,

    -- alert state machine: "up" or "down". Transitions only fire alerts.
    alert_state                 TEXT NOT NULL DEFAULT 'up',
    last_alert_sent             INTEGER,

    created_at                  INTEGER NOT NULL,
    updated_at                  INTEGER NOT NULL
);
CREATE INDEX properties_url ON properties(url);

-- Result of a single HTTP check. Cleaned up after 3 days by the scheduler.
CREATE TABLE checks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    property_id  BLOB NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    status_code  INTEGER NOT NULL,
    response_ms  INTEGER NOT NULL DEFAULT 0,
    headers      TEXT NOT NULL DEFAULT '{}',
    created_at   INTEGER NOT NULL
);
CREATE INDEX checks_created_at         ON checks(created_at);
CREATE INDEX checks_property_created   ON checks(property_id, created_at DESC);

-- Key-value table for one-off settings (schema version, etc.).
CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
-- Per-phase timing breakdown for HTTP checks. Pre-existing rows leave
-- these NULL; the dashboard chart skips nulls. response_ms remains the
-- canonical total (alert email avg still reads it).
ALTER TABLE checks ADD COLUMN dns_ms  INTEGER;
ALTER TABLE checks ADD COLUMN tcp_ms  INTEGER;
ALTER TABLE checks ADD COLUMN tls_ms  INTEGER;
ALTER TABLE checks ADD COLUMN ttfb_ms INTEGER;
