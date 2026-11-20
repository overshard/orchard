package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateOldRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE devices (id INTEGER PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'phone', token TEXT NOT NULL UNIQUE,
			off_lists TEXT NOT NULL DEFAULT '', created INTEGER NOT NULL, last_seen INTEGER NOT NULL DEFAULT 0, last_proto TEXT NOT NULL DEFAULT '', last_ip TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE rules (id INTEGER PRIMARY KEY, domain TEXT NOT NULL UNIQUE, action TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', created INTEGER NOT NULL)`,
		`INSERT INTO rules (domain, action, created) VALUES ('kept.example', 'allow', 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rs, _ := s.Rules()
	if len(rs) != 1 || rs[0].Domain != "kept.example" || rs[0].Device != 0 {
		t.Fatalf("rules after migrating: %+v", rs)
	}
	if err := s.PutRuleFor("kept.example", "block", "", 3, ""); err != nil {
		t.Fatalf("a device rule beside an every device one: %v", err)
	}
}
