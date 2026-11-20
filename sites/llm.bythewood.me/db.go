// The key ring and the call log hold the full text of every prompt and
// completion, so they get a database of their own that can be deleted alone, and
// secure_delete zeroes a deleted call instead of leaving it in the free pages.
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

	"github.com/anthropics/anthropic-sdk-go/v2"
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
	if _, err := db.Exec(schema + spendSchema); err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	// SQLite's advice for a long lived connection. A failure only costs the
	// planner its statistics, so it isn't fatal.
	_, _ = db.Exec("PRAGMA optimize=0x10002")
	return &Store{db: db}, nil
}

// addedColumns came after their tables first shipped. ALTER TABLE has no IF
// NOT EXISTS, so each is looked for before it's added.
var addedColumns = []struct{ table, column string }{
	{"keys", "cap_micros"},
	{"calls", "in_tok"}, {"calls", "cache_read"}, {"calls", "cache_write"}, {"calls", "searches"}, {"calls", "first_ms"},
	{"spend", "call_id"}, {"spend", "in_tok"}, {"spend", "cache_read"}, {"spend", "cache_write"}, {"spend", "out_tok"}, {"spend", "searches"},
}

func migrate(db *sql.DB) error {
	for _, c := range addedColumns {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, c.table, c.column).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE ` + c.table + ` ADD COLUMN ` + c.column + ` INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS spend_call ON spend(call_id) WHERE call_id > 0`)
	return err
}

// Close checkpoints the write ahead log first, so a container stopped with
// SIGKILL instead of SIGTERM loses at most the call in flight.
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
	Used    time.Time `json:"used"`
	Revoked bool      `json:"revoked"`
	// Millionths of a dollar a calendar month, and 0 for no cap.
	CapMicros int64 `json:"cap_micros"`
}

// keyPrefix is on every key this service issues, so one found in a config file
// is identifiable as belonging here and not to some other provider.
const keyPrefix = "orch-"

// NewKey mints a key and returns the plaintext once. Nothing keeps it, so a lost
// key is reissued, not recovered.
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

// AddKey registers a key minted by an earlier store, which is how a gateway
// restored without its volume learns the keys still sitting in dash's, pond's
// and cron's .env files. A key it already has, revoked or not, is left as it is.
func (s *Store) AddKey(name, secret string) (bool, error) {
	name, secret = strings.TrimSpace(name), strings.TrimSpace(secret)
	if name == "" {
		return false, errors.New("a key needs a name saying what will use it")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(secret, keyPrefix))
	if !strings.HasPrefix(secret, keyPrefix) || err != nil || len(raw) != 32 {
		return false, errors.New("that isn't a key this service issued")
	}
	r, err := s.db.Exec(`INSERT OR IGNORE INTO keys(name, hash, prefix, created_at) VALUES(?,?,?,?)`,
		name, hashKey(secret), secret[:len(keyPrefix)+6], time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n == 1, nil
}

func hashKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Authenticate resolves a presented key. A revoked key isn't a match, and the
// row fetched by hash is confirmed with a constant time compare instead of
// trusted.
func (s *Store) Authenticate(secret string) (Key, bool) {
	if !strings.HasPrefix(secret, keyPrefix) {
		return Key{}, false
	}
	sum := hashKey(secret)
	var k Key
	var created, used, revoked int64
	var stored string
	err := s.db.QueryRow(`SELECT id, name, prefix, hash, created_at, last_used, revoked_at, cap_micros FROM keys WHERE hash=?`, sum).
		Scan(&k.ID, &k.Name, &k.Prefix, &stored, &created, &used, &revoked, &k.CapMicros)
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
	rows, err := s.db.Query(`SELECT id, name, prefix, created_at, last_used, revoked_at, cap_micros FROM keys ORDER BY revoked_at, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Key{}
	for rows.Next() {
		var k Key
		var created, used, revoked int64
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &created, &used, &revoked, &k.CapMicros); err != nil {
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

// Revoke isn't a delete, so the call log keeps pointing at a real key and the
// history of what a retired service asked for stays readable.
func (s *Store) Revoke(id int64) error {
	_, err := s.db.Exec(`UPDATE keys SET revoked_at=? WHERE id=? AND revoked_at=0`, time.Now().Unix(), id)
	return err
}

func (s *Store) SetCap(id, micros int64) error {
	_, err := s.db.Exec(`UPDATE keys SET cap_micros=? WHERE id=?`, max(micros, 0), id)
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
	tokenSplit
	// Time to the first token, known only for a call that streamed.
	FirstMS int64 `json:"first_ms,omitempty"`
	// Millionths of a dollar. Estimated is a call older than the ledger's
	// link to it, priced from its token counts instead.
	Cost      int64 `json:"cost_micros"`
	Estimated bool  `json:"cost_estimated,omitempty"`
	started   time.Time
}

// addUsage takes one API response into the call. The prompt is the window the
// last response read, while the split and the cost add up every response.
func (c *Call) addUsage(u anthropic.Usage) {
	c.PromptTok = promptTokens(u)
	c.OutputTok += int(u.OutputTokens)
	c.InTok += u.InputTokens
	c.CacheRead += u.CacheReadInputTokens
	c.CacheWrite += u.CacheCreationInputTokens
	c.Searches += u.ServerToolUse.WebSearchRequests
	c.Cost += costMicros(u)
	logUsage(u)
}

// usage is the call as the API would have counted it. A call from before the
// split was kept has its whole prompt counted as uncached input.
func (c Call) usage() anthropic.Usage {
	u := anthropic.Usage{InputTokens: c.InTok, CacheReadInputTokens: c.CacheRead,
		CacheCreationInputTokens: c.CacheWrite, OutputTokens: int64(c.OutputTok)}
	if !c.recorded() {
		u.InputTokens = int64(c.PromptTok)
	}
	u.ServerToolUse.WebSearchRequests = c.Searches
	return u
}

func (c Call) Failed() bool { return c.Status >= 400 || c.Err != "" }

// LogCall writes the call and returns its id, so the spend it caused can point
// at it.
func (s *Store) LogCall(c Call) int64 {
	r, err := s.db.Exec(`INSERT INTO calls
		(key_id, caller, model, messages, completion, tools, prompt_tok, output_tok, decode_tps, ms, status, err, at,
		 in_tok, cache_read, cache_write, searches, first_ms)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.KeyID, c.Caller, c.Model, c.Messages, c.Completion, c.Tools,
		c.PromptTok, c.OutputTok, c.DecodeTPS, c.MS, c.Status, c.Err, time.Now().Unix(),
		c.InTok, c.CacheRead, c.CacheWrite, c.Searches, c.FirstMS)
	if err != nil {
		slog.Error("logging a call failed", "err", err)
		return 0
	}
	id, _ := r.LastInsertId()
	return id
}

// record writes a finished call down, unless it was incognito, and then its
// spend, linked to the row when there is one.
func (s *Store) record(c Call, keep bool) {
	var id int64
	if keep {
		c.MS = time.Since(c.started).Milliseconds()
		id = s.LogCall(c)
	}
	s.logSpend(spendRow{KeyID: c.KeyID, CallID: id, Micros: c.Cost, tokenSplit: c.tokenSplit, OutTok: int64(c.OutputTok)})
}

func (s *Store) Calls(caller string, limit int) ([]Call, error) {
	calls, _, err := s.FindCalls(callFilter{Caller: caller, Limit: limit})
	return calls, err
}

// callFilter is the calls page's query string. Every part of it runs in SQL
// before the limit, so an old failure is found however many calls came after.
type callFilter struct {
	ID     int64
	Caller string
	Failed bool
	// Day is midnight on the budget's clock, and Since the start of a range.
	Day   time.Time
	Since time.Time
	Q     string
	Limit int
}

func (f callFilter) where() (string, []any) {
	var conds []string
	var args []any
	if f.ID > 0 {
		conds, args = append(conds, "id=?"), append(args, f.ID)
	}
	if f.Caller != "" {
		conds, args = append(conds, "caller=?"), append(args, f.Caller)
	}
	if f.Failed {
		conds = append(conds, "(status >= 400 OR err <> '')")
	}
	if !f.Day.IsZero() {
		conds = append(conds, "at >= ? AND at < ?")
		args = append(args, f.Day.Unix(), f.Day.AddDate(0, 0, 1).Unix())
	}
	if !f.Since.IsZero() {
		conds, args = append(conds, "at >= ?"), append(args, f.Since.Unix())
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		conds = append(conds, `(messages LIKE ? ESCAPE '\' OR completion LIKE ? ESCAPE '\' OR err LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// FindCalls returns the newest calls that match and how many match in all.
func (s *Store) FindCalls(f callFilter) ([]Call, int, error) {
	where, args := f.where()
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM calls`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id, key_id, caller, model, messages, completion, tools,
	             prompt_tok, output_tok, decode_tps, ms, status, err, at,
	             in_tok, cache_read, cache_write, searches, first_ms,
	             (SELECT SUM(micros) FROM spend WHERE call_id = calls.id)
	      FROM calls`+where+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Call{}
	for rows.Next() {
		var c Call
		var at int64
		var spent sql.NullInt64
		if err := rows.Scan(&c.ID, &c.KeyID, &c.Caller, &c.Model, &c.Messages, &c.Completion,
			&c.Tools, &c.PromptTok, &c.OutputTok, &c.DecodeTPS, &c.MS, &c.Status, &c.Err, &at,
			&c.InTok, &c.CacheRead, &c.CacheWrite, &c.Searches, &c.FirstMS, &spent); err != nil {
			return nil, 0, err
		}
		c.At = time.Unix(at, 0)
		switch {
		case spent.Valid:
			c.Cost = spent.Int64
		case !c.recorded() && c.PromptTok+c.OutputTok > 0:
			c.Cost, c.Estimated = costMicros(c.usage()), true
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
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

// Prune drops calls older than the retention window, so the prompts don't pile
// up forever by default.
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
