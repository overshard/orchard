package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS rules (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    name      TEXT    NOT NULL UNIQUE,
    note      TEXT    NOT NULL DEFAULT '',
    cond      TEXT    NOT NULL,
    action    TEXT    NOT NULL,
    status    INTEGER NOT NULL DEFAULT 403,
    message   TEXT    NOT NULL DEFAULT '',
    rate      INTEGER NOT NULL DEFAULT 0,
    ban_after INTEGER NOT NULL DEFAULT 0,
    ban_for   INTEGER NOT NULL DEFAULT 0,
    quiet     INTEGER NOT NULL DEFAULT 0,
    watching  INTEGER NOT NULL DEFAULT 0,
    pending   INTEGER NOT NULL DEFAULT 0,
    author    TEXT    NOT NULL,
    created   INTEGER NOT NULL,
    expires   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS hits (
    rule    INTEGER NOT NULL,
    hour    INTEGER NOT NULL,
    hits    INTEGER NOT NULL DEFAULT 0,
    watched INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (rule, hour)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS traffic (
    hour    INTEGER NOT NULL,
    host    TEXT    NOT NULL,
    asn     INTEGER NOT NULL,
    country TEXT    NOT NULL,
    verdict TEXT    NOT NULL,
    n       INTEGER NOT NULL,
    PRIMARY KEY (hour, host, asn, country, verdict)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS orgs (
    asn INTEGER PRIMARY KEY,
    org TEXT    NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    at      INTEGER NOT NULL,
    ip      TEXT    NOT NULL,
    asn     INTEGER NOT NULL,
    country TEXT    NOT NULL,
    host    TEXT    NOT NULL,
    method  TEXT    NOT NULL,
    path    TEXT    NOT NULL,
    ua      TEXT    NOT NULL,
    rule    TEXT    NOT NULL,
    verdict TEXT    NOT NULL,
    watch   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS events_at ON events(at);
CREATE INDEX IF NOT EXISTS events_rule ON events(rule, at);
CREATE INDEX IF NOT EXISTS events_ip ON events(ip, at);
CREATE TABLE IF NOT EXISTS bans (
    ip      TEXT    PRIMARY KEY,
    until   INTEGER NOT NULL,
    rule    TEXT    NOT NULL,
    created INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS changes (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    at   INTEGER NOT NULL,
    who  TEXT    NOT NULL,
    rule TEXT    NOT NULL,
    what TEXT    NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
    k TEXT PRIMARY KEY,
    v TEXT NOT NULL
);
`

// keepFor is how long counts and sampled requests stay.
const keepFor = 30 * 24 * time.Hour

type Store struct{ db *sql.DB }

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

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
	return time.Unix(n, 0).UTC()
}

const ruleCols = `id, name, note, cond, action, status, message, rate, ban_after, ban_for, quiet, watching, pending, author, created, expires`

func scanRule(sc interface{ Scan(...any) error }) (Rule, error) {
	var r Rule
	var banFor, created, expires int64
	var quiet, watching, pending int
	err := sc.Scan(&r.ID, &r.Name, &r.Note, &r.When, &r.Action, &r.Status, &r.Message, &r.Rate, &r.BanAfter, &banFor,
		&quiet, &watching, &pending, &r.Author, &created, &expires)
	r.BanFor = time.Duration(banFor) * time.Second
	r.Quiet, r.Watching, r.Pending = quiet == 1, watching == 1, pending == 1
	r.Created, r.Expires = fromUnix(created), fromUnix(expires)
	return r, err
}

func (s *Store) Rules() ([]Rule, error) {
	rows, err := s.db.Query(`SELECT ` + ruleCols + ` FROM rules ORDER BY pending DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var errNoRule = errors.New("no such rule")

func (s *Store) Rule(id int64) (Rule, error) {
	r, err := scanRule(s.db.QueryRow(`SELECT `+ruleCols+` FROM rules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, errNoRule
	}
	return r, err
}

func (s *Store) RuleNamed(name string) (Rule, error) {
	r, err := scanRule(s.db.QueryRow(`SELECT `+ruleCols+` FROM rules WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return r, errNoRule
	}
	return r, err
}

func (s *Store) AddRule(r *Rule, who string) error {
	if err := r.validate(); err != nil {
		return err
	}
	if r.Created.IsZero() {
		r.Created = time.Now().UTC()
	}
	res, err := s.db.Exec(`INSERT INTO rules (name, note, cond, action, status, message, rate, ban_after, ban_for, quiet, watching, pending, author, created, expires)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Name, r.Note, r.When, r.Action, r.Status, r.Message, r.Rate, r.BanAfter, int64(r.BanFor/time.Second),
		b2i(r.Quiet), b2i(r.Watching), b2i(r.Pending), r.Author, unix(r.Created), unix(r.Expires))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("a rule named %s already exists", r.Name)
		}
		return err
	}
	r.ID, _ = res.LastInsertId()
	s.Change(who, r.Name, "added, "+describe(*r))
	return nil
}

func (s *Store) UpdateRule(r *Rule, who string) error {
	if err := r.validate(); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE rules SET name = ?, note = ?, cond = ?, action = ?, status = ?, message = ?, rate = ?, ban_after = ?, ban_for = ?,
		quiet = ?, watching = ?, pending = ?, expires = ? WHERE id = ?`,
		r.Name, r.Note, r.When, r.Action, r.Status, r.Message, r.Rate, r.BanAfter, int64(r.BanFor/time.Second),
		b2i(r.Quiet), b2i(r.Watching), b2i(r.Pending), unix(r.Expires), r.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("a rule named %s already exists", r.Name)
		}
		return err
	}
	s.Change(who, r.Name, "changed, "+describe(*r))
	return nil
}

func (s *Store) DeleteRule(r Rule, who string) error {
	if _, err := s.db.Exec(`DELETE FROM rules WHERE id = ?`, r.ID); err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM hits WHERE rule = ?`, r.ID)
	s.Change(who, r.Name, "removed")
	return nil
}

func describe(r Rule) string {
	d := r.Then()
	if r.Watching {
		d = "watching as " + d
	}
	if !r.Expires.IsZero() {
		d += ", ends " + r.Expires.Format("2006-01-02 15:04 UTC")
	}
	return d
}

type Change struct {
	At              time.Time
	Who, Rule, What string
}

func (s *Store) Change(who, rule, what string) {
	_, _ = s.db.Exec(`INSERT INTO changes (at, who, rule, what) VALUES (?, ?, ?, ?)`, time.Now().Unix(), who, rule, what)
}

func (s *Store) Changes(limit int) ([]Change, int, error) {
	var total int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM changes`).Scan(&total)
	rows, err := s.db.Query(`SELECT at, who, rule, what FROM changes ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, total, err
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		var at int64
		if err := rows.Scan(&at, &c.Who, &c.Rule, &c.What); err != nil {
			return nil, total, err
		}
		c.At = fromUnix(at)
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// Flush writes what check counted since the last one in a single transaction.
func (s *Store) Flush(p pending) error {
	if len(p.traffic) == 0 && len(p.hits) == 0 && len(p.events) == 0 && len(p.bans) == 0 && len(p.orgs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, n := range p.traffic {
		if _, err := tx.Exec(`INSERT INTO traffic (hour, host, asn, country, verdict, n) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT DO UPDATE SET n = n + excluded.n`, k.Hour, k.Host, k.ASN, k.Country, k.Verdict, n); err != nil {
			return err
		}
	}
	for k, c := range p.hits {
		if _, err := tx.Exec(`INSERT INTO hits (rule, hour, hits, watched) VALUES (?, ?, ?, ?)
			ON CONFLICT DO UPDATE SET hits = hits + excluded.hits, watched = watched + excluded.watched`, k.Rule, k.Hour, c.Hits, c.Watched); err != nil {
			return err
		}
	}
	for asn, org := range p.orgs {
		if _, err := tx.Exec(`INSERT INTO orgs (asn, org) VALUES (?, ?) ON CONFLICT DO UPDATE SET org = excluded.org`, asn, org); err != nil {
			return err
		}
	}
	for _, e := range p.events {
		if _, err := tx.Exec(`INSERT INTO events (at, ip, asn, country, host, method, path, ua, rule, verdict, watch) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.At.UnixMilli(), e.IP, e.ASN, e.Country, e.Host, e.Method, e.Path, e.UA, e.Rule, e.Verdict, b2i(e.Watch)); err != nil {
			return err
		}
	}
	for _, b := range p.bans {
		if _, err := tx.Exec(`INSERT INTO bans (ip, until, rule, created) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET until = excluded.until, rule = excluded.rule, created = excluded.created`,
			b.IP.String(), b.Until.Unix(), b.Rule, b.Created.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Sweep(now time.Time) {
	cut := now.Add(-keepFor)
	_, _ = s.db.Exec(`DELETE FROM traffic WHERE hour < ?`, cut.Unix())
	_, _ = s.db.Exec(`DELETE FROM hits WHERE hour < ?`, cut.Unix())
	_, _ = s.db.Exec(`DELETE FROM events WHERE at < ?`, cut.UnixMilli())
	_, _ = s.db.Exec(`DELETE FROM bans WHERE until < ?`, now.Unix())
	_, _ = s.db.Exec(`DELETE FROM changes WHERE at < ?`, now.AddDate(-1, 0, 0).Unix())
}

func (s *Store) Bans() ([]Ban, error) {
	rows, err := s.db.Query(`SELECT ip, until, rule, created FROM bans WHERE until > ? ORDER BY created DESC`, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ban
	for rows.Next() {
		var ip, rule string
		var until, created int64
		if err := rows.Scan(&ip, &until, &rule, &created); err != nil {
			return nil, err
		}
		a, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		out = append(out, Ban{IP: a, Until: fromUnix(until), Rule: rule, Created: fromUnix(created)})
	}
	return out, rows.Err()
}

func (s *Store) AddBan(b Ban) error {
	_, err := s.db.Exec(`INSERT INTO bans (ip, until, rule, created) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET until = excluded.until, rule = excluded.rule`,
		b.IP.String(), b.Until.Unix(), b.Rule, b.Created.Unix())
	return err
}

func (s *Store) Lift(ip netip.Addr) error {
	_, err := s.db.Exec(`DELETE FROM bans WHERE ip = ?`, ip.String())
	return err
}

func (s *Store) Setting(k string) string {
	var v string
	_ = s.db.QueryRow(`SELECT v FROM settings WHERE k = ?`, k).Scan(&v)
	return v
}

func (s *Store) SetSetting(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO settings (k, v) VALUES (?, ?) ON CONFLICT DO UPDATE SET v = excluded.v`, k, v)
	return err
}

// Secret signs challenge seeds and pass cookies. It's made once and kept, so a
// deploy doesn't send everyone who solved one back through it.
func (s *Store) Secret() ([]byte, error) {
	if v := s.Setting("secret"); v != "" {
		return hex.DecodeString(v)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, s.SetSetting("secret", hex.EncodeToString(b))
}
