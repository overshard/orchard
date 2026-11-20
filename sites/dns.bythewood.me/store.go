package main

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// keepDays is how long a raw lookup is kept. hours, the per device count, is
// kept for keepHoursDays so a device's fortnight still draws once its rows age out.
const (
	keepDays      = 7
	keepHoursDays = 90
)

const schema = `
CREATE TABLE IF NOT EXISTS devices (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT 'phone',
	token TEXT NOT NULL UNIQUE,
	off_lists TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL,
	last_seen INTEGER NOT NULL DEFAULT 0,
	last_proto TEXT NOT NULL DEFAULT '',
	last_ip TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS rules (
	id INTEGER PRIMARY KEY,
	domain TEXT NOT NULL UNIQUE,
	action TEXT NOT NULL,
	note TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS queries (
	id INTEGER PRIMARY KEY,
	at INTEGER NOT NULL,
	device INTEGER NOT NULL,
	name TEXT NOT NULL,
	qtype INTEGER NOT NULL,
	status TEXT NOT NULL,
	reason TEXT NOT NULL DEFAULT '',
	via TEXT NOT NULL DEFAULT '',
	rcode INTEGER NOT NULL DEFAULT 0,
	ms INTEGER NOT NULL DEFAULT 0,
	proto TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	answer TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS queries_at ON queries(at);
CREATE INDEX IF NOT EXISTS queries_device_at ON queries(device, at);
CREATE INDEX IF NOT EXISTS queries_status_at ON queries(status, at);
CREATE TABLE IF NOT EXISTS hours (
	hour INTEGER NOT NULL,
	device INTEGER NOT NULL,
	total INTEGER NOT NULL DEFAULT 0,
	blocked INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (hour, device)
);
CREATE TABLE IF NOT EXISTS lists (
	key TEXT PRIMARY KEY,
	entries INTEGER NOT NULL DEFAULT 0,
	fetched INTEGER NOT NULL DEFAULT 0,
	changed INTEGER NOT NULL DEFAULT 0,
	version TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	error_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS settings (
	k TEXT PRIMARY KEY,
	v TEXT NOT NULL
);
`

type Store struct {
	db *sql.DB
}

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

func (s *Store) Setting(k string) string {
	var v string
	_ = s.db.QueryRow(`SELECT v FROM settings WHERE k = ?`, k).Scan(&v)
	return v
}

