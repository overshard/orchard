package main

import (
	"errors"
	"testing"
	"time"
)

func give(t *testing.T, s *store, id int64, pebbles int) {
	t.Helper()
	if _, err := s.w.Exec(`UPDATE ponds SET pebbles = ? WHERE id = ?`, pebbles, id); err != nil {
		t.Fatal(err)
	}
}

func TestGravelBarFillsAndStops(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.build(id, "gravel"); !errors.Is(err, errPoor) {
		t.Fatalf("built with 20 pebbles: %v", err)
	}
	give(t, s, id, 100)
	if _, err := s.build(id, "gravel"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.collect(id); !errors.Is(err, errNotYet) {
		t.Errorf("collected from an empty bar: %v", err)
	}
	*clock = clock.Add(95 * time.Minute)
	got, err := s.collect(id)
	if err != nil || got.Pebbles != 9 || got.Pond.Pebbles != 9 {
		t.Fatalf("an hour and a half at 6 an hour: %+v %v", got, err)
	}
	// The five minutes past the ninth pebble carry over.
	*clock = clock.Add(5 * time.Minute)
	if got, _ := s.collect(id); got.Pebbles != 1 {
		t.Errorf("the carried minutes made %d", got.Pebbles)
	}
	*clock = clock.Add(3 * 24 * time.Hour)
	if got, _ := s.collect(id); got.Pebbles != 48 {
		t.Errorf("a full bar held %d, want 48", got.Pebbles)
	}
}

func TestUpgradePaysOutFirst(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 100)
	if _, err := s.build(id, "gravel"); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(2 * time.Hour)
	give(t, s, id, 250)
	v, err := s.build(id, "gravel")
	if err != nil || v.Pebbles != 12 || v.Bank.Gravel.Level != 2 || v.Bank.Gravel.Rate != 12 {
		t.Fatalf("%+v %v", v.Bank.Gravel, err)
	}
	for range buildLevels {
		give(t, s, id, 10000)
		_, err = s.build(id, "gravel")
	}
	if !errors.Is(err, errMaxed) {
		t.Errorf("a sixth level: %v", err)
	}
}

func TestGardenGrowsPicksAndCooks(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 150)
	if err := s.windGrowth(); err != nil {
		t.Fatal(err)
	}
	v, err := s.build(id, "garden")
	if err != nil || len(v.Bank.Garden.Plots) != 2 {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err := s.pick(id); !errors.Is(err, errNotYet) {
		t.Errorf("picked a plot that was just sown: %v", err)
	}
	if _, err := s.water(id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.water(id); !errors.Is(err, errNotYet) {
		t.Errorf("watered twice: %v", err)
	}
	// Watered plots save an hour and a half of the three.
	*clock = clock.Add(90 * time.Minute)
	got, err := s.pick(id)
	if err != nil || len(got.Got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, k := range got.Got {
		if _, ok := ingredientOf(k); !ok {
			t.Errorf("picked %q", k)
		}
	}
	held := 0
	for _, in := range got.Pond.Kitchen.Ingredients {
		held += in.Count
	}
	if held != 2 {
		t.Errorf("the kitchen holds %d", held)
	}
	for _, it := range got.Pond.Items {
		if _, food := ingredientOf(it.Kind); food {
			t.Errorf("%s is in the purse too", it.Kind)
		}
	}

	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, 'firefly', 1), (?, 'morel', 1)
		ON CONFLICT DO UPDATE SET count = count + 1`, id, id); err != nil {
		t.Fatal(err)
	}
	c, err := s.cook(id, "morel", "firefly")
	if err != nil || c.Dish.Name != "Midnight broth" || c.Tier != 4 || !c.New {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := s.cook(id, "morel", "firefly"); !errors.Is(err, errNotHeld) {
		t.Errorf("cooked with nothing left: %v", err)
	}
	before := c.Pond.Pebbles
	v, err = s.sellDish(id, c.Dish.Kind)
	if err != nil || v.Pebbles != before+600 || len(v.Kitchen.Dishes) != 0 || len(v.Kitchen.Cookbook) != 1 {
		t.Fatalf("%+v %v", v.Kitchen, err)
	}
}

func TestFrostStopsTheGarden(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 150)
	if _, err := s.build(id, "garden"); err != nil {
		t.Fatal(err)
	}
	s.day.pinned.Store(true)
	s.day.last.Store(&reading{At: *clock, Temp: 28, HasTemp: true, Cond: "Clear"})
	if err := s.windGrowth(); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(6 * time.Hour)
	if _, err := s.pick(id); !errors.Is(err, errNotYet) {
		t.Errorf("grew in a frost: %v", err)
	}
	if _, err := s.water(id); !errors.Is(err, errNotYet) {
		t.Errorf("watered in a frost: %v", err)
	}
	v, _ := s.view(id)
	if v.Bank.Garden.Why != "frost" || v.Bank.Garden.Plots[0].ReadyIn != -1 {
		t.Errorf("%+v", v.Bank.Garden)
	}
}

func TestEveryDishHasAKindAndAName(t *testing.T) {
	seen := map[string]bool{}
	for a := range ingredients {
		for b := range ingredients {
			d := dishOf(a, b)
			if d.name == "" || d.tier < 0 || d.tier >= tierCount {
				t.Errorf("%d %d: %+v", a, b, d)
			}
			back, ok := dishOfKind(d.kind())
			if !ok || back.name != d.name {
				t.Errorf("%s doesn't read back", d.kind())
			}
			seen[d.kind()] = true
		}
	}
	if len(seen) != dishCount {
		t.Errorf("%d dishes, want %d", len(seen), dishCount)
	}
	for _, bad := range []string{"dish_1_0", "dish_0_99", "dish_01_2", "dish_-1_2", "agate"} {
		if _, ok := dishOfKind(bad); ok {
			t.Errorf("%q read as a dish", bad)
		}
	}
}

func TestSomethingAlwaysGrows(t *testing.T) {
	for m := 1; m <= 12; m++ {
		now := time.Date(2026, time.Month(m), 15, 12, 0, 0, 0, pondZone)
		w := seasonWeights(now, false)
		if w[0] == 0 {
			t.Errorf("no duckweed in month %d", m)
		}
		if (m == 6 || m == 7) != (w[6] > 0) {
			t.Errorf("fireflies in month %d: %d", m, w[6])
		}
	}
}
