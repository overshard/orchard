package main

import (
	"slices"
	"strings"
	"testing"
)

func hold(t *testing.T, s *store, id int64, kind string, n int) {
	t.Helper()
	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, ?, ?)
		ON CONFLICT (pond_id, kind) DO UPDATE SET count = excluded.count`, id, kind, n); err != nil {
		t.Fatal(err)
	}
}

// marksOn is every line on a thing in the bag, nil when it isn't there.
func marksOn(v *view, kind string) []string {
	for _, b := range v.Bag {
		if b.Kind == kind {
			return b.Wanted
		}
	}
	return nil
}

func TestTheFavourMarksWhatItAsksFor(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	who, kind, n := favourOf(dayOf(*clock))
	hold(t, s, id, kind, n-1)
	v, _ := s.view(id)
	got := marksOn(v, kind)
	if len(got) != 1 || !strings.HasPrefix(got[0], residents[who].name+" needs ") || !strings.Contains(got[0], "you have") {
		t.Fatalf("one short of the favour: %q", got)
	}
	for _, b := range v.Bag {
		if b.Kind != kind && len(b.Wanted) > 0 {
			t.Errorf("%s is marked: %q", b.Kind, b.Wanted)
		}
	}
	hold(t, s, id, kind, n)
	if v, _ = s.view(id); len(marksOn(v, kind)) != 1 || strings.Contains(marksOn(v, kind)[0], "you have") {
		t.Errorf("with enough for the favour: %q", marksOn(v, kind))
	}
	hold(t, s, id, kind, n+1)
	out, err := s.favour(id)
	if err != nil {
		t.Fatal(err)
	}
	if got := marksOn(out.Pond, kind); len(got) != 0 {
		t.Errorf("after the favour: %q", got)
	}
}

func TestAFavouriteDishIsMarkedUntilItsGiven(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	sedge := residents[0]
	fav, also := sedge.favDish(), dishOf(12, 12)
	a, b := ingredients[fav.a].kind, ingredients[fav.b].kind
	hold(t, s, id, fav.kind(), 2)
	hold(t, s, id, also.kind(), 1)
	hold(t, s, id, a, 1)
	hold(t, s, id, b, 1)
	// Nothing in the kitchen is new, so only a neighbour wants these.
	if _, err := s.w.Exec(`INSERT INTO cookbook (pond_id, dish, first_at) VALUES (?, ?, 0)`, id, fav.kind()); err != nil {
		t.Fatal(err)
	}
	v, _ := s.view(id)
	if got := marksOn(v, fav.kind()); !slices.Contains(got, "Sedge's favourite dish.") {
		t.Fatalf("Sedge's favourite in the bag: %q", got)
	}
	if got := marksOn(v, also.kind()); len(got) != 1 || !strings.HasPrefix(got[0], "Sedge likes this nearly") {
		t.Errorf("the fish dish Sedge likes: %q", got)
	}
	// It hasn't said what goes in it yet.
	if got := marksOn(v, a); len(got) != 0 {
		t.Errorf("an ingredient before the recipe's known: %q", got)
	}
	if _, err := s.w.Exec(`INSERT INTO neighbours (pond_id, who, points) VALUES (?, 0, ?)`, id, friendAt[recipeAt]); err != nil {
		t.Fatal(err)
	}
	v, _ = s.view(id)
	want := "Goes in " + strings.ToLower(fav.name) + ", which Sedge loves."
	if !slices.Contains(marksOn(v, a), want) || !slices.Contains(marksOn(v, b), want) {
		t.Errorf("the ingredients once it's known: %q and %q", marksOn(v, a), marksOn(v, b))
	}
	out, err := s.gift(id, 0, fav.kind())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{fav.kind(), also.kind(), a, b} {
		for _, line := range marksOn(out.Pond, k) {
			if strings.Contains(line, "Sedge") {
				t.Errorf("%s after Sedge has eaten: %q", k, line)
			}
		}
	}
	// A best friend wants for nothing.
	if _, err := s.w.Exec(`UPDATE neighbours SET points = 100, gift_day = '' WHERE pond_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if v, _ = s.view(id); len(marksOn(v, fav.kind())) != 0 {
		t.Errorf("a best friend's favourite: %q", marksOn(v, fav.kind()))
	}
}

