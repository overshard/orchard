// The key ring and the call log.
//
// One file, two tables, and the same reasoning chat's history uses: this holds
// the full text of every prompt and every completion the estate has produced,
// so it is its own database rather than tables beside anything else, and
// deleting it is one command that touches nothing else. secure_delete is on, so
// a deleted call is zeroed rather than left in the free pages.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS keys (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  -- SHA-256 and not Argon2id. A key is 32 bytes this service generated, so
  -- there is nothing to brute force and no reason to spend 100ms of every
  -- request proving it. Argon2id is for repos, where the secret is chosen by
  -- a person.
  hash       TEXT NOT NULL UNIQUE,
  -- The first characters, so a key is recognisable in a list without being
  -- recoverable from one.
  prefix     TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used  INTEGER NOT NULL DEFAULT 0,
  revoked_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS calls (
  id          INTEGER PRIMARY KEY,
  key_id      INTEGER NOT NULL DEFAULT 0,
  caller      TEXT NOT NULL DEFAULT '',
  model       TEXT NOT NULL DEFAULT '',
  messages    TEXT NOT NULL DEFAULT '[]',
  completion  TEXT NOT NULL DEFAULT '',
  tools       TEXT NOT NULL DEFAULT '',
  prompt_tok  INTEGER NOT NULL DEFAULT 0,
  output_tok  INTEGER NOT NULL DEFAULT 0,
  decode_tps  REAL NOT NULL DEFAULT 0,
  ms          INTEGER NOT NULL DEFAULT 0,
  status      INTEGER NOT NULL DEFAULT 0,
  err         TEXT NOT NULL DEFAULT '',
  at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS calls_at ON calls(at DESC);
CREATE INDEX IF NOT EXISTS calls_caller ON calls(caller, at DESC);
`

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=secure_delete(ON)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close checkpoints the write ahead log first, so a container stopped with
// SIGKILL rather than SIGTERM loses at most the call in flight.
func (s *Store) Close() error {
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		slog.Warn("wal checkpoint", "err", err)
	}
	return s.db.Close()
}

func (s *Store) Checkpoint() {
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		slog.Debug("wal checkpoint", "err", err)
	}
}

type Key struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Prefix  string    `json:"prefix"`
	Created time.Time `json:"created"`
	Used    time.Time `json:"used,omitempty"`
	Revoked bool      `json:"revoked"`
}

// keyPrefix is on every key this service issues, so one found in a config file
// is identifiable as belonging here rather than to some other provider.
const keyPrefix = "orch-"

// NewKey mints a key and returns the plaintext exactly once. Nothing keeps it,
// so a lost key is reissued rather than recovered.
func (s *Store) NewKey(name string) (string, Key, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", Key{}, errors.New("a key needs a name saying what will use it")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", Key{}, err
	}
	secret := keyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	sum := hashKey(secret)
	now := time.Now().Unix()
	r, err := s.db.Exec(`INSERT INTO keys(name, hash, prefix, created_at) VALUES(?,?,?,?)`,
		name, sum, secret[:len(keyPrefix)+6], now)
	if err != nil {
		return "", Key{}, err
	}
	id, _ := r.LastInsertId()
	return secret, Key{ID: id, Name: name, Prefix: secret[:len(keyPrefix)+6], Created: time.Unix(now, 0)}, nil
}

func hashKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Authenticate resolves a presented key. A revoked key is not a match, and the
// comparison is constant time even though the lookup is by hash, because the
// row is fetched by hash and then confirmed rather than trusted.
func (s *Store) Authenticate(secret string) (Key, bool) {
	if !strings.HasPrefix(secret, keyPrefix) {
		return Key{}, false
	}
	sum := hashKey(secret)
	var k Key
	var created, used, revoked int64
	var stored string
	err := s.db.QueryRow(`SELECT id, name, prefix, hash, created_at, last_used, revoked_at FROM keys WHERE hash=?`, sum).
		Scan(&k.ID, &k.Name, &k.Prefix, &stored, &created, &used, &revoked)
	if err != nil {
		return Key{}, false
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(sum)) != 1 {
		return Key{}, false
	}
	if revoked != 0 {
		return Key{}, false
	}
	k.Created, k.Used = time.Unix(created, 0), time.Unix(used, 0)
	return k, true
}

func (s *Store) TouchKey(id int64) {
	if _, err := s.db.Exec(`UPDATE keys SET last_used=? WHERE id=?`, time.Now().Unix(), id); err != nil {
		slog.Debug("touch key", "err", err)
	}
}

func (s *Store) Keys() ([]Key, error) {
	rows, err := s.db.Query(`SELECT id, name, prefix, created_at, last_used, revoked_at FROM keys ORDER BY revoked_at, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Key{}
	for rows.Next() {
		var k Key
		var created, used, revoked int64
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &created, &used, &revoked); err != nil {
			return nil, err
		}
		k.Created, k.Revoked = time.Unix(created, 0), revoked != 0
		if used > 0 {
			k.Used = time.Unix(used, 0)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Revoke is not a delete, so the call log keeps pointing at a real key and the
// history of what a retired service asked for stays readable.
func (s *Store) Revoke(id int64) error {
	_, err := s.db.Exec(`UPDATE keys SET revoked_at=? WHERE id=? AND revoked_at=0`, time.Now().Unix(), id)
	return err
}

func (s *Store) DeleteKey(id int64) error {
	_, err := s.db.Exec(`DELETE FROM keys WHERE id=?`, id)
	return err
}

// Call is one request through the gateway, prompt and completion included.
type Call struct {
	ID         int64     `json:"id"`
	KeyID      int64     `json:"key_id"`
	Caller     string    `json:"caller"`
	Model      string    `json:"model"`
	Messages   string    `json:"messages"`
	Completion string    `json:"completion"`
	Tools      string    `json:"tools,omitempty"`
	PromptTok  int       `json:"prompt_tokens"`
	OutputTok  int       `json:"output_tokens"`
	DecodeTPS  float64   `json:"decode_tps"`
	MS         int64     `json:"ms"`
	Status     int       `json:"status"`
	Err        string    `json:"err,omitempty"`
	At         time.Time `json:"at"`
}

func (s *Store) LogCall(c Call) {
	_, err := s.db.Exec(`INSERT INTO calls
		(key_id, caller, model, messages, completion, tools, prompt_tok, output_tok, decode_tps, ms, status, err, at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.KeyID, c.Caller, c.Model, c.Messages, c.Completion, c.Tools,
		c.PromptTok, c.OutputTok, c.DecodeTPS, c.MS, c.Status, c.Err, time.Now().Unix())
	if err != nil {
		slog.Error("logging a call failed", "err", err)
	}
}

func (s *Store) Calls(caller string, limit int) ([]Call, error) {
	q := `SELECT id, key_id, caller, model, messages, completion, tools,
	             prompt_tok, output_tok, decode_tps, ms, status, err, at
	      FROM calls`
	args := []any{}
	if caller != "" {
		q += ` WHERE caller=?`
		args = append(args, caller)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Call{}
	for rows.Next() {
		var c Call
		var at int64
		if err := rows.Scan(&c.ID, &c.KeyID, &c.Caller, &c.Model, &c.Messages, &c.Completion,
			&c.Tools, &c.PromptTok, &c.OutputTok, &c.DecodeTPS, &c.MS, &c.Status, &c.Err, &at); err != nil {
			return nil, err
		}
		c.At = time.Unix(at, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

// Usage is the per caller roll up the overview page shows.
type Usage struct {
	Caller    string  `json:"caller"`
	Calls     int     `json:"calls"`
	PromptTok int     `json:"prompt_tokens"`
	OutputTok int     `json:"output_tokens"`
	Errors    int     `json:"errors"`
	AvgTPS    float64 `json:"avg_tps"`
}

func (s *Store) Usage(since time.Time) ([]Usage, error) {
	rows, err := s.db.Query(`
		SELECT caller, COUNT(*), COALESCE(SUM(prompt_tok),0), COALESCE(SUM(output_tok),0),
		       COALESCE(SUM(CASE WHEN status >= 400 OR err <> '' THEN 1 ELSE 0 END),0),
		       COALESCE(AVG(NULLIF(decode_tps,0)),0)
		FROM calls WHERE at >= ? GROUP BY caller ORDER BY COUNT(*) DESC`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Usage{}
	for rows.Next() {
		var u Usage
		if err := rows.Scan(&u.Caller, &u.Calls, &u.PromptTok, &u.OutputTok, &u.Errors, &u.AvgTPS); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Prune drops calls older than the retention window. The prompts are the whole
// of what anyone asked this estate, so they do not accumulate forever by
// default.
func (s *Store) Prune(keep time.Duration) (int64, error) {
	r, err := s.db.Exec(`DELETE FROM calls WHERE at < ?`, time.Now().Add(-keep).Unix())
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

func (s *Store) Counts() (keys, calls int) {
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM keys WHERE revoked_at=0`).Scan(&keys)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM calls`).Scan(&calls)
	return
}
