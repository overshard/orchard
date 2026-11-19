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
CREATE TABLE IF NOT EXISTS jobs (
  -- AUTOINCREMENT so a deleted one-shot's id is never handed to a new job,
  -- which would file its runs under the wrong job.
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'notify',
  -- Empty cron means a one-shot at at_unix.
  cron       TEXT NOT NULL DEFAULT '',
  at_unix    INTEGER NOT NULL DEFAULT 0,
  title      TEXT NOT NULL DEFAULT '',
  message    TEXT NOT NULL DEFAULT '',
  priority   TEXT NOT NULL DEFAULT 'default',
  link       TEXT NOT NULL DEFAULT '',
  enabled    INTEGER NOT NULL DEFAULT 1,
  next_run   INTEGER NOT NULL DEFAULT 0,
  failures   INTEGER NOT NULL DEFAULT 0,
  last_run   INTEGER NOT NULL DEFAULT 0,
  last_err   TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS jobs_next ON jobs(enabled, next_run);
CREATE TABLE IF NOT EXISTS runs (
  id       INTEGER PRIMARY KEY,
  job_id   INTEGER NOT NULL,
  name     TEXT NOT NULL,
  kind     TEXT NOT NULL,
  at       INTEGER NOT NULL,
  ms       INTEGER NOT NULL DEFAULT 0,
  ok       INTEGER NOT NULL DEFAULT 0,
  pushed   INTEGER NOT NULL DEFAULT 0,
  title    TEXT NOT NULL DEFAULT '',
  body     TEXT NOT NULL DEFAULT '',
  err      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS runs_at ON runs(at DESC);
CREATE INDEX IF NOT EXISTS runs_job ON runs(job_id, at DESC);
CREATE TABLE IF NOT EXISTS keys (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  hash       TEXT NOT NULL UNIQUE,
  prefix     TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS snoozes (
  hash       TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,
  priority   TEXT NOT NULL,
  link       TEXT NOT NULL,
  expires_at INTEGER NOT NULL
);
`

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	// One writer, so the scheduler and a request saving a job never meet as
	// SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	_, _ = db.Exec("PRAGMA optimize=0x10002")
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		slog.Warn("wal checkpoint", "err", err)
	}
	return s.db.Close()
}

type Job struct {
	ID        int64
	Name      string
	Kind      string
	Cron      string
	At        time.Time
	Title     string
	Message   string
	Priority  string
	Link      string
	Enabled   bool
	NextRun   time.Time
	Failures  int
	LastRun   time.Time
	LastErr   string
	CreatedBy string
	Created   time.Time
	Updated   time.Time
}

func (j Job) Once() bool { return j.Cron == "" }

const jobCols = `id, name, kind, cron, at_unix, title, message, priority, link, enabled,
  next_run, failures, last_run, last_err, created_by, created_at, updated_at`

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnix(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

type scanner interface{ Scan(...any) error }

func scanJob(r scanner) (Job, error) {
	var j Job
	var at, next, last, created, updated int64
	var enabled int
	err := r.Scan(&j.ID, &j.Name, &j.Kind, &j.Cron, &at, &j.Title, &j.Message, &j.Priority, &j.Link,
		&enabled, &next, &j.Failures, &last, &j.LastErr, &j.CreatedBy, &created, &updated)
	j.At, j.NextRun, j.LastRun = fromUnix(at), fromUnix(next), fromUnix(last)
	j.Created, j.Updated, j.Enabled = fromUnix(created), fromUnix(updated), enabled == 1
	return j, err
}

func (s *Store) Job(id int64) (Job, error) {
	return scanJob(s.db.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE id=?`, id))
}

// Jobs lists enabled jobs soonest first, then paused ones.
func (s *Store) Jobs() ([]Job, error) {
	return s.jobs(`SELECT ` + jobCols + ` FROM jobs ORDER BY enabled DESC, next_run, id`)
}

func (s *Store) Due(now time.Time) ([]Job, error) {
	return s.jobs(`SELECT `+jobCols+` FROM jobs WHERE enabled=1 AND next_run>0 AND next_run<=? ORDER BY next_run`, now.Unix())
}

// Soonest is the next time anything is due, zero when nothing is.
func (s *Store) Soonest() time.Time {
	var n sql.NullInt64
	_ = s.db.QueryRow(`SELECT MIN(next_run) FROM jobs WHERE enabled=1 AND next_run>0`).Scan(&n)
	return fromUnix(n.Int64)
}

