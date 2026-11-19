package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// A grant is one connector you allowed, holding the auth session it reads
// with. Tokens are only ever stored hashed, and the session is what auth
// handed over, since it has to be sent on to each tool.
const schema = `
CREATE TABLE IF NOT EXISTS grants (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id   TEXT    NOT NULL,
    client_name TEXT    NOT NULL,
    session     TEXT    NOT NULL,
    resource    TEXT    NOT NULL,
    created     INTEGER NOT NULL,
    last_used   INTEGER NOT NULL DEFAULT 0,
    revoked     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS tokens (
    hash     BLOB    PRIMARY KEY,
    grant_id INTEGER NOT NULL REFERENCES grants(id),
    kind     TEXT    NOT NULL,
    expires  INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS tokens_grant ON tokens(grant_id);
CREATE TABLE IF NOT EXISTS calls (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    at       INTEGER NOT NULL,
    grant_id INTEGER NOT NULL,
    client   TEXT    NOT NULL,
    method   TEXT    NOT NULL,
    tool     TEXT    NOT NULL DEFAULT '',
    args     TEXT    NOT NULL DEFAULT '',
    ms       INTEGER NOT NULL,
    ok       INTEGER NOT NULL,
    err      TEXT    NOT NULL DEFAULT '',
    chars    INTEGER NOT NULL DEFAULT 0,
    preview  TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS calls_at ON calls(at);
CREATE TABLE IF NOT EXISTS events (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    at     INTEGER NOT NULL,
    kind   TEXT    NOT NULL,
    client TEXT    NOT NULL DEFAULT '',
    detail TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS events_at ON events(at);
`

// keepFor is how long calls and events stay, the same as llm keeps its calls.
const keepFor = 90 * 24 * time.Hour

const (
	accessTTL  = time.Hour
	refreshTTL = 60 * 24 * time.Hour
)

type Store struct{ db *sql.DB }

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

type Grant struct {
	ID         int64
	ClientID   string
	ClientName string
	Session    string
	Resource   string
	Created    time.Time
	LastUsed   time.Time
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func tokenHash(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

func (s *Store) AddGrant(clientID, name, session, resource string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO grants (client_id, client_name, session, resource, created) VALUES (?, ?, ?, ?, ?)`,
		clientID, name, session, resource, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Issue makes a fresh access and refresh token pair for a grant.
func (s *Store) Issue(grant int64) (access, refresh string, err error) {
	access, refresh = randomToken(), randomToken()
	now := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM tokens WHERE expires < ?`, now.Unix()); err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(`INSERT INTO tokens (hash, grant_id, kind, expires) VALUES (?, ?, 'access', ?), (?, ?, 'refresh', ?)`,
		tokenHash(access), grant, now.Add(accessTTL).Unix(), tokenHash(refresh), grant, now.Add(refreshTTL).Unix()); err != nil {
		return "", "", err
	}
	return access, refresh, tx.Commit()
}

var errNoToken = errors.New("no such token")

// Lookup finds the live grant behind a token of the given kind.
func (s *Store) Lookup(token, kind string) (Grant, error) {
	var g Grant
	var created, used int64
	err := s.db.QueryRow(`SELECT g.id, g.client_id, g.client_name, g.session, g.resource, g.created, g.last_used
		FROM tokens t JOIN grants g ON g.id = t.grant_id
		WHERE t.hash = ? AND t.kind = ? AND t.expires > ? AND g.revoked = 0`,
		tokenHash(token), kind, time.Now().Unix()).Scan(&g.ID, &g.ClientID, &g.ClientName, &g.Session, &g.Resource, &created, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return g, errNoToken
	}
	g.Created, g.LastUsed = time.Unix(created, 0), unixOrZero(used)
	return g, err
}

// Spend removes a refresh token once it's been used, since every refresh hands
// out a new one.
func (s *Store) Spend(token string) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE hash = ?`, tokenHash(token))
	return err
}

func (s *Store) Touch(grant int64) {
	_, _ = s.db.Exec(`UPDATE grants SET last_used = ? WHERE id = ? AND last_used < ?`, time.Now().Unix(), grant, time.Now().Add(-time.Minute).Unix())
}

// Revoke ends a grant and every token it holds, for when auth says its
// session is gone.
func (s *Store) Revoke(grant int64) {
	_, _ = s.db.Exec(`UPDATE grants SET revoked = 1 WHERE id = ?`, grant)
	_, _ = s.db.Exec(`DELETE FROM tokens WHERE grant_id = ?`, grant)
}

func (s *Store) Grants() ([]Grant, error) {
	rows, err := s.db.Query(`SELECT id, client_id, client_name, resource, created, last_used FROM grants WHERE revoked = 0 ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		var created, used int64
		if rows.Scan(&g.ID, &g.ClientID, &g.ClientName, &g.Resource, &created, &used) == nil {
			g.Created, g.LastUsed = time.Unix(created, 0), unixOrZero(used)
			out = append(out, g)
		}
	}
	return out, rows.Err()
}

