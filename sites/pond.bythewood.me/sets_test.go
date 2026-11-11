package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func setOf(v *view, kind string) setView {
	for _, s := range v.Sets {
		if s.Kind == kind {
			return s
		}
	}
	return setView{}
}

func TestEverySetMemberIsReal(t *testing.T) {
	// Kinds are stored, so the list only ever grows at the end.
	want := []string{"mushrooms", "night", "garden", "shiny", "workshop", "weather"}
	seen := map[string]string{}
	for i, s := range sets {
		if i < len(want) && s.kind != want[i] {
			t.Errorf("set %d is %s, and it was stored as %s", i, s.kind, want[i])
		}
		easy, hard := false, false
		for _, k := range s.members {
			m, ok := memberOf(k)
			if !ok || m.Name == "" || m.Name == k || m.Where == "" || m.Sort == "" {
				t.Errorf("%s has %s in it, which isn't a thing: %+v", s.kind, k, m)
			}
			if other, dup := seen[k]; dup {
				t.Errorf("%s is in %s and %s", k, other, s.kind)
			}
			seen[k] = s.kind
			if pets[k] != "" {
				t.Errorf("%s asks for %s, and a pet can take months of work", s.kind, k)
			}
			if d, ok := dishOfKind(k); ok {
				if _, named := namedDishes[[2]int{d.a, d.b}]; !named {
					t.Errorf("%s asks for %s, a dish with no name of its own", s.kind, d.name)
				}
			}
			easy, hard = easy || m.Tier <= 1, hard || m.Tier >= 2
		}
		if !easy || !hard {
			t.Errorf("%s isn't a mix of easy and hard", s.kind)
		}
		if s.gives == "" || !strings.HasPrefix(s.scene, "s_") {
			t.Errorf("%s gives %q as %q", s.kind, s.gives, s.scene)
		}
	}
	if _, ok := memberOf("t_nothing"); ok {
		t.Error("a kind nobody has is a member")
	}
}

// A set is read off the found tables, so nothing in it has to be held, and
// once it's finished it stays finished.
func TestASetCountsWhatWasSoldAway(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `INSERT INTO trinkets_found (pond_id, kind) VALUES (?, 't_marble'), (?, 't_earring')`, id, id)
	exec(t, s, `INSERT INTO rocks_found (pond_id, kind) VALUES (?, 'pyrite')`, id)
	v, _ := s.view(id)
	if got := setOf(v, "shiny"); got.Done || got.Have != 3 || got.Members[3].Found || !got.Members[0].Found {
		t.Fatalf("three of the shiny things: %+v", got)
	}
	exec(t, s, `INSERT INTO fish (pond_id, kind, count, best, first_day) VALUES (?, 'koi', 1, 200, '2026-11-01')`, id)
	if v, _ = s.view(id); !setOf(v, "shiny").Done || setOf(v, "shiny").Day != "" || v.Landed != nil {
		t.Fatalf("all four found, before a move: %+v", setOf(v, "shiny"))
	}
	for _, it := range v.Bag {
		if slices.Contains(sets[3].members[:], it.Kind) {
			t.Errorf("%s is in the bag, and the set is meant to stand without it", it.Kind)
		}
	}
	g, err := s.greet(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if g.Pond.Landed == nil || !slices.Contains(g.Pond.Landed.Sets, "shiny") || setOf(g.Pond, "shiny").Day != "2026-11-10" {
		t.Fatalf("the greeting after: %+v", g.Pond.Landed)
	}
	// The scenery is the set's scene on a finished set, which the page draws.
	raw, _ := json.Marshal(setOf(g.Pond, "shiny"))
	if !strings.Contains(string(raw), `"scene":"s_chime"`) || !strings.Contains(string(raw), `"done":true`) {
		t.Errorf("the wind chime isn't out: %s", raw)
	}
	if g, _ = s.greet(id, false); g.Pond.Landed != nil {
		t.Errorf("it landed twice: %+v", g.Pond.Landed)
	}
	exec(t, s, `DELETE FROM trinkets_found WHERE pond_id = ?`, id)
	if v, _ = s.view(id); !setOf(v, "shiny").Done {
		t.Error("a finished set came undone")
	}
	var n int
	s.r.QueryRow(`SELECT COUNT(*) FROM sets WHERE pond_id = ? AND kind = 'shiny'`, id).Scan(&n)
	if n != 1 {
		t.Errorf("%d rows for the one set", n)
	}
	// It's in the diary the day after, once the frog is out of its egg.
	exec(t, s, `UPDATE frogs SET hatched_at = hatched_at - 10*86400 WHERE pond_id = ?`, id)
	*clock = clock.Add(24 * time.Hour)
	s.greet(id, false)
	if got := diaryAll(t, s, id); len(got) != 1 || !strings.Contains(got[0].Line, "wind chime") {
		t.Errorf("the diary after a set: %+v", got)
	}
}

func TestASetMarksTheDishItsWaitingOn(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET staged = 0`)
	hold(t, s, id, "t_button", 1)
	hold(t, s, id, "t_chanterelle", 1)
	v, _ := s.view(id)
	want := "Goes in a dish the mushrooms set is waiting on."
	if !slices.Contains(marksOn(v, "t_button"), want) || !slices.Contains(marksOn(v, "t_chanterelle"), want) {
		t.Fatalf("the two mushrooms: %q %q", marksOn(v, "t_button"), marksOn(v, "t_chanterelle"))
	}
	exec(t, s, `INSERT INTO cookbook (pond_id, dish, first_at) VALUES (?, 'dish_8_9', 0)`, id)
	if v, _ = s.view(id); slices.Contains(marksOn(v, "t_button"), want) {
		t.Error("still marked once the soup's been made")
	}
}

func TestTheNearestSetAndBadgeAreGoals(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET starter = ? WHERE id = ?`, starterTaken, id)
	v, _ := s.view(id)
	kinds := strings.Fields(goalKinds(v.Goals))
	if n := len(kinds); n < 3 || kinds[n-2] != "badge" || kinds[n-1] != "set" {
		t.Fatalf("a pond with nothing found: %v", kinds)
	}
	// Three of a set is a step from done, so it goes over the things to try.
	exec(t, s, `INSERT INTO trinkets_found (pond_id, kind) VALUES (?, 't_marble'), (?, 't_earring'), (?, 't_raft')`, id, id, id)
	exec(t, s, `INSERT INTO rocks_found (pond_id, kind) VALUES (?, 'pyrite')`, id)
	v, _ = s.view(id)
	if g := v.Goals[0]; g.Kind != "set" || g.Label != "Finish the shiny things set" || g.Count != "3/4" || g.To != "collect" {
		t.Errorf("three of the shiny things: %+v", g)
	}
	for h := 1; h <= 9; h++ {
		exec(t, s, `INSERT INTO hats (pond_id, shape, colour, count) VALUES (?, ?, 0, 1)`, id, h)
	}
	v, _ = s.view(id)
	if got := goalKinds(v.Goals[:2]); got != "badge set" {
		t.Fatalf("nine hats and three of a set: %s", goalKinds(v.Goals))
	}
	if g := v.Goals[0]; g.Label != "Find ten hats" || g.Count != "9/10" {
		t.Errorf("nine hats: %+v", g)
	}
	sets, pins := 0, 0
	for _, g := range v.Goals {
		sets += one(g.Kind == "set")
		pins += one(g.Kind == "badge")
	}
	if sets != 1 || pins != 1 {
		t.Errorf("%d set rows and %d badge rows: %s", sets, pins, goalKinds(v.Goals))
	}
}
