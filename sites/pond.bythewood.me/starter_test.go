package main

import (
	"errors"
	"testing"
)

func TestStarterList(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	if v.Starter == nil || v.Starter.Ready || len(v.Starter.Steps) != 4 {
		t.Fatalf("a new pond's list: %+v", v.Starter)
	}
	if _, err := s.starterClaim(id); !errors.Is(err, errNotYet) {
		t.Errorf("claimed with nothing done: %v", err)
	}
	if _, err := s.spin(id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.turnLog(id, 0); err != nil {
		t.Fatal(err)
	}
	dive(t, s, clock, id, 5)
	if err := s.keySeen(id); err != nil {
		t.Fatal(err)
	}
	v, _ = s.view(id)
	if !v.Starter.Ready {
		t.Fatalf("everything done and not ready: %+v", v.Starter)
	}
	before := v.Pebbles
	v, err := s.starterClaim(id)
	if err != nil || v.Pebbles != before+starterPrize || v.Starter != nil {
		t.Fatalf("claiming: %v %d %+v", err, v.Pebbles, v.Starter)
	}
	if _, err := s.starterClaim(id); !errors.Is(err, errNotYet) {
		t.Errorf("claimed twice: %v", err)
	}
	if v, _ := s.view(id); v.Pebbles != before+starterPrize {
		t.Errorf("the prize paid more than once: %d", v.Pebbles)
	}
	for _, g := range v.Goals {
		if g.First {
			t.Errorf("a first day step after the prize: %+v", g)
		}
	}
}
