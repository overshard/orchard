package main

import (
	"errors"
	"testing"
)

func TestEverySeedNameIsOnTheList(t *testing.T) {
	for _, b := range nameBase {
		for _, p := range namePre {
			if !validName(p + b) {
				t.Errorf("%q can come off a seed and isn't on the list", p+b)
			}
		}
	}
	seen := map[string]bool{}
	for _, b := range nameBases {
		if seen[b] {
			t.Errorf("%q is on the list twice", b)
		}
		seen[b] = true
	}
	if len(namePool) < 5000 {
		t.Errorf("only %d names to roll", len(namePool))
	}
}

func TestRenameTakesOnlyListedNames(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	p, err := loadPond(s.r, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.rename(id, p.FrogID, "Bobby Tables"); !errors.Is(err, errBadName) {
		t.Fatalf("a typed name went through, %v", err)
	}
	v, err := s.rename(id, p.FrogID, "captain  noodle")
	if err != nil {
		t.Fatal(err)
	}
	if v.Frog.Name != "CAPTAIN NOODLE" || v.Frogs[0].Name != "CAPTAIN NOODLE" {
		t.Fatalf("got %q and %q", v.Frog.Name, v.Frogs[0].Name)
	}
	if _, err := s.rename(id, p.FrogID+99, "PIP"); !errors.Is(err, errNotHeld) {
		t.Fatalf("renamed somebody else's frog, %v", err)
	}
	for _, n := range rollNames() {
		if !validName(n) {
			t.Errorf("rolled %q, which isn't on the list", n)
		}
	}
}
