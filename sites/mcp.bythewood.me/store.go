package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
-- Refresh tokens already swapped for new ones, kept until they would have
-- expired, so a stolen copy turning up later ends the grant.
CREATE TABLE IF NOT EXISTS spent (
    hash     BLOB    PRIMARY KEY,
    grant_id INTEGER NOT NULL,
    at       INTEGER NOT NULL,
    expires  INTEGER NOT NULL
) WITHOUT ROWID;
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
CREATE INDEX IF NOT EXISTS calls_tool ON calls(tool, at);
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
	// A spent refresh token sent again this soon is two refreshes racing
	// from the same client, which is refused without ending the grant.
	reuseGrace = time.Minute
)

type Store struct{ db *sql.DB }

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	if err := addColumns(db, "calls", map[string]string{
		"rpc_id": "TEXT NOT NULL DEFAULT ''",
		"code":   "INTEGER NOT NULL DEFAULT 0",
	}); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// addColumns adds any of cols a table doesn't have yet, since SQLite has no
// ADD COLUMN IF NOT EXISTS.
func addColumns(db *sql.DB, table string, cols map[string]string) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			have[n] = true
		}
	}
	rows.Close()
	for _, n := range slices.Sorted(maps.Keys(cols)) {
		if have[n] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + n + ` ` + cols[n]); err != nil {
			return err
		}
	}
	return nil
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

var errReused = errors.New("a spent refresh token came back")

// Spend swaps a refresh token for its grant in one statement, so two refreshes
// with the same token can't both win. A token spent past reuseGrace ago ends
// the grant and returns errReused with the grant's id.
func (s *Store) Spend(token, clientID string) (int64, error) {
	now := time.Now()
	h := tokenHash(token)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var grant, expires int64
	err = tx.QueryRow(`DELETE FROM tokens WHERE hash = ? AND kind = 'refresh' AND expires > ?
		AND grant_id IN (SELECT id FROM grants WHERE revoked = 0 AND client_id = ?)
		RETURNING grant_id, expires`, h, now.Unix(), clientID).Scan(&grant, &expires)
	if err == nil {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO spent (hash, grant_id, at, expires) VALUES (?, ?, ?, ?)`,
			h, grant, now.Unix(), expires); err != nil {
			return 0, err
		}
		return grant, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var at int64
	err = tx.QueryRow(`SELECT grant_id, at FROM spent WHERE hash = ? AND expires > ?`, h, now.Unix()).Scan(&grant, &at)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && now.Sub(time.Unix(at, 0)) < reuseGrace) {
		return 0, errNoToken
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE grants SET revoked = 1 WHERE id = ?`, grant); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM tokens WHERE grant_id = ?`, grant); err != nil {
		return 0, err
	}
	return grant, errors.Join(errReused, tx.Commit())
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
	// RPCID is the request's JSON-RPC id as it was sent. Code is the error
	// code of a JSON-RPC error reply, or the HTTP status of a reply that
	// wasn't JSON-RPC at all, like the challenge after a revoke.
	RPCID string
	Code  int
}

func (s *Store) AddCall(c Call) {
	_, _ = s.db.Exec(`INSERT INTO calls (at, grant_id, client, method, tool, args, ms, ok, err, chars, preview, rpc_id, code) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.At.UnixMilli(), c.Grant, c.Client, c.Method, c.Tool, c.Args, c.MS, c.OK, c.Err, c.Chars, c.Preview, c.RPCID, c.Code)
}

// CallFilter narrows the calls table. Q matches anywhere in the arguments and
// ID picks out one call.
type CallFilter struct {
	Tool, Client, Q string
	Range           string
	Failed          bool
	ID              int64
	Since           time.Time
	Limit           int
}

func (f CallFilter) where() (string, []any) {
	q := ` WHERE at >= ?`
	args := []any{f.Since.UnixMilli()}
	if f.ID > 0 {
		q += ` AND id = ?`
		args = append(args, f.ID)
	}
	if f.Tool != "" {
		q += ` AND tool = ?`
		args = append(args, f.Tool)
	}
	if f.Client != "" {
		q += ` AND client = ?`
		args = append(args, f.Client)
	}
	if f.Q != "" {
		q += ` AND args LIKE ? ESCAPE '\'`
		args = append(args, "%"+likeEscape(f.Q)+"%")
	}
	if f.Failed {
		q += ` AND ok = 0`
	}
	return q, args
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *Store) Calls(f CallFilter) ([]Call, error) {
	where, args := f.where()
	if f.Limit <= 0 {
		f.Limit = 200
	}
	rows, err := s.db.Query(`SELECT id, at, grant_id, client, method, tool, args, ms, ok, err, chars, preview, rpc_id, code FROM calls`+where+` ORDER BY id DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Call
	for rows.Next() {
		var c Call
		var at int64
		if rows.Scan(&c.ID, &at, &c.Grant, &c.Client, &c.Method, &c.Tool, &c.Args, &c.MS, &c.OK, &c.Err, &c.Chars, &c.Preview, &c.RPCID, &c.Code) == nil {
			c.At = time.UnixMilli(at)
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// CountCalls is how many calls match a filter, failed ones too, ignoring Limit.
func (s *Store) CountCalls(f CallFilter) (n, failed int) {
	where, args := f.where()
	_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(ok = 0), 0) FROM calls`+where, args...).Scan(&n, &failed)
	return n, failed
}

// Clients is every connector name that made a call since a time.
func (s *Store) Clients(since time.Time) []string {
	rows, err := s.db.Query(`SELECT DISTINCT client FROM calls WHERE at >= ? ORDER BY client`, since.UnixMilli())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out
}

// LastHandshake is when each connector last opened a session with the server,
// by initialize or server/discover.
func (s *Store) LastHandshake() map[string]time.Time {
	out := map[string]time.Time{}
	rows, err := s.db.Query(`SELECT client, MAX(at) FROM calls WHERE method IN ('initialize', 'server/discover') GROUP BY client`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		var at int64
		if rows.Scan(&c, &at) == nil {
			out[c] = time.UnixMilli(at)
		}
	}
	return out
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
	_, _ = s.db.Exec(`DELETE FROM spent WHERE expires < ?`, now.Unix())
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
