package main

import (
	"database/sql"
	"testing"
)

func TestRockShelfKeepsBoiledRocks(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	if err := s.write(func(tx *sql.Tx) error { return addItem(tx, id, "wood", 1) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`DELETE FROM items WHERE pond_id = ? AND kind = 'wood'`, id); err != nil {
		t.Fatal(err)
	}
	v, err := s.view(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Rocks) != len(rocks) {
		t.Fatalf("the shelf has %d rocks, want %d", len(v.Rocks), len(rocks))
	}
	for _, r := range v.Rocks {
		if r.Kind == "wood" && (!r.Found || r.Count != 0) {
			t.Fatalf("wood on the shelf = %+v, want found with none held", r)
		}
		if r.Kind == "agate" && r.Found {
			t.Fatal("agate is on the shelf without ever being had")
		}
	}
}

func TestEveryRockHasANameAndALean(t *testing.T) {
	for _, r := range rocks {
		if itemNames[r.kind] == "" {
			t.Errorf("%s has no name", r.kind)
		}
		if rockLeans[r.kind] == nil {
			t.Errorf("%s doesn't lean the dye works", r.kind)
		}
	}
}
