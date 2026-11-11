package main

import (
	"errors"
	"testing"
	"time"
)

func raceRivalOf(board []raceRival, name string) raceRival {
	for _, r := range board {
		if r.Name == name {
			return r
		}
	}
	return raceRival{}
}

func TestTheRaceIsTheSameForEveryPond(t *testing.T) {
	s, _ := testStore(t)
	_, a := newPond(t, s)
	_, b := newPond(t, s)
	x, err := s.raceStart(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := s.raceStart(b)
	if err != nil {
		t.Fatal(err)
	}
	if x.Seed != y.Seed || x.Run == y.Run || !x.Live || len(x.Race.Board) != len(residents) {
		t.Errorf("two ponds' starts: %+v %+v", x, y)
	}
	for i := range x.Race.Board {
		if x.Race.Board[i] != y.Race.Board[i] {
			t.Errorf("the boards differ at %d", i)
		}
	}
	if raceSeed("2026-11-10") == raceSeed("2026-11-11") {
		t.Error("two days with one course")
	}
}

func TestRushIsNearTheTopEveryDay(t *testing.T) {
	day := time.Date(2026, 1, 1, 12, 0, 0, 0, pondZone)
	for i := range 365 {
		board := raceBoard(dayOf(day.AddDate(0, 0, i)))
		rush := raceRivalOf(board, "Rush")
		above := 0
		for _, r := range board {
			if r.Gates > rush.Gates {
				above++
			}
			if r.Gates < 3 || r.Gates > raceMost {
				t.Fatalf("%s on %d gates", r.Name, r.Gates)
			}
		}
		if above > 1 {
			t.Errorf("%s has %d ahead of Rush", dayOf(day.AddDate(0, 0, i)), above)
		}
	}
}

func TestOnlyTheFirstRaceCounts(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	board := raceBoard(dayOf(*clock))
	top := 0
	for _, r := range board {
		top = max(top, r.Gates)
	}
	race := func(score, gates int, secs time.Duration) *raceDone {
		t.Helper()
		st, err := s.raceStart(id)
		if err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(secs * time.Second)
		d, err := s.raceFinish(id, st.Run, score, gates)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if _, err := s.raceFinish(id, "nope", 5, 5); !errors.Is(err, errBadRun) {
		t.Errorf("a race never started: %v", err)
	}
	// More gates than the course could give in the time count for nothing.
	if d := race(top+5, top+1, 3); d.Live || d.Place != 0 || d.Prize != 0 {
		t.Errorf("an impossible race: %+v", d)
	}
	before, _ := s.view(id)
	d := race(top+5, top+1, 60)
	if !d.Live || d.Place != 1 || d.Prize != racePrize[0] || d.Pond.Pebbles != before.Pebbles+d.Prize {
		t.Fatalf("a winning first race: %+v", d)
	}
	if d.Pond.Dive.Race.Place != 1 || d.Pond.Dive.Race.Gates != top+1 || d.Pond.Dive.Paid != d.Prize {
		t.Errorf("the view after it: %+v", d.Pond.Dive)
	}
	again := race(top+5, top+1, 60)
	if again.Live || again.Prize != 0 || again.Place != 1 || again.Race.Gates != top+1 {
		t.Errorf("a second race counted: %+v", again)
	}
	// A new day is a new board and a new counted run.
	*clock = clock.Add(24 * time.Hour)
	if d := race(2, 0, 30); !d.Live || d.Place != len(residents)+1 || d.Prize != 0 {
		t.Errorf("a last place first race: %+v", d)
	}
}

func TestTheRacePrizeStaysInsideDivesCap(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	for range 40 {
		dive(t, s, clock, id, 40)
	}
	st, _ := s.raceStart(id)
	*clock = clock.Add(90 * time.Second)
	d, err := s.raceFinish(id, st.Run, 85, 80)
	if err != nil {
		t.Fatal(err)
	}
	if d.Place != 1 || d.Prize != 0 || d.Pond.Dive.Paid != diveCap {
		t.Errorf("a win with the cap full: %+v paid %d", d, d.Pond.Dive.Paid)
	}
}