// cooks is the lines the kitchen put on a thing, without the day's favour.
func cooks(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.Contains(l, "named dish") {
			out = append(out, l)
		}
	}
	return out
}

func TestAnUnmadeNamedDishMarksItsHalves(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	hold(t, s, id, "duckweed", 2)
	hold(t, s, id, "mayfly", 1)
	hold(t, s, id, "watercress", 1)
	hold(t, s, id, "cricket", 1)
	v, _ := s.view(id)
	if got := marksOn(v, "duckweed"); len(got) != 0 {
		t.Fatalf("marked before the kitchen's open: %q", got)
	}
	exec(t, s, `UPDATE ponds SET staged = 0`)
	v, _ = s.view(id)
	// A cricket sandwich is a tier over pond soup, so it's the one marked.
	cress, cricket := cooks(marksOn(v, "watercress")), cooks(marksOn(v, "cricket"))
	if len(cress) != 1 || cress[0] != "Cooks with cricket into a named dish you haven't made." {
		t.Errorf("watercress: %q", cress)
	}
	if len(cricket) != 1 || cricket[0] != "Cooks with watercress into a named dish you haven't made." {
		t.Errorf("cricket: %q", cricket)
	}
	if duck, fly := cooks(marksOn(v, "duckweed")), cooks(marksOn(v, "mayfly")); len(duck) != 0 || len(fly) != 0 {
		t.Errorf("a plainer dish is marked too: %q %q", duck, fly)
	}
	for _, line := range append(cress, cricket...) {
		if strings.Contains(line, "sandwich") {
			t.Errorf("the line names the dish: %q", line)
		}
	}
	exec(t, s, `INSERT INTO cookbook (pond_id, dish, first_at) VALUES (?, ?, 0)`, id, dishOf(1, 2).kind())
	v, _ = s.view(id)
	if got := cooks(marksOn(v, "duckweed")); len(got) != 1 || got[0] != "Two of these cook into a named dish you haven't made." {
		t.Errorf("duckweed once the sandwich is made: %q", got)
	}
	exec(t, s, `INSERT INTO cookbook (pond_id, dish, first_at) VALUES (?, ?, 0)`, id, dishOf(0, 0).kind())
	v, _ = s.view(id)
	for _, b := range v.Bag {
		if got := cooks(b.Wanted); len(got) != 0 {
			t.Errorf("%s with every named dish made: %q", b.Kind, got)
		}
	}
}

// A young pond's bag is a bit of everything from the garden and a few
// trinkets, and the lamps only mean something while a few of them are lit.
func TestAYoungBagHasFewMarks(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET staged = 0`)
	for _, g := range ingredients {
		hold(t, s, id, g.kind, 3)
	}
	for _, k := range []string{"t_acorn", "t_duck", "sticks", "reeds", "clay", "agate", "quartz", dishOf(0, 1).kind(), dishOf(2, 3).kind()} {
		hold(t, s, id, k, 2)
	}
	exec(t, s, `INSERT INTO hats (pond_id, shape, colour, count) VALUES (?, 4, 0, 2), (?, 6, 0, 1)`, id, id)
	v, _ := s.view(id)
	marked := 0
	for _, b := range v.Bag {
		if len(b.Wanted) > 0 {
			marked++
		}
	}
	if len(v.Bag) < 20 {
		t.Fatalf("the bag only has %d things in it", len(v.Bag))
	}
	// Two halves of one dish and whatever today's favour is after.
	if marked == 0 || marked > 3 {
		t.Errorf("%d of %d slots in a young bag are marked", marked, len(v.Bag))
	}
}

// Another source of marks is one more func on the list, and a mark for a
// thing that isn't in the bag goes nowhere.
func TestAMarkerIsOneFuncOnTheList(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	hold(t, s, id, "t_duck", 1)
	old := markers
	t.Cleanup(func() { markers = old })
	markers = append(slices.Clone(markers), func(v *view) []mark {
		return []mark{{"t_duck", "A set wants it."}, {"t_pearl", "A set wants it."}, {hatKind(hat{3, 3}), "A set wants it."}}
	})
	v, _ := s.view(id)
	if got := marksOn(v, "t_duck"); len(got) != 1 || got[0] != "A set wants it." {
		t.Errorf("the duck: %q", got)
	}
	if marksOn(v, "t_pearl") != nil {
		t.Error("a pearl the pond doesn't hold is in the bag")
	}
}
