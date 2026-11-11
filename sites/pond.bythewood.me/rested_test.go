package main

import (
	"testing"
	"time"
)

func play(t *testing.T, s *store, clock *time.Time, id int64, key string, score int) *arcadeDone {
	t.Helper()
	st, err := s.arcadeStart(id, key)
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(2 * time.Minute)
	out, err := s.arcadeFinish(id, key, st.Run, score, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWellRestedPaysAQuarterMoreOnce(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if v, _ := s.view(id); v.Rested {
		t.Fatal("an egg laid a moment ago is well rested")
	}
	*clock = clock.Add(restAfter)
	// Tongue flick pays a pebble a point.
	if out := play(t, s, clock, id, "flick", 20); out.Paid != 25 || out.Rested != 5 || out.Pond.Rested {
		t.Fatalf("the first run: %+v, still rested %v", out, out.Pond.Rested)
	}
	// One game uses it up for every game.
	if out := play(t, s, clock, id, "raft", 20); out.Paid != 20 || out.Rested != 0 {
		t.Fatalf("the next game: %+v", out)
	}
	*clock = clock.Add(restAfter - 3*time.Minute)
	if out := play(t, s, clock, id, "raft", 20); out.Rested != 0 {
		t.Fatalf("just short of rested: %+v", out)
	}
	*clock = clock.Add(restAfter)
	v, _ := s.view(id)
	said := false
	for _, td := range v.Todo {
		said = said || td.Kind == "rested"
	}
	if !v.Rested || !said {
		t.Fatalf("rested %v and the strip says so %v", v.Rested, said)
	}
	d := dive(t, s, clock, id, 16)
	base := 32 + len(d.Aims)*aimPebbles
	if d.Paid != base+base/4 || d.Rested != base/4 || d.Pond.Rested {
		t.Fatalf("a rested dive: paid %d with %d for the rest", d.Paid, d.Rested)
	}
}

func TestWellRestedStaysInsideTheCap(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	g, _ := arcadeOf("flick")
	total := 0
	for range 12 {
		*clock = clock.Add(restAfter)
		if dayOf(*clock) != dayOf(clock.Add(-restAfter)) {
			total = 0
		}
		out := play(t, s, clock, id, "flick", 48)
		total += out.Paid
		if total > g.cap || out.Rested > out.Paid || out.Rested > 12 {
			t.Fatalf("%d paid today against a cap of %d, %d of the last for the rest", total, g.cap, out.Rested)
		}
	}
	// A cap with five left pays five, rested or not.
	if paid, bonus := (&pond{}).payRun(40, 5, *clock); paid != 5 || bonus != 0 {
		t.Errorf("five under the cap paid %d with %d for the rest", paid, bonus)
	}
	if paid, bonus := (&pond{}).payRun(40, 45, *clock); paid != 45 || bonus != 5 {
		t.Errorf("forty five under the cap paid %d with %d for the rest", paid, bonus)
	}
	if paid, bonus := (&pond{}).payRun(40, -3, *clock); paid != 0 || bonus != 0 {
		t.Errorf("a cap already passed paid %d", paid)
	}
}
