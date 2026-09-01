package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Nothing here stores a credential in a form it can be read back from. Session
// identifiers are kept as a SHA-256 of the cookie value, recovery codes and
// login codes as Argon2id, so a copy of this file is not a way in.
const schema = `
-- One row, enforced by the CHECK. A table rather than a constant because
-- renaming is a feature, and because ntfy_account names a different namespace
-- that ntfy itself cannot rename, so the two have to be free to drift apart.
CREATE TABLE IF NOT EXISTS users (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    username     TEXT    NOT NULL,
    ntfy_account TEXT    NOT NULL,
    created      INTEGER NOT NULL
);

-- Opaque random identifiers rather than a signed payload, which is what makes
-- revocation possible: a signed cookie is valid until it expires no matter what
-- this table says, and the only way to end one is to rotate the signing key and
-- end every other session with it.
CREATE TABLE IF NOT EXISTS sessions (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    hash      BLOB    NOT NULL UNIQUE,
    created   INTEGER NOT NULL,
    last_seen INTEGER NOT NULL,
    expires   INTEGER NOT NULL,
    -- When this session last proved possession of the phone. Regenerating
    -- recovery codes wants a recent one, so a stolen cookie cannot rotate
    -- the credentials it would need to keep itself alive.
    sudo_at   INTEGER NOT NULL DEFAULT 0,
    ip        TEXT    NOT NULL DEFAULT '',
    country   TEXT    NOT NULL DEFAULT '',
    city      TEXT    NOT NULL DEFAULT '',
    ua        TEXT    NOT NULL DEFAULT '',
    cf_ray    TEXT    NOT NULL DEFAULT '',
    revoked   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS sessions_live ON sessions(hash) WHERE revoked = 0;

-- At most one unconsumed row at a time. A second login request while one is
-- outstanding returns the same page and publishes nothing, which is what
-- collapses a flood of requests into one notification per window.
--
-- browser_hash binds the code to whoever asked for it. Without it a code pushed
-- to the phone could be typed into somebody else's session, which is the whole
-- attack that push OTP is otherwise wide open to.
CREATE TABLE IF NOT EXISTS pending_logins (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    code_hash    BLOB    NOT NULL,
    code_salt    BLOB    NOT NULL,
    browser_hash BLOB    NOT NULL,
    created      INTEGER NOT NULL,
    expires      INTEGER NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    consumed     INTEGER NOT NULL DEFAULT 0,
    ip           TEXT    NOT NULL DEFAULT '',
    country      TEXT    NOT NULL DEFAULT '',
    city         TEXT    NOT NULL DEFAULT '',
    ua           TEXT    NOT NULL DEFAULT ''
);

-- One row per notification actually published. The per account ceiling counts
-- these rather than requests, and it ignores the source address:
-- per-IP limits are bypassed by sending one request from each of a thousand
-- proxies, and this is the ceiling that still holds when that happens.
CREATE TABLE IF NOT EXISTS sends (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS sends_ts ON sends(ts);

-- The break-glass, ten at a time, Argon2id hashed like repos' push tokens.
-- prefix is the first four characters in clear, which finds the right row
-- without hashing all ten and gives nothing away on its own.
CREATE TABLE IF NOT EXISTS recovery_codes (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    prefix  TEXT    NOT NULL,
    hash    BLOB    NOT NULL,
    salt    BLOB    NOT NULL,
    created INTEGER NOT NULL,
    used_at INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS recovery_live ON recovery_codes(prefix) WHERE used_at = 0;

-- The local half of the audit trail. Every row here also ships to
-- logging.bythewood.me, and this copy exists because the first thing to do
-- after an alert is look, and logging may be the thing that is down.
--
-- No code, expired or otherwise, is ever written here. Retention outlives
-- incident response.
CREATE TABLE IF NOT EXISTS events (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    ts      INTEGER NOT NULL,
    kind    TEXT    NOT NULL,
    ip      TEXT    NOT NULL DEFAULT '',
    country TEXT    NOT NULL DEFAULT '',
    city    TEXT    NOT NULL DEFAULT '',
    ua      TEXT    NOT NULL DEFAULT '',
    cf_ray  TEXT    NOT NULL DEFAULT '',
    detail  TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS events_ts ON events(ts);
`

// openDB opens the database and applies the schema. Pragmas go in the DSN
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
	return db, nil
}