func (s *Store) SetSetting(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO settings (k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v`, k, v)
	return err
}

// Device is one thing that asks, a phone or a browser, known by its token.
type Device struct {
	ID        int64
	Name      string
	Kind      string
	Token     string
	OffLists  []string
	Created   time.Time
	LastSeen  time.Time
	LastProto string
	LastIP    string
}

// Off says whether this device skips a list.
func (d *Device) Off(key string) bool {
	for _, k := range d.OffLists {
		if k == key {
			return true
		}
	}
	return false
}

// tokenAlphabet is lowercase base32, since a token is a DNS label and Android
// lowercases the hostname typed into Private DNS.
const tokenAlphabet = "abcdefghijklmnopqrstuvwxyz" + "234567"

const tokenLen = 16

func newToken() string {
	b := make([]byte, tokenLen)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = tokenAlphabet[int(b[i])%len(tokenAlphabet)]
	}
	return string(b)
}

func validToken(t string) bool {
	if len(t) != tokenLen {
		return false
	}
	for i := 0; i < len(t); i++ {
		if !strings.ContainsRune(tokenAlphabet, rune(t[i])) {
			return false
		}
	}
	return true
}

const deviceCols = `id, name, kind, token, off_lists, created, last_seen, last_proto, last_ip`

func scanDevice(sc interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	var off string
	var created, seen int64
	if err := sc.Scan(&d.ID, &d.Name, &d.Kind, &d.Token, &off, &created, &seen, &d.LastProto, &d.LastIP); err != nil {
		return nil, err
	}
	if off != "" {
		d.OffLists = strings.Split(off, ",")
	}
	d.Created = time.Unix(created, 0)
	if seen > 0 {
		d.LastSeen = time.Unix(seen, 0)
	}
	return &d, nil
}

func (s *Store) Devices() ([]*Device, error) {
	rows, err := s.db.Query(`SELECT ` + deviceCols + ` FROM devices ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Device(id int64) (*Device, error) {
	return scanDevice(s.db.QueryRow(`SELECT `+deviceCols+` FROM devices WHERE id = ?`, id))
}

func (s *Store) AddDevice(name, kind string) (*Device, error) {
	res, err := s.db.Exec(`INSERT INTO devices (name, kind, token, created) VALUES (?, ?, ?, ?)`, name, kind, newToken(), time.Now().Unix())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Device(id)
}

func (s *Store) UpdateDevice(id int64, name, kind string, off []string) error {
	_, err := s.db.Exec(`UPDATE devices SET name = ?, kind = ?, off_lists = ? WHERE id = ?`, name, kind, strings.Join(off, ","), id)
	return err
}

func (s *Store) RotateToken(id int64) error {
	_, err := s.db.Exec(`UPDATE devices SET token = ? WHERE id = ?`, newToken(), id)
	return err
}

// DeleteDevice keeps the device's lookups until they age out, since they're
// still what happened, and only its token stops working.
func (s *Store) DeleteDevice(id int64) error {
	_, err := s.db.Exec(`DELETE FROM devices WHERE id = ?`, id)
	return err
}

func (s *Store) Seen(id int64, at time.Time, proto, ip string) error {
	_, err := s.db.Exec(`UPDATE devices SET last_seen = ?, last_proto = ?, last_ip = ? WHERE id = ?`, at.Unix(), proto, ip, id)
	return err
}

// Rule is one domain I allowed or blocked by hand. It covers every subdomain.
type Rule struct {
	ID      int64
	Domain  string
	Action  string
	Note    string
	Created time.Time
}

func (s *Store) Rules() ([]Rule, error) {
	rows, err := s.db.Query(`SELECT id, domain, action, note, created FROM rules ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var c int64
		if err := rows.Scan(&r.ID, &r.Domain, &r.Action, &r.Note, &c); err != nil {
			return nil, err
		}
		r.Created = time.Unix(c, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

var errBadDomain = errors.New("that isn't a domain")

// PutRule allows or blocks a domain, replacing whatever rule it had.
func (s *Store) PutRule(domain, action, note string) error {
	domain = cleanName(domain)
	if !looksLikeDomain(domain) {
		return errBadDomain
	}
	if action != "allow" && action != "block" {
		return fmt.Errorf("unknown action %q", action)
	}
	_, err := s.db.Exec(`INSERT INTO rules (domain, action, note, created) VALUES (?, ?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET action = excluded.action, note = excluded.note`, domain, action, note, time.Now().Unix())
	return err
}

func (s *Store) DeleteRule(id int64) (Rule, error) {
	var r Rule
	var c int64
	if err := s.db.QueryRow(`SELECT id, domain, action, note, created FROM rules WHERE id = ?`, id).Scan(&r.ID, &r.Domain, &r.Action, &r.Note, &c); err != nil {
		return r, err
	}
	_, err := s.db.Exec(`DELETE FROM rules WHERE id = ?`, id)
	return r, err
}

func cleanName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "*.")
	if u, ok := strings.CutPrefix(s, "https://"); ok {
		s = u
	} else if u, ok := strings.CutPrefix(s, "http://"); ok {
		s = u
	}
	if i := strings.IndexAny(s, "/:?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSuffix(s, ".")
}

func looksLikeDomain(s string) bool {
	if s == "" || len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if l == "" || len(l) > 63 {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}

// ListState is what the last refresh of a blocklist left behind.
type ListState struct {
	Key     string
	Entries int
	Fetched time.Time
	Changed time.Time
	Version string
	Error   string
	ErrorAt time.Time
}

func (s *Store) ListStates() (map[string]ListState, error) {
	rows, err := s.db.Query(`SELECT key, entries, fetched, changed, version, error, error_at FROM lists`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ListState{}
	for rows.Next() {
		var l ListState
		var f, c, e int64
		if err := rows.Scan(&l.Key, &l.Entries, &f, &c, &l.Version, &l.Error, &e); err != nil {
			return nil, err
		}
		l.Fetched, l.Changed, l.ErrorAt = unixOr(f), unixOr(c), unixOr(e)
		out[l.Key] = l
	}
	return out, rows.Err()
}

func unixOr(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

func (s *Store) ListFetched(key string, entries int, version string, changed bool, at time.Time) error {
	ch := `changed`
	if changed {
		ch = `excluded.changed`
	}
	_, err := s.db.Exec(`INSERT INTO lists (key, entries, fetched, changed, version, error, error_at) VALUES (?, ?, ?, ?, ?, '', 0)
		ON CONFLICT(key) DO UPDATE SET entries = excluded.entries, fetched = excluded.fetched, changed = `+ch+`,
		version = excluded.version, error = '', error_at = 0`, key, entries, at.Unix(), at.Unix(), version)
	return err
}

func (s *Store) ListFailed(key, msg string, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO lists (key, error, error_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET error = excluded.error, error_at = excluded.error_at`, key, msg, at.Unix())
	return err
}

// Lookup is one question a device asked and what it was told.
type Lookup struct {
	ID     int64
	At     time.Time
	Device int64
	Name   string
	Qtype  uint16
	// Status is pass, block (a list), deny (my rule), allow (my rule over a
	// list), fail (no answer upstream) or local (answered here).
	Status string
	Reason string
	// Via is the CNAME a block was found on, when it wasn't the name asked.
	Via    string
	Rcode  int
	Ms     int
	Proto  string
	IP     string
	Answer string
}

func (l Lookup) Blocked() bool { return l.Status == "block" || l.Status == "deny" }

// Insert writes a batch of lookups and their hourly counts in one transaction,
// so the counts never disagree with the rows.
func (s *Store) Insert(batch []Lookup) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ins, err := tx.Prepare(`INSERT INTO queries (at, device, name, qtype, status, reason, via, rcode, ms, proto, ip, answer) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	type hk struct{ hour, dev int64 }
	counts := map[hk][2]int{}
	for _, l := range batch {
		if _, err := ins.Exec(l.At.UnixMilli(), l.Device, l.Name, l.Qtype, l.Status, l.Reason, l.Via, l.Rcode, l.Ms, l.Proto, l.IP, l.Answer); err != nil {
			return err
		}
		k := hk{l.At.Unix() / 3600 * 3600, l.Device}
		c := counts[k]
		c[0]++
		if l.Blocked() {
			c[1]++
		}
		counts[k] = c
	}
	for k, c := range counts {
		if _, err := tx.Exec(`INSERT INTO hours (hour, device, total, blocked) VALUES (?, ?, ?, ?)
			ON CONFLICT(hour, device) DO UPDATE SET total = total + excluded.total, blocked = blocked + excluded.blocked`, k.hour, k.dev, c[0], c[1]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Sweep drops what's past its keep, a chunk at a time so a long backlog never
// holds the write lock for long.
func (s *Store) Sweep(now time.Time) {
	cut := now.Add(-keepDays * 24 * time.Hour).UnixMilli()
	for {
		res, err := s.db.Exec(`DELETE FROM queries WHERE id IN (SELECT id FROM queries WHERE at < ? LIMIT 5000)`, cut)
		if err != nil {
			return
		}
		if n, _ := res.RowsAffected(); n < 5000 {
			break
		}
	}
	_, _ = s.db.Exec(`DELETE FROM hours WHERE hour < ?`, now.Add(-keepHoursDays*24*time.Hour).Unix())
}

const lookupCols = `id, at, device, name, qtype, status, reason, via, rcode, ms, proto, ip, answer`

func scanLookups(rows *sql.Rows) ([]Lookup, error) {
	defer rows.Close()
	var out []Lookup
	for rows.Next() {
		var l Lookup
		var at int64
		if err := rows.Scan(&l.ID, &at, &l.Device, &l.Name, &l.Qtype, &l.Status, &l.Reason, &l.Via, &l.Rcode, &l.Ms, &l.Proto, &l.IP, &l.Answer); err != nil {
			return nil, err
		}
		l.At = time.UnixMilli(at)
		out = append(out, l)
	}
	return out, rows.Err()
}

// Filter narrows the log. Zero values mean everything.
type Filter struct {
	Device int64
	// Status is a Lookup status, or "blocked" for block and deny together.
	Status string
	Reason string
	Search string
	Since  time.Time
	After  int64
	Before int64
	Limit  int
}

func (f Filter) where() (string, []any) {
	var w []string
	var args []any
	if f.Device != 0 {
		w, args = append(w, "device = ?"), append(args, f.Device)
	}
	switch f.Status {
	case "":
	case "blocked":
		w = append(w, "status IN ('block', 'deny')")
	default:
		w, args = append(w, "status = ?"), append(args, f.Status)
	}
	if f.Reason != "" {
		w, args = append(w, "reason = ?"), append(args, f.Reason)
	}
	if f.Search != "" {
		w, args = append(w, "(name LIKE ? ESCAPE '\\' OR via LIKE ? ESCAPE '\\')"), append(args, likeArg(f.Search), likeArg(f.Search))
	}
	if !f.Since.IsZero() {
		w, args = append(w, "at >= ?"), append(args, f.Since.UnixMilli())
	}
	if f.After != 0 {
		w, args = append(w, "id > ?"), append(args, f.After)
	}
	if f.Before != 0 {
		w, args = append(w, "id < ?"), append(args, f.Before)
	}
	if len(w) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(w, " AND "), args
}

func likeArg(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.ToLower(s)) + "%"
}

func (s *Store) Log(f Filter) ([]Lookup, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 200
	}
	w, args := f.where()
	rows, err := s.db.Query(`SELECT `+lookupCols+` FROM queries`+w+` ORDER BY id DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	return scanLookups(rows)
}

// Count is the matches of a filter and how many were blocked, for the log's head.
func (s *Store) Count(f Filter) (total, blocked int) {
	w, args := f.where()
	_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status IN ('block', 'deny')), 0) FROM queries`+w, args...).Scan(&total, &blocked)
	return
}

// Ranked is one row of a top list.
type Ranked struct {
	Key     string
	N       int
	Devices int
	Last    time.Time
	Reason  string
}

// Top ranks names over a filter, busiest first.
func (s *Store) Top(f Filter, by string, n int) []Ranked {
	col := "name"
	switch by {
	case "reason":
		col = "reason"
	case "device":
		col = "device"
	case "base":
		col = "name"
	}
	w, args := f.where()
	rows, err := s.db.Query(`SELECT `+col+`, COUNT(*), COUNT(DISTINCT device), MAX(at), MAX(reason) FROM queries`+w+` GROUP BY `+col+` ORDER BY 2 DESC LIMIT ?`, append(args, n)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Ranked
	for rows.Next() {
		var r Ranked
		var at int64
		if rows.Scan(&r.Key, &r.N, &r.Devices, &at, &r.Reason) == nil {
			r.Last = time.UnixMilli(at)
			out = append(out, r)
		}
	}
	return out
}

// Distinct counts distinct names over a filter.
func (s *Store) Distinct(f Filter) int {
	w, args := f.where()
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT name) FROM queries`+w, args...).Scan(&n)
	return n
}

