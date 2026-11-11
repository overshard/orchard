package main

import "testing"

func kinds(ts []todo) map[string]bool {
	out := map[string]bool{}
	for _, t := range ts {
		out[t.Kind] = true
	}
	return out
}

func TestTodoDropsAGameOnceItsPaid(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	v, err := s.view(id)
	if err != nil {
		t.Fatal(err)
	}
	if k := kinds(v.Todo); !k["wheel"] || !k["log"] || !k["dive"] || !k["arcade"] {
		t.Fatalf("a new pond's todo: %+v", v.Todo)
	}
	day := dayOf(*clock)
	if _, err := s.w.Exec(`UPDATE ponds SET dive_runs = 3, dive_day = ?, log_day = ?, free_spins = 0 WHERE id = ?`, day, day, id); err != nil {
		t.Fatal(err)
	}
	for _, g := range arcadeGames {
		if _, err := s.w.Exec(`INSERT INTO arcade (pond_id, game, day, runs, paid, best, run, started_at) VALUES (?, ?, ?, ?, 30, 30, '', 0)`, id, g.key, day, fullRuns(g.key, *clock)); err != nil {
			t.Fatal(err)
		}
	}
	v, _ = s.view(id)
	if k := kinds(v.Todo); k["dive"] || k["arcade"] || k["wheel"] || k["log"] {
		t.Errorf("games past their full pay still suggested: %+v", v.Todo)
	}
	give(t, s, id, 100)
	if v, _ := s.view(id); len(v.Todo) != 1 || v.Todo[0].To != "town" {
		t.Errorf("the bank suggested before it's open: %+v", v.Todo)
	}
	if _, err := s.w.Exec(`UPDATE ponds SET staged = 0 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.view(id); len(v.Todo) != 1 || v.Todo[0].Label != "Build the gravel bar" {
		t.Errorf("with nothing to do and 100 pebbles: %+v", v.Todo)
	}
}
