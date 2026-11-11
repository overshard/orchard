package main

import (
	"testing"
	"time"
)

func TestEveryDiskHasTwoGoalsADay(t *testing.T) {
	keys := map[string]bool{}
	for _, a := range aimList {
		if keys[a.key] {
			t.Errorf("%s is listed twice", a.key)
		}
		keys[a.key] = true
		if len(a.line) > 40 {
			t.Errorf("%s is too long for the title card: %q", a.key, a.line)
		}
	}
	games := []string{"dive"}
	for _, g := range arcadeGames {
		games = append(games, g.key)
	}
	day := time.Date(2026, 11, 1, 12, 0, 0, 0, pondZone)
	for _, g := range games {
		seen := map[string]bool{}
		for i := range 60 {
			d := dayOf(day.AddDate(0, 0, i))
			as := aimsOf(d, g)
			if len(as) != aimsADay || as[0].key == as[1].key {
				t.Fatalf("%s on %s has %v", g, d, as)
			}
			if again := aimsOf(d, g); again[0].key != as[0].key || again[1].key != as[1].key {
				t.Errorf("%s changed within %s", g, d)
			}
			seen[as[0].key+as[1].key] = true
		}
		if all := len(aimsFor(g)); all > aimsADay && len(seen) < 3 {
			t.Errorf("%s dealt the same pair %d ways in 60 days", g, len(seen))
		}
	}
}

func aimsFor(game string) []aim {
	var out []aim
	for _, a := range aimList {
		if a.game == game {
			out = append(out, a)
		}
	}
	return out
}

func TestAGoalPaysOnceInsideTheCap(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	game := "jar"
	g, _ := arcadeOf(game)
	run := func(score int, counts map[string]int) *arcadeDone {
		t.Helper()
		st, err := s.arcadeStart(id, game)
		if err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(time.Minute)
		d, err := s.arcadeFinish(id, game, st.Run, score, nil, counts)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	all := map[string]int{"kings": 1, "elders": 1, "frogs": 3, "froglets": 8}
	// Counts that need more than the score can't meet anything.
	if d := run(25, all); len(d.Aims) != 0 || d.Paid != 1 {
		t.Errorf("counts past the score: %+v", d)
	}
	d := run(140, all)
	if len(d.Aims) != aimsADay || d.Paid != 140/g.per+aimsADay*aimPebbles {
		t.Errorf("a run that met both: %+v", d)
	}
	if d := run(140, all); len(d.Aims) != 0 || d.Paid != 140/g.per {
		t.Errorf("a second run met them again: %+v", d)
	}
	v, _ := s.view(id)
	for _, a := range v.Arcade {
		if a.Key == game && (len(a.Aims) != aimsADay || !a.Aims[0].Done || !a.Aims[1].Done) {
			t.Errorf("the view has %+v", a.Aims)
		}
	}
	// A day with the cap filled still ticks a goal and pays nothing past it.
	*clock = clock.Add(24 * time.Hour)
	for range 12 {
		run(1000, nil)
	}
	if d := run(140, all); len(d.Aims) != aimsADay || d.Paid != 0 {
		t.Errorf("past the cap: %+v", d)
	}
	if v, _ := s.view(id); v.Arcade[len(v.Arcade)-1].Paid > g.cap {
		t.Errorf("paid %d past a cap of %d", v.Arcade[len(v.Arcade)-1].Paid, g.cap)
	}
}

func TestAGoalNeedsItsNoneAtNought(t *testing.T) {
	a := aim{need: map[string]int{"gates": 10}, none: []string{"bugs"}}
	if !a.met(10, map[string]int{"gates": 10, "bugs": 0}) || a.met(10, map[string]int{"gates": 10}) || a.met(12, map[string]int{"gates": 10, "bugs": 2}) {
		t.Error("a goal with none")
	}
	if countsFit("dive", 5, map[string]int{"gates": 20}) || countsFit("dive", 50, map[string]int{"gates": -1}) {
		t.Error("counts past the score fit")
	}
}
