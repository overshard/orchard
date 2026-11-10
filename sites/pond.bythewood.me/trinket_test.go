package main

import (
	"errors"
	"testing"
)

func TestTrinketsPlaceAndCook(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 60)
	o, err := s.trinketBox(id)
	if err != nil {
		t.Fatal(err)
	}
	got := o.Prize.Item
	if _, ok := trinketOf(got); !ok || o.Pond.Pebbles != 0 {
		t.Fatalf("a box gave %q and left %d", got, o.Pond.Pebbles)
	}
	for _, it := range o.Pond.Items {
		if it.Kind == got {
			t.Errorf("the trinket is in the purse")
		}
	}
	if _, err := s.place(id, 0, "t_pearl"); got != "t_pearl" && !errors.Is(err, errNotHeld) {
		t.Errorf("placed a pearl the pond doesn't have: %v", err)
	}
	if _, err := s.place(id, 2, got); err != nil {
		t.Fatal(err)
	}
	v, err := s.place(id, 0, got)
	if err != nil || v.Trinkets.Spots[0] != got || v.Trinkets.Spots[2] != "" {
		t.Fatalf("moving a trinket left %v: %v", v.Trinkets.Spots, err)
	}

	// A golden morel cooks into a legendary dish, and the set still has it.
	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, 't_goldmorel', 1), (?, 'duckweed', 1)`, id, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`INSERT INTO trinkets_found (pond_id, kind) VALUES (?, 't_goldmorel')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.place(id, 1, "t_goldmorel"); err != nil {
		t.Fatal(err)
	}
	c, err := s.cook(id, "t_goldmorel", "duckweed")
	if err != nil {
		t.Fatal(err)
	}
	if c.Tier != tierCount-1 {
		t.Errorf("a golden morel dish is tier %d", c.Tier)
	}
	if c.Pond.Trinkets.Spots[1] != "" {
		t.Errorf("the eaten morel is still out by the pad")
	}
	for _, tv := range c.Pond.Trinkets.All {
		if tv.Kind == "t_goldmorel" && (!tv.Found || tv.Count != 0) {
			t.Errorf("after cooking: %+v", tv)
		}
	}
}

func TestTrinketSpares(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, 't_duck', 2)`, id); err != nil {
		t.Fatal(err)
	}
	v, err := s.sellTrinket(id, "t_duck")
	if err != nil || v.Pebbles != starterPebbles+15 {
		t.Fatalf("sold a spare duck for %d: %v", v.Pebbles-starterPebbles, err)
	}
	if _, err := s.sellTrinket(id, "t_duck"); !errors.Is(err, errNotHeld) {
		t.Errorf("sold the last duck: %v", err)
	}
}
