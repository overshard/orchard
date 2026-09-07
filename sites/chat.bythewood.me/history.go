// Conversations on disk, and the mode that keeps them off it.
//
// This is a second database rather than more tables next to anything else, for
// the reason search's history is: deleting every conversation has to be
// possible without touching whatever else the site caches, and a separate file
// is one line to exclude from a backup. Deletion is forward only, so the honest
// way to keep something out of a backup is never to have put it in one.
package main

import (
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
  id         INTEGER PRIMARY KEY,
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
  conv_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role    TEXT NOT NULL,
  content TEXT NOT NULL,
  tools   TEXT NOT NULL DEFAULT '[]',
  -- What the user typed, when content also carries the text of their
  -- attachments. Empty when the two are the same.
  display TEXT NOT NULL DEFAULT '',
  files   TEXT NOT NULL DEFAULT '[]',
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
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			slog.Warn("migrate", "stmt", stmt, "err", err)
		}
	}
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
	ID        int64     `json:"id"`
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
}

func (s *Store) NewConversation(title string) (int64, error) {
	now := time.Now().Unix()
	r, err := s.db.Exec(`INSERT INTO conversations(title, created_at, updated_at) VALUES(?,?,?)`,
		title, now, now)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) Append(convID int64, m Stored) error {
	b, _ := json.Marshal(m.Tools)
	f, _ := json.Marshal(m.Files)
	if _, err := s.db.Exec(`INSERT INTO messages(conv_id, role, content, tools, display, files, at) VALUES(?,?,?,?,?,?,?)`,
		convID, string(m.Role), m.Content, string(b), m.Display, string(f), time.Now().Unix()); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE conversations SET updated_at=? WHERE id=?`, time.Now().Unix(), convID)
	return err
}

func (s *Store) Messages(convID int64) ([]Stored, error) {
	rows, err := s.db.Query(`SELECT role, content, tools, display, files, at FROM messages WHERE conv_id=? ORDER BY id`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Stored
	for rows.Next() {
		var m Stored
		var role, tools, files string
		var at int64
		if err := rows.Scan(&role, &m.Content, &tools, &m.Display, &files, &at); err != nil {
			return nil, err
		}
		m.Role, m.At = Role(role), time.Unix(at, 0)
		_ = json.Unmarshal([]byte(tools), &m.Tools)
		_ = json.Unmarshal([]byte(files), &m.Files)
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

func (s *Store) Get(convID int64) (Conversation, error) {
	var c Conversation
	var up int64
	err := s.db.QueryRow(`SELECT id, title, updated_at, summary, summarized FROM conversations WHERE id=?`,
		convID).Scan(&c.ID, &c.Title, &up, &c.Summary, &c.Summarize)
	c.Updated = time.Unix(up, 0)
	return c, err
}

func (s *Store) SetSummary(convID int64, summary string, covered int) error {
	_, err := s.db.Exec(`UPDATE conversations SET summary=?, summarized=? WHERE id=?`, summary, covered, convID)
	return err
}

func (s *Store) SetTitle(convID int64, title string) error {
	_, err := s.db.Exec(`UPDATE conversations SET title=? WHERE id=? AND title=''`, title, convID)
	return err
}

func (s *Store) Delete(convID int64) error {
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
