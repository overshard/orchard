package main

import (
	"errors"
	"testing"
	"time"
)

func TestForageOnceADay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.forage(id); !errors.Is(err, errNotYet) {
		t.Fatalf("a pond with only its front frog foraged: %v", err)
	}
	hatched := clock.Add(-20 * 24 * time.Hour).Unix()
	if _, err := s.w.Exec(`INSERT INTO frogs (pond_id, seed, name, hatched_at, out_front) VALUES (?, 4242, 'X', ?, 0), (?, 99, 'Y', ?, 0)`,
		id, hatched, id, clock.Unix()); err != nil {
		t.Fatal(err)
	}
	v, err := s.view(id)
	if err != nil || v.Forage != 1 {
		t.Fatalf("forage ready = %d, want 1 since one is an egg: %v", v.Forage, err)
	}
	got, err := s.forage(id)
	if err != nil || len(got.Finds) != 1 || got.Pond.Forage != 0 {
		t.Fatalf("forage gave %+v: %v", got, err)
	}
	if _, err := s.forage(id); !errors.Is(err, errNotYet) {
		t.Fatalf("foraged twice in a day: %v", err)
	}
	*clock = clock.Add(24 * time.Hour)
	if _, err := s.forage(id); err != nil {
		t.Fatalf("no forage the next day: %v", err)
	}
}

func TestEveryNatureForages(t *testing.T) {
	now := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	for _, n := range natures {
		for range 200 {
			z := rollForage(n, now, false)
			if z.Text == "" || (z.Pebbles == 0 && z.Item == "") {
				t.Fatalf("%s brought back %+v", n, z)
			}
			if z.Item != "" && thingName(z.Item) == "" {
				t.Fatalf("%s brought back %q, which has no name", n, z.Item)
			}
		}
	}
}
