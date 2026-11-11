package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func goalKinds(gs []goal) string {
	out := []string{}
	for _, g := range gs {
		out = append(out, g.Kind)
	}
	return strings.Join(out, " ")
}

func exec(t *testing.T, s *store, q string, args ...any) {
	t.Helper()
	if _, err := s.w.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func TestANewPondsGoalsAreItsFirstDay(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	if got := goalKinds(v.Goals); got != "wheel log dive key" {
		t.Fatalf("a new pond's goals: %s", got)
	}
	for _, g := range v.Goals {
		if !g.First || g.Done {
			t.Errorf("a first day step: %+v", g)
		}
	}
	if _, err := s.spin(id, false); err != nil {
		t.Fatal(err)
	}
	v, _ = s.view(id)
	if !v.Goals[0].Done || v.Goals[1].Done {
		t.Errorf("after a spin: %+v", v.Goals)
	}
	// The steps go out once, as goals.
	raw, _ := json.Marshal(v.Starter)
	if strings.Contains(string(raw), "teps") {
		t.Errorf("the starter still sends its steps: %s", raw)
	}
}

func TestGoalsForAWeekOldPond(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET starter = ? WHERE id = ?`, starterTaken, id)
	age(t, s, id, 7*24*time.Hour)
	v, _ := s.view(id)
	if got := goalKinds(v.Goals); got != "idle bar gift" {
		t.Errorf("a week old pond that's done nothing: %s", got)
	}
	if g := v.Goals[1]; g.Count != "20/100" || g.To != "bank" {
		t.Errorf("the gravel bar goal: %+v", g)
	}
}

func TestGoalsForAWellUsedPond(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET starter = ?, staged = 0, tried = ?, gravel_level = 1, garden_level = 1, dive_best = 40 WHERE id = ?`,
		starterTaken, triedStall|triedShare, id)
	age(t, s, id, 30*24*time.Hour)
	exec(t, s, `UPDATE frogs SET job = 'forage', job_at = ? WHERE pond_id = ?`, clock.Unix(), id)
	exec(t, s, `INSERT INTO frogs (pond_id, seed, name, hatched_at, out_front) VALUES (?, 7, 'PIP', ?, 0)`, id, clock.Unix())
	exec(t, s, `INSERT INTO neighbours (pond_id, who, points) VALUES (?, 0, 5)`, id)
	for _, g := range arcadeGames {
		exec(t, s, `INSERT INTO arcade (pond_id, game, best) VALUES (?, ?, 10)`, id, g.key)
	}
	v, err := s.view(id)
	if err != nil {
		t.Fatal(err)
	}
	if got := goalKinds(v.Goals); got != "cook" {
		t.Fatalf("a well used pond: %s", got)
	}
	if g := v.Goals[0]; !strings.HasPrefix(g.Count, "0/") || g.To != "garden" {
		t.Errorf("the cooking goal: %+v", g)
	}
	for a := range ingredients {
		for b := a; b < len(ingredients); b++ {
			exec(t, s, `INSERT INTO cookbook (pond_id, dish, first_at) VALUES (?, ?, 0)`, id, dishOf(a, b).kind())
		}
	}
	if v, _ := s.view(id); len(v.Goals) != 0 {
		t.Errorf("a pond that's done it all: %+v", v.Goals)
	}
}

func TestNoGoalPointsAtAClosedPlace(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET starter = ?, pebbles = 500 WHERE id = ?`, starterTaken, id)
	shares := func(v *view) bool { return strings.Contains(goalKinds(v.Goals), "share") }
	for _, by := range []time.Duration{0, 24 * time.Hour, 72 * time.Hour} {
		age(t, s, id, by)
		v, _ := s.view(id)
		for _, g := range v.Goals {
			if !v.Open.has(g.To) {
				t.Errorf("%v in, a goal points at a closed place: %+v", by, g)
			}
		}
		for _, td := range v.Todo {
			if !v.Open.has(td.To) {
				t.Errorf("%v in, a todo points at a closed place: %+v", by, td)
			}
		}
		if len(v.Goals) == 0 || len(v.Goals) > goalsShown {
			t.Errorf("%v in: %s", by, goalKinds(v.Goals))
		}
		if shares(v) && !v.Open.Market {
			t.Errorf("%v in, the market is a goal before it's open", by)
		}
	}
}

func TestListingOnTheStallIsRemembered(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET starter = ?, gravel_level = 1 WHERE id = ?`, starterTaken, id)
	exec(t, s, `INSERT INTO neighbours (pond_id, who, points) VALUES (?, 0, 5)`, id)
	exec(t, s, `INSERT INTO items (pond_id, kind, count) VALUES (?, 'sticks', 2)`, id)
	if v, _ := s.view(id); !strings.Contains(goalKinds(v.Goals), "stall") {
		t.Fatalf("no stall goal: %s", goalKinds(v.Goals))
	}
	worth, _ := worthOf("sticks")
	if _, err := s.stallList(id, "sticks", worth); err != nil {
		t.Fatal(err)
	}
	exec(t, s, `DELETE FROM stall WHERE pond_id = ?`, id)
	if v, _ := s.view(id); strings.Contains(goalKinds(v.Goals), "stall") {
		t.Errorf("the stall is still a goal after a listing: %s", goalKinds(v.Goals))
	}
}
