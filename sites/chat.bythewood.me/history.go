// Conversations on disk, and the mode that keeps them off it.
//
// This is a second database rather than more tables next to anything else, for
// the reason search's history is: deleting every conversation has to be
// possible without touching whatever else the site caches, and a separate file
// is one line to exclude from a backup. Deletion is forward only, so the honest
// way to keep something out of a backup is never to have put it in one.
package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS conversations (
  -- A uuid rather than a counter, so an address carries no ordering and says
  -- nothing about how many conversations there are.
  id         TEXT PRIMARY KEY,
  title      TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  -- The running summary that replaces the oldest turns once a conversation
  -- outgrows the window. Empty until the first compaction.
  summary    TEXT NOT NULL DEFAULT '',
  -- How many messages the summary already covers, so compaction knows where
  -- to resume rather than summarising the same turns again.
  summarized INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
  id      INTEGER PRIMARY KEY,
  conv_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role    TEXT NOT NULL,
  content TEXT NOT NULL,
  tools   TEXT NOT NULL DEFAULT '[]',
  -- What the user typed, when content also carries the text of their
  -- attachments. Empty when the two are the same.
  display TEXT NOT NULL DEFAULT '',
  files   TEXT NOT NULL DEFAULT '[]',
  -- The numbered pages an answer cites, so a reload links the same way the
  -- turn did. The page text they were matched against is not kept.
  sources TEXT NOT NULL DEFAULT '[]',
  -- The charts an answer drew, as their subjects rather than their readings, so
  -- reopening a conversation fetches today's numbers rather than replaying old
  -- ones.
  widgets TEXT NOT NULL DEFAULT '[]',
  at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS messages_conv ON messages(conv_id, id);

-- The rate limit penalty box, kept here so a deploy does not clear it. An
-- in-process map meant every restart asked a host that was already refusing,
-- which is the surest way to keep a ban alive.
CREATE TABLE IF NOT EXISTS penalties (
  host  TEXT PRIMARY KEY,
  till  INTEGER NOT NULL,
  trips INTEGER NOT NULL DEFAULT 1
);

-- One row per outbound call to a budgeted host, so a deploy does not hand the
-- model a fresh day's allowance. Nothing older than a day is kept.
CREATE TABLE IF NOT EXISTS spend (
  host TEXT    NOT NULL,
  at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS spend_host_at ON spend(host, at);
`

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// secure_delete zeroes a deleted row rather than leaving it in the free
	// pages, which is the difference between deleting a conversation and
	// deleting the text of one.
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=secure_delete(ON)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	migrate(db)
	migrateIDs(db)
	st := &Store{db: db}
	if err := st.initFacts(); err != nil {
		return nil, err
	}
	return st, nil
}

// migrate adds what a database written by an older build does not have. SQLite
// has no ADD COLUMN IF NOT EXISTS, so each one is attempted and a duplicate is
// the expected answer on every run after the first.
func migrate(db *sql.DB) {
	for _, stmt := range []string{
		`ALTER TABLE messages ADD COLUMN display TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN files TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE messages ADD COLUMN sources TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE messages ADD COLUMN widgets TEXT NOT NULL DEFAULT '[]'`,
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			slog.Warn("migrate", "stmt", stmt, "err", err)
		}
	}
}

// migrateIDs rebuilds both tables when the conversation id is still a counter.
// SQLite cannot change a column's type, and the ids have to be handed out
// before anything can point at them, so it is a copy rather than an update. The
// old addresses stop resolving, which was accepted when the change was asked
// for.
func migrateIDs(db *sql.DB) {
	rows, err := db.Query(`PRAGMA table_info(conversations)`)
	if err != nil {
		return
	}
	kind := ""
	for rows.Next() {
		var cid int
		var name, typ, dflt any
		var notnull, pk int
		if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil && name == "id" {
			kind, _ = typ.(string)
		}
	}
	rows.Close()
	if !strings.EqualFold(kind, "INTEGER") {
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("could not start the id migration", "err", err)
		return
	}
	defer tx.Rollback()

	fail := func(step string, err error) bool {
		if err == nil {
			return false
		}
		slog.Error("the id migration stopped", "step", step, "err", err)
		return true
	}

	var old []int64
	ids, err := tx.Query(`SELECT id FROM conversations ORDER BY id`)
	if fail("reading the conversations", err) {
		return
	}
	for ids.Next() {
		var id int64
		if ids.Scan(&id) == nil {
			old = append(old, id)
		}
	}
	ids.Close()

	for _, stmt := range []string{
		`CREATE TABLE conversations_new (
		  id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '',
		  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
		  summary TEXT NOT NULL DEFAULT '', summarized INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE messages_new (
		  id INTEGER PRIMARY KEY,
		  conv_id TEXT NOT NULL REFERENCES conversations_new(id) ON DELETE CASCADE,
		  role TEXT NOT NULL, content TEXT NOT NULL,
		  tools TEXT NOT NULL DEFAULT '[]', display TEXT NOT NULL DEFAULT '',
		  files TEXT NOT NULL DEFAULT '[]', sources TEXT NOT NULL DEFAULT '[]',
		  widgets TEXT NOT NULL DEFAULT '[]',
		  at INTEGER NOT NULL)`,
	} {
		if fail("creating the new tables", run(tx, stmt)) {
			return
		}
	}

	for _, o := range old {
		id := newID()
		if fail("copying a conversation", run(tx,
			`INSERT INTO conversations_new SELECT ?, title, created_at, updated_at, summary, summarized FROM conversations WHERE id=?`,
			id, o)) {
			return
		}
		if fail("copying its messages", run(tx,
			`INSERT INTO messages_new(conv_id, role, content, tools, display, files, sources, widgets, at)
			 SELECT ?, role, content, tools, display, files, sources, widgets, at FROM messages WHERE conv_id=? ORDER BY id`,
			id, o)) {
			return
		}
	}

	// The child goes first, so dropping the parent never fires a cascade over
	// rows that are still the only copy.
	for _, stmt := range []string{
		`DROP TABLE messages`,
		`DROP TABLE conversations`,
		`ALTER TABLE conversations_new RENAME TO conversations`,
		`ALTER TABLE messages_new RENAME TO messages`,
		`CREATE INDEX IF NOT EXISTS messages_conv ON messages(conv_id, id)`,
	} {
		if fail("swapping the tables", run(tx, stmt)) {
			return
		}
	}
	if fail("committing", tx.Commit()) {
		return
	}
	slog.Info("conversation ids are uuids now", "conversations", len(old))
}

func run(tx *sql.Tx, stmt string, args ...any) error {
	_, err := tx.Exec(stmt, args...)
	return err
}

// Close checkpoints the write ahead log into the database file before closing.
// Without this the newest conversations live only in the -wal, and a container
// stopped with SIGKILL rather than SIGTERM can lose them.
func (s *Store) Close() error {
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		slog.Warn("wal checkpoint", "err", err)
	}
	return s.db.Close()
}

// Checkpoint folds the log into the file without closing. It runs after every
// turn, so the cost of a hard kill is at most the turn in flight rather than
// everything since the process started.
func (s *Store) Checkpoint() {
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		slog.Debug("wal checkpoint", "err", err)
	}
}

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Updated   time.Time `json:"updated"`
	Preview   string    `json:"preview,omitempty"`
	Summary   string    `json:"-"`
	Summarize int       `json:"-"`
}

type Stored struct {
	Role    Role          `json:"role"`
	Content string        `json:"content"`
	Display string        `json:"display,omitempty"`
	Files   []Attachment  `json:"files,omitempty"`
	Tools   []ToolSummary `json:"tools,omitempty"`
	Sources []Source      `json:"sources,omitempty"`
	Widgets []Widget      `json:"widgets,omitempty"`
	At      time.Time     `json:"at"`
}

// Shown is what the user typed, which is the whole message unless files were
// attached and their text was folded into it.
func (m Stored) Shown() string {
	if m.Display != "" {
		return m.Display
	}
	return m.Content
}

// ToolSummary is what the UI shows under a message: which tools ran and how
// they went, not their whole payload.
type ToolSummary struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
	MS   int64  `json:"ms"`
	OK   bool   `json:"ok"`
	Err  string `json:"err,omitempty"`
	// How old the data behind this call is, for a tool that reads a snapshot
	// rather than the live thing. Shown on the chip, because an answer off a
	// local corpus looks exactly like one off the web otherwise.
	Age string `json:"age,omitempty"`
}

func (s *Store) NewConversation(title string) (string, error) {
	now := time.Now().Unix()
	id := newID()
	if _, err := s.db.Exec(`INSERT INTO conversations(id, title, created_at, updated_at) VALUES(?,?,?,?)`,
		id, title, now, now); err != nil {
		return "", err
	}
	return id, nil
}

// newID is a version 4 uuid. Nothing in the repo needed one before this, and a
// dependency for sixteen random bytes and a format string is not a trade worth
// making.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// The only way this fails is a broken kernel, and carrying on with a
		// predictable id would be worse than saying so.
		panic("no randomness for a conversation id: " + err.Error())
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (s *Store) Append(convID string, m Stored) error {
	b, _ := json.Marshal(m.Tools)
	f, _ := json.Marshal(m.Files)
	src, _ := json.Marshal(m.Sources)
	wid, _ := json.Marshal(m.Widgets)
	if _, err := s.db.Exec(`INSERT INTO messages(conv_id, role, content, tools, display, files, sources, widgets, at) VALUES(?,?,?,?,?,?,?,?,?)`,
		convID, string(m.Role), m.Content, string(b), m.Display, string(f), string(src), string(wid), time.Now().Unix()); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE conversations SET updated_at=? WHERE id=?`, time.Now().Unix(), convID)
	return err
}

func (s *Store) Messages(convID string) ([]Stored, error) {
	rows, err := s.db.Query(`SELECT role, content, tools, display, files, sources, widgets, at FROM messages WHERE conv_id=? ORDER BY id`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Stored
	for rows.Next() {
		var m Stored
		var role, tools, files, sources, widgets string
		var at int64
		if err := rows.Scan(&role, &m.Content, &tools, &m.Display, &files, &sources, &widgets, &at); err != nil {
			return nil, err
		}
		m.Role, m.At = Role(role), time.Unix(at, 0)
		_ = json.Unmarshal([]byte(tools), &m.Tools)
		_ = json.Unmarshal([]byte(files), &m.Files)
		_ = json.Unmarshal([]byte(sources), &m.Sources)
		_ = json.Unmarshal([]byte(widgets), &m.Widgets)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) List(limit int) ([]Conversation, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.title, c.updated_at,
		       COALESCE((SELECT COALESCE(NULLIF(display, ''), content) FROM messages WHERE conv_id=c.id AND role='user' ORDER BY id LIMIT 1), '')
		FROM conversations c
		WHERE EXISTS (SELECT 1 FROM messages WHERE conv_id=c.id)
		ORDER BY c.updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		var up int64
		var first string
		if err := rows.Scan(&c.ID, &c.Title, &up, &first); err != nil {
			return nil, err
		}
		c.Updated = time.Unix(up, 0)
		if c.Title == "" {
			c.Title = titleFrom(first)
		}
		c.Preview = trim(first, 90)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Get(convID string) (Conversation, error) {
	var c Conversation
	var up int64
	err := s.db.QueryRow(`SELECT id, title, updated_at, summary, summarized FROM conversations WHERE id=?`,
		convID).Scan(&c.ID, &c.Title, &up, &c.Summary, &c.Summarize)
	c.Updated = time.Unix(up, 0)
	return c, err
}

func (s *Store) SetSummary(convID string, summary string, covered int) error {
	_, err := s.db.Exec(`UPDATE conversations SET summary=?, summarized=? WHERE id=?`, summary, covered, convID)
	return err
}

func (s *Store) SetTitle(convID string, title string) error {
	_, err := s.db.Exec(`UPDATE conversations SET title=? WHERE id=? AND title=''`, title, convID)
	return err
}

func (s *Store) Delete(convID string) error {
	_, err := s.db.Exec(`DELETE FROM conversations WHERE id=?`, convID)
	return err
}

// DeleteAll drops every conversation. VACUUM afterwards so the pages actually
// come back rather than sitting in the file as free space.
func (s *Store) DeleteAll() error {
	if _, err := s.db.Exec(`DELETE FROM conversations`); err != nil {
		return err
	}
	_, err := s.db.Exec(`VACUUM`)
	return err
}

func (s *Store) Count() (convs, msgs int) {
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM conversations`).Scan(&convs)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&msgs)
	return
}

func titleFrom(first string) string {
	t := trim(strings.TrimSpace(first), 48)
	if t == "" {
		return "New chat"
	}
	return t
}

func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n-1]) + "…"
}

func fmtWhen(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return t.Format("2 Jan")
	}
}

// Penalties reads back the rate limit boxes that outlived the last process.
// Anything already expired is left behind rather than deleted here, since the
// next Trip overwrites it and a read should not write.
func (s *Store) Penalties() map[string][2]int64 {
	out := map[string][2]int64{}
	rows, err := s.db.Query(`SELECT host, till, trips FROM penalties WHERE till > ?`,
		time.Now().UnixMilli())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var host string
		var till, trips int64
		if err := rows.Scan(&host, &till, &trips); err != nil {
			return out
		}
		out[host] = [2]int64{till, trips}
	}
	return out
}

// SavePenalty records one host's box so it survives a restart.
func (s *Store) SavePenalty(host string, till time.Time, trips int) {
	_, _ = s.db.Exec(`INSERT INTO penalties (host, till, trips) VALUES (?,?,?)
		ON CONFLICT(host) DO UPDATE SET till = excluded.till, trips = excluded.trips`,
		host, till.UnixMilli(), trips)
}

// ClearPenalty forgets a host that answered, so one bad afternoon does not
// leave it on a long backoff for good.
func (s *Store) ClearPenalty(host string) {
	_, _ = s.db.Exec(`DELETE FROM penalties WHERE host = ?`, host)
}

// Spend reads back a host's calls from the last day.
func (s *Store) Spend(host string) []time.Time {
	rows, err := s.db.Query(`SELECT at FROM spend WHERE host = ? AND at > ? ORDER BY at`,
		host, time.Now().Add(-24*time.Hour).UnixMilli())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var at int64
		if err := rows.Scan(&at); err != nil {
			return out
		}
		out = append(out, time.UnixMilli(at))
	}
	return out
}

// SaveSpend replaces a host's record with what the budget currently holds, and
// drops what has aged out in the same statement. Called after a turn rather
// than per request, since losing the last turn's counts to a hard kill costs
// less than a write on the request path.
func (s *Store) SaveSpend(host string, at []time.Time) {
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM spend WHERE host = ?`, host); err != nil {
		return
	}
	for _, t := range at {
		if _, err := tx.Exec(`INSERT INTO spend (host, at) VALUES (?, ?)`, host, t.UnixMilli()); err != nil {
			return
		}
	}
	_ = tx.Commit()
}