// Hourly is the last n hours of totals and blocks, oldest first, for one
// device or every device when dev is 0.
func (s *Store) Hourly(dev int64, n int, now time.Time) (total, blocked []float64) {
	end := now.Unix() / 3600 * 3600
	start := end - int64(n-1)*3600
	total, blocked = make([]float64, n), make([]float64, n)
	q := `SELECT hour, SUM(total), SUM(blocked) FROM hours WHERE hour >= ?`
	args := []any{start}
	if dev != 0 {
		q += ` AND device = ?`
		args = append(args, dev)
	}
	rows, err := s.db.Query(q+` GROUP BY hour`, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var h int64
		var t, b float64
		if rows.Scan(&h, &t, &b) == nil {
			if i := (h - start) / 3600; i >= 0 && i < int64(n) {
				total[i], blocked[i] = t, b
			}
		}
	}
	return
}

// Daily is the last n days of totals and blocks by UTC day, oldest first.
func (s *Store) Daily(dev int64, n int, now time.Time) (total, blocked []float64) {
	end := now.Unix() / 86400 * 86400
	start := end - int64(n-1)*86400
	total, blocked = make([]float64, n), make([]float64, n)
	q := `SELECT hour / 86400 * 86400, SUM(total), SUM(blocked) FROM hours WHERE hour >= ?`
	args := []any{start}
	if dev != 0 {
		q += ` AND device = ?`
		args = append(args, dev)
	}
	rows, err := s.db.Query(q+` GROUP BY 1`, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var d int64
		var t, b float64
		if rows.Scan(&d, &t, &b) == nil {
			if i := (d - start) / 86400; i >= 0 && i < int64(n) {
				total[i], blocked[i] = t, b
			}
		}
	}
	return
}

// Latency is the median and 95th percentile answer time over a filter, in ms.
func (s *Store) Latency(f Filter) (p50, p95 int) {
	w, args := f.where()
	_ = s.db.QueryRow(`SELECT COALESCE(MIN(CASE WHEN cd >= 0.5 THEN ms END), 0), COALESCE(MIN(CASE WHEN cd >= 0.95 THEN ms END), 0)
		FROM (SELECT ms, CUME_DIST() OVER (ORDER BY ms) AS cd FROM queries`+w+`)`, args...).Scan(&p50, &p95)
	return
}
