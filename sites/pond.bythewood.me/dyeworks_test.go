package main

import (
	"errors"
	"testing"
	"time"
)

func TestDyeWorks(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	rocks := []string{"pyrite", "pyrite", "agate"}
	if _, err := s.dyeLoad(id, rocks); !errors.Is(err, errNotHeld) {
		t.Fatalf("loaded a dye works that isn't built: %v", err)
	}
	give(t, s, id, 200)
	if _, err := s.build(id, "dye"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.dyeLoad(id, rocks); !errors.Is(err, errNotHeld) {
		t.Fatalf("loaded rocks the pond doesn't have: %v", err)
	}
	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, 'pyrite', 2), (?, 'agate', 1)`, id, id); err != nil {
		t.Fatal(err)
	}
	v, err := s.dyeLoad(id, rocks)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Bank.Dye.Busy || v.Bank.Dye.ReadyIn != 24*3600 || len(v.Items) != 0 {
		t.Errorf("after loading: %+v, items %v", v.Bank.Dye, v.Items)
	}
	if _, err := s.dyeCollect(id); !errors.Is(err, errNotYet) {
		t.Errorf("collected a batch straight away: %v", err)
	}
	*clock = clock.Add(24 * time.Hour)
	d, err := s.dyeCollect(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := dyeMorph[d.Dye.Kind]; !ok || d.Pond.Bank.Dye.Busy {
		t.Errorf("came out with %+v", d.Dye)
	}
	if d.Dye.Kind == "dye_gold" || d.Dye.Kind == "dye_blue" {
		t.Errorf("a level one works made %s", d.Dye.Kind)
	}
}

// What goes in leans what comes out, and a level only makes its own colours.
func TestDyeLeans(t *testing.T) {
	gold := 0
	for range 3000 {
		switch rollDye(2, []string{"pyrite", "pyrite", "pyrite"}) {
		case "dye_gold":
			gold++
		case "dye_berry", "dye_blue":
			t.Fatal("level two made a colour it can't")
		}
	}
	if gold < 2100 {
		t.Errorf("three pyrite made gold %d times in 3000", gold)
	}
}
