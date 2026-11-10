package main

import (
	"errors"
	"testing"
	"time"
)

func TestArcadePaysAndCaps(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.arcadeFinish(id, "flick", "nope", 10); !errors.Is(err, errBadRun) {
		t.Fatalf("a run never started: %v", err)
	}
	if _, err := s.arcadeStart(id, "pinball"); !errors.Is(err, errNotHeld) {
		t.Errorf("a game that isn't one: %v", err)
	}
	play := func(score int, secs time.Duration) *arcadeDone {
		t.Helper()
		st, err := s.arcadeStart(id, "flick")
		if err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(secs * time.Second)
		d, err := s.arcadeFinish(id, "flick", st.Run, score)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if d := play(500, 5); d.Score != 0 || d.Paid != 0 {
		t.Errorf("an impossible run paid: %+v", d)
	}
	start := play(0, 1).Pond.Pebbles
	d := play(10, 20)
	if d.Paid != 10 || !d.Best || d.Pond.Pebbles != start+10 {
		t.Errorf("a first run: %+v", d)
	}
	play(10, 20)
	if d := play(30, 20); d.Paid != 7 {
		t.Errorf("a fourth run paid %d, want a quarter", d.Paid)
	}
	for range 10 {
		play(60, 20)
	}
	if v, _ := s.view(id); v.Arcade[0].Paid != v.Arcade[0].Cap {
		t.Errorf("past the cap: %+v", v.Arcade[0])
	}
	*clock = clock.Add(24 * time.Hour)
	if v, _ := s.view(id); v.Arcade[0].Paid != 0 || v.Arcade[0].Best != 60 {
		t.Errorf("a new day: %+v", v.Arcade[0])
	}
}

// A crossing run that would have filled the day's cap pays half of it.
func TestCrossingPaysHalf(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	st, err := s.arcadeStart(id, "cross")
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(60 * time.Second)
	d, err := s.arcadeFinish(id, "cross", st.Run, 61)
	if err != nil {
		t.Fatal(err)
	}
	if d.Paid != 30 {
		t.Errorf("a 61 point crossing paid %d, want 30", d.Paid)
	}
}

// Every game the page can open has to be one the server will start.
func TestNewSideGamesStart(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	for _, k := range []string{"fish", "stack", "shell", "skip"} {
		if _, err := s.arcadeStart(id, k); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
}