func (s *Store) jobs(q string, args ...any) ([]Job, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) Insert(j Job) (int64, error) {
	now := time.Now().Unix()
	r, err := s.db.Exec(`INSERT INTO jobs(name, kind, cron, at_unix, title, message, priority, link, enabled,
	  next_run, created_by, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.Name, j.Kind, j.Cron, unix(j.At), j.Title, j.Message, j.Priority, j.Link, b2i(j.Enabled),
		unix(j.NextRun), j.CreatedBy, now, now)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// Update saves what a person can edit and clears the failure count, since an
// edit is somebody looking at it.
func (s *Store) Update(j Job) error {
	_, err := s.db.Exec(`UPDATE jobs SET name=?, kind=?, cron=?, at_unix=?, title=?, message=?, priority=?,
	  link=?, enabled=?, next_run=?, failures=0, updated_at=? WHERE id=?`,
		j.Name, j.Kind, j.Cron, unix(j.At), j.Title, j.Message, j.Priority, j.Link, b2i(j.Enabled),
		unix(j.NextRun), time.Now().Unix(), j.ID)
	return err
}

// Reschedule is the scheduler's write after a fire, which leaves anything a
// person changed in the meantime alone.
func (s *Store) Reschedule(id int64, next time.Time, failures int, ran time.Time, errText string) error {
	_, err := s.db.Exec(`UPDATE jobs SET next_run=?, failures=?, last_run=?, last_err=? WHERE id=?`,
		unix(next), failures, unix(ran), errText, id)
	return err
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM jobs WHERE id=?`, id)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

type Run struct {
	ID     int64
	JobID  int64
	Name   string
	Kind   string
	At     time.Time
	MS     int64
	OK     bool
	Pushed bool
	Title  string
	Body   string
	Err    string
}

func (s *Store) AddRun(r Run) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO runs(job_id, name, kind, at, ms, ok, pushed, title, body, err)
	  VALUES(?,?,?,?,?,?,?,?,?,?)`,
		r.JobID, r.Name, r.Kind, r.At.Unix(), r.MS, b2i(r.OK), b2i(r.Pushed), r.Title, r.Body, r.Err)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Runs is the newest first, for one job when job is above zero.
func (s *Store) Runs(job int64, limit int) ([]Run, error) {
	q := `SELECT id, job_id, name, kind, at, ms, ok, pushed, title, body, err FROM runs`
	args := []any{}
	if job > 0 {
		q += ` WHERE job_id=?`
		args = append(args, job)
	}
	q += ` ORDER BY at DESC, id DESC LIMIT ?`
	rows, err := s.db.Query(q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		var at int64
		var ok, pushed int
		if err := rows.Scan(&r.ID, &r.JobID, &r.Name, &r.Kind, &at, &r.MS, &ok, &pushed, &r.Title, &r.Body, &r.Err); err != nil {
			return nil, err
		}
		r.At, r.OK, r.Pushed = time.Unix(at, 0), ok == 1, pushed == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Counts() (jobs, runs, pushed int) {
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE enabled=1`).Scan(&jobs)
	_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(pushed),0) FROM runs`).Scan(&runs, &pushed)
	return
}

// Prune keeps a year of runs and drops snooze buttons nobody pressed.
func (s *Store) Prune(now time.Time) {
	_, _ = s.db.Exec(`DELETE FROM runs WHERE at<?`, now.AddDate(-1, 0, 0).Unix())
	_, _ = s.db.Exec(`DELETE FROM snoozes WHERE expires_at<?`, now.Unix())
}

const keyPrefix = "cron-"

func newSecret(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// NewKey returns the plaintext once. Only the SHA-256 is kept, which is enough
// for 32 random bytes nobody chose.
func (s *Store) NewKey(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("a key needs a name saying what will use it")
	}
	secret, err := newSecret(keyPrefix)
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(`INSERT INTO keys(name, hash, prefix, created_at) VALUES(?,?,?,?)`,
		name, hashSecret(secret), secret[:len(keyPrefix)+6], time.Now().Unix())
	return secret, err
}

// Authenticate returns the key's name.
func (s *Store) Authenticate(secret string) (string, bool) {
	if !strings.HasPrefix(secret, keyPrefix) {
		return "", false
	}
	sum := hashSecret(secret)
	var id int64
	var name, stored string
	if err := s.db.QueryRow(`SELECT id, name, hash FROM keys WHERE hash=?`, sum).Scan(&id, &name, &stored); err != nil {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(sum)) != 1 {
		return "", false
	}
	_, _ = s.db.Exec(`UPDATE keys SET last_used=? WHERE id=?`, time.Now().Unix(), id)
	return name, true
}

type Key struct {
	ID      int64
	Name    string
	Prefix  string
	Created time.Time
	Used    time.Time
}

func (s *Store) Keys() ([]Key, error) {
	rows, err := s.db.Query(`SELECT id, name, prefix, created_at, last_used FROM keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Key{}
	for rows.Next() {
		var k Key
		var c, u int64
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &c, &u); err != nil {
			return nil, err
		}
		k.Created, k.Used = fromUnix(c), fromUnix(u)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteKey(id int64) error {
	_, err := s.db.Exec(`DELETE FROM keys WHERE id=?`, id)
	return err
}

// Snooze is what a pressed snooze button sends again.
type Snooze struct {
	Name, Title, Body, Priority, Link string
}

// AddSnooze returns the token that goes in the button's url. The url is the
// whole credential, so it is single use and expires in a week.
func (s *Store) AddSnooze(n Snooze, now time.Time) (string, error) {
	tok, err := newSecret("")
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(`INSERT INTO snoozes(hash, name, title, body, priority, link, expires_at) VALUES(?,?,?,?,?,?,?)`,
		hashSecret(tok), n.Name, n.Title, n.Body, n.Priority, n.Link, now.Add(7*24*time.Hour).Unix())
	return tok, err
}

// TakeSnooze spends a token.
func (s *Store) TakeSnooze(tok string, now time.Time) (Snooze, bool) {
	var n Snooze
	h := hashSecret(tok)
	err := s.db.QueryRow(`DELETE FROM snoozes WHERE hash=? AND expires_at>=? RETURNING name, title, body, priority, link`,
		h, now.Unix()).Scan(&n.Name, &n.Title, &n.Body, &n.Priority, &n.Link)
	return n, err == nil
}
