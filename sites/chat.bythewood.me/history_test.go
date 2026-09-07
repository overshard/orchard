package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// The shape the database had before the ids became uuids, without the sources
// column either, which is the state the deployed one is actually in.
const oldSchema = `
CREATE TABLE conversations (
  id INTEGER PRIMARY KEY, title TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
  summary TEXT NOT NULL DEFAULT '', summarized INTEGER NOT NULL DEFAULT 0);
CREATE TABLE messages (
  id INTEGER PRIMARY KEY,
  conv_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role TEXT NOT NULL, content TEXT NOT NULL,
  tools TEXT NOT NULL DEFAULT '[]', display TEXT NOT NULL DEFAULT '',
  files TEXT NOT NULL DEFAULT '[]', at INTEGER NOT NULL);
INSERT INTO conversations(id,title,created_at,updated_at) VALUES(1,'First',10,10),(2,'Second',20,20);
INSERT INTO messages(conv_id,role,content,at) VALUES
  (1,'user','one question',11),(1,'assistant','one answer',12),
  (2,'user','two question',21),(2,'assistant','two answer',22);
`

func TestMigrateToUUIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := OpenStore(path)
	if err != nil {
		t.Fatalf("opening a counter-id database: %v", err)
	}
	defer st.Close()

	convs, err := st.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 2 {
		t.Fatalf("want 2 conversations, got %d", len(convs))
	}
	seen := map[string]bool{}
	for _, c := range convs {
		if len(c.ID) != 36 {
			t.Errorf("%q is not a uuid", c.ID)
		}
		if seen[c.ID] {
			t.Errorf("two conversations share the id %q", c.ID)
		}
		seen[c.ID] = true

		msgs, err := st.Messages(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) != 2 {
			t.Fatalf("%s: want 2 messages, got %d", c.Title, len(msgs))
		}
		// The pair has to have stayed with its own conversation and in order.
		want := map[string]string{"First": "one", "Second": "two"}[c.Title]
		if msgs[0].Content != want+" question" || msgs[1].Content != want+" answer" {
			t.Errorf("%s carries the wrong messages: %+v", c.Title, msgs)
		}
	}

	// Reopening finds uuids already and leaves everything alone.
	st.Close()
	again, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	convs2, _ := again.List(10)
	if len(convs2) != 2 {
		t.Fatalf("a second open changed the count to %d", len(convs2))
	}
	for _, c := range convs2 {
		if !seen[c.ID] {
			t.Errorf("a second open handed out new ids, %q", c.ID)
		}
	}
}

func TestNewIDIsAUUID(t *testing.T) {
	a, b := newID(), newID()
	if a == b {
		t.Fatal("two calls returned the same id")
	}
	if len(a) != 36 || a[8] != '-' || a[13] != '-' || a[18] != '-' || a[23] != '-' {
		t.Fatalf("%q is not the canonical shape", a)
	}
	if a[14] != '4' {
		t.Errorf("%q is not version 4", a)
	}
	if !strings.ContainsRune("89ab", rune(a[19])) {
		t.Errorf("%q has the wrong variant nibble", a)
	}
}
