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
	if _, err := s.build(id, "gravel"); !errors.Is(err, errShort) {
		t.Fatalf("level two with no clay: %v", err)
	}
	if v, _ := s.view(id); v.Pebbles != 250 || v.Bank.Gravel.Mats["clay"] != 10 {
		t.Fatalf("a short build took something, or says no clay: %d %v", v.Pebbles, v.Bank.Gravel.Mats)
	}
	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, 'clay', 1000), (?, 'sticks', 1000)`, id, id); err != nil {
		t.Fatal(err)
	}
	v, err := s.build(id, "gravel")
	if err != nil || v.Pebbles != 12 || v.Bank.Gravel.Level != 2 || v.Bank.Gravel.Rate != 12 || v.held["clay"] != 990 {
		t.Fatalf("%+v %v clay %d", v.Bank.Gravel, err, v.held["clay"])
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
	if err != nil || v.Pebbles != before+taperPaid(600) || len(v.Kitchen.Dishes) != 0 || len(v.Kitchen.Cookbook) != 1 {
		t.Fatalf("%+v %v", v.Kitchen, err)
	}
}

// Big Bean always buys, and what he pays comes down the day's taper with
// whatever else was sold that day, then starts again in full the next day.
func TestBigBeanBuysDownTheTaper(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	soup, jam := dishOf(0, 0).kind(), dishOf(6, 6).kind()
	stock(t, s, id, soup, 40)
	stock(t, s, id, jam, 3)
	stock(t, s, id, "t_statue", 2)
	v, _ := s.view(id)
	start, face := v.Pebbles, 0
	for range 40 {
		var err error
		if v, err = s.sellDish(id, soup); err != nil {
			t.Fatal(err)
		}
		face += dishWorth[dishOf(0, 0).tier]
	}
	for range 2 {
		var err error
		if v, err = s.sellDish(id, jam); err != nil {
			t.Fatal(err)
		}
		face += dishWorth[4]
	}
	st, _ := trinketOf("t_statue")
	if v, _ = s.sellTrinket(id, "t_statue"); v == nil {
		t.Fatal("Moth wouldn't buy a statue")
	}
	face += hatRate[st.tier] / 4
	if v.Pebbles-start != taperPaid(face) || v.Taper.Sold != face || v.Taper.Paid != taperPaid(face) {
		t.Fatalf("%d sold at face paid %d, the taper says %d and %+v", face, v.Pebbles-start, taperPaid(face), v.Taper)
	}
	if v.held[soup] != 0 || v.held[jam] != 1 {
		t.Fatalf("%d soup and %d jam left", v.held[soup], v.held[jam])
	}
	*clock = clock.Add(24 * time.Hour)
	if v, _ = s.view(id); v.Taper.Sold != 0 {
		t.Fatalf("the next day starts at %d sold", v.Taper.Sold)
	}
	before := v.Pebbles
	if v, err := s.sellDish(id, jam); err != nil || v.Pebbles-before != taperPaid(dishWorth[4]) {
		t.Fatalf("a jam the next day paid %d, %v", v.Pebbles-before, err)
	}
}

func TestTheTaperSteps(t *testing.T) {
	if taperPaid(taperFull) != taperFull || taperPaid(taperHalf) != taperFull+(taperHalf-taperFull)/2 {
		t.Fatalf("the steps pay %d and %d", taperPaid(taperFull), taperPaid(taperHalf))
	}
	for face := 1; face < 5000; face++ {
		if step := taperPaid(face) - taperPaid(face-1); step < 0 || step > 1 {
			t.Fatalf("the pebble at %d pays %d", face, step)
		}
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

func TestAGardenerSpeedsThePlots(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 150)
	if _, err := s.build(id, "garden"); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(2 * 24 * time.Hour)
	v, _ := s.view(id)
	slow := v.Bank.Garden.Plots[0].ReadyIn
	f := v.Frogs[0].ID
	if _, err := s.setJob(id, f, "garden", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`INSERT INTO job_xp (pond_id, frog_id, job, xp) VALUES (?, ?, 'garden', ?)`, id, f, xpFor[maxLevel]); err != nil {
		t.Fatal(err)
	}
	v, _ = s.view(id)
	g := v.Bank.Garden
	if g.Speed != 2 || g.Gardener == "" || (slow > 0 && g.Plots[0].ReadyIn >= slow) {
		t.Errorf("a 99 gardener: speed %v by %q, ready in %d against %d", g.Speed, g.Gardener, g.Plots[0].ReadyIn, slow)
	}
}
