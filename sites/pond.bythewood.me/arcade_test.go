package main

import (
	"errors"
	"testing"
	"time"
)

func TestArcadePaysAndCaps(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.arcadeFinish(id, "flick", "nope", 10, nil); !errors.Is(err, errBadRun) {
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
		d, err := s.arcadeFinish(id, "flick", st.Run, score, nil)
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
	d, err := s.arcadeFinish(id, "cross", st.Run, 61, nil)
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

// A run that sat paused behind a hidden page for an hour pays like any other.
func TestAPausedRunStillPays(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	st, err := s.arcadeStart(id, "flick")
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Hour)
	if d, err := s.arcadeFinish(id, "flick", st.Run, 12, nil); err != nil || d.Paid != 12 {
		t.Errorf("a paused flick: %v %+v", err, d)
	}
	run, err := s.diveStart(id)
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Hour)
	if d, err := s.diveFinish(id, run.Run, 9, 6); err != nil || d.Score != 9 || d.Paid == 0 {
		t.Errorf("a paused dive: %v %+v", err, d)
	}
}

// The disk of the day pays in full a fourth time, under the same cap, and
// it's the same disk for every pond that day.
func TestDiskOfTheDay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	_, other := newPond(t, s)
	key := diskOfDay(dayOf(*clock))
	g, _ := arcadeOf(key)
	play := func(k string, score int) *arcadeDone {
		t.Helper()
		st, err := s.arcadeStart(id, k)
		if err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(30 * time.Second)
		d, err := s.arcadeFinish(id, k, st.Run, score, nil)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	plain := arcadeGames[0]
	if plain.key == key {
		plain = arcadeGames[1]
	}
	one := 4 * g.per
	for i := 1; i <= 5; i++ {
		want := 4
		if i == 5 {
			want = 1
		}
		if d := play(key, one); d.Paid != want {
			t.Errorf("run %d of the disk of the day paid %d, want %d", i, d.Paid, want)
		}
	}
	for i := 1; i <= 4; i++ {
		want := 4
		if i == 4 {
			want = 1
		}
		if d := play(plain.key, 4*plain.per); d.Paid != want {
			t.Errorf("run %d of %s paid %d, want %d", i, plain.key, d.Paid, want)
		}
	}
	days, total := 0, 0
	for _, pid := range []int64{id, other} {
		v, _ := s.view(pid)
		for _, a := range v.Arcade {
			if a.Day {
				days++
				if a.Key != key || a.Full != arcadeFullRuns+1 || a.Cap != g.cap {
					t.Errorf("the disk of the day on the view: %+v", a)
				}
			}
			total++
		}
	}
	if days != 2 || total != 2*len(arcadeGames) {
		t.Errorf("%d disks of the day across two ponds", days)
	}
	seen := map[string]bool{}
	for i := range 120 {
		seen[diskOfDay(dayOf(clock.Add(time.Duration(i)*24*time.Hour)))] = true
	}
	if len(seen) != len(arcadeGames) {
		t.Errorf("only %d of %d games were ever the disk of the day", len(seen), len(arcadeGames))
	}
}