// Call is one request an agent made, kept with what it asked and a short
// preview of the answer so a strange result can be checked later.
type Call struct {
	ID      int64
	At      time.Time
	Grant   int64
	Client  string
	Method  string
	Tool    string
	Args    string
	MS      int64
	OK      bool
	Err     string
	Chars   int
	Preview string
}

func (s *Store) AddCall(c Call) {
	_, _ = s.db.Exec(`INSERT INTO calls (at, grant_id, client, method, tool, args, ms, ok, err, chars, preview) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.At.UnixMilli(), c.Grant, c.Client, c.Method, c.Tool, c.Args, c.MS, c.OK, c.Err, c.Chars, c.Preview)
}

type CallFilter struct {
	Tool, Client string
	Failed       bool
	Since        time.Time
	Limit        int
}

func (s *Store) Calls(f CallFilter) ([]Call, error) {
	q := `SELECT id, at, grant_id, client, method, tool, args, ms, ok, err, chars, preview FROM calls WHERE at >= ?`
	args := []any{f.Since.UnixMilli()}
	if f.Tool != "" {
		q += ` AND tool = ?`
		args = append(args, f.Tool)
	}
	if f.Client != "" {
		q += ` AND client = ?`
		args = append(args, f.Client)
	}
	if f.Failed {
		q += ` AND ok = 0`
	}
	if f.Limit <= 0 {
		f.Limit = 200
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Call
	for rows.Next() {
		var c Call
		var at int64
		if rows.Scan(&c.ID, &at, &c.Grant, &c.Client, &c.Method, &c.Tool, &c.Args, &c.MS, &c.OK, &c.Err, &c.Chars, &c.Preview) == nil {
			c.At = time.UnixMilli(at)
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// Event is something that happened to a connector rather than a call it
// made: allowed, refused, refreshed, ended.
type Event struct {
	At     time.Time
	Kind   string
	Client string
	Detail string
}

func (s *Store) AddEvent(kind, client, detail string) {
	_, _ = s.db.Exec(`INSERT INTO events (at, kind, client, detail) VALUES (?, ?, ?, ?)`, time.Now().UnixMilli(), kind, client, detail)
}

func (s *Store) Events(limit int) ([]Event, error) {
	rows, err := s.db.Query(`SELECT at, kind, client, detail FROM events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var at int64
		if rows.Scan(&at, &e.Kind, &e.Client, &e.Detail) == nil {
			e.At = time.UnixMilli(at)
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// Sweep drops calls and events past keepFor and the tokens that ran out.
func (s *Store) Sweep(now time.Time) {
	cut := now.Add(-keepFor).UnixMilli()
	_, _ = s.db.Exec(`DELETE FROM calls WHERE at < ?`, cut)
	_, _ = s.db.Exec(`DELETE FROM events WHERE at < ?`, cut)
	_, _ = s.db.Exec(`DELETE FROM tokens WHERE expires < ?`, now.Unix())
}

// GrantUse is how much each live grant has been used in the last week.
func (s *Store) GrantUse(since time.Time) map[int64]int {
	out := map[int64]int{}
	rows, err := s.db.Query(`SELECT grant_id, COUNT(*) FROM calls WHERE at >= ? AND tool != '' GROUP BY grant_id`, since.UnixMilli())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var g int64
		var n int
		if rows.Scan(&g, &n) == nil {
			out[g] = n
		}
	}
	return out
}

// unixOrZero keeps a never as a zero time, which time.Unix(0, 0) isn't.
func unixOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
