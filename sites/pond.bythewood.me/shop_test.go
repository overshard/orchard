package main

import (
	"errors"
	"testing"
	"time"
)

// Buying what a dish needs from Brindle and selling him the dish never makes
// pebbles, sale or not.
func TestLarderCantBeFarmed(t *testing.T) {
	for a := range ingredients {
		for b := a; b < len(ingredients); b++ {
			cost := salePrice(larderPrice[ingredients[a].tier]) + salePrice(larderPrice[ingredients[b].tier])
			if d := dishOf(a, b); dishWorth[d.tier] > cost {
				t.Errorf("%s costs %d and sells for %d", d.name, cost, dishWorth[d.tier])
			}
		}
	}
}

func TestStockIsTheSameForEverybody(t *testing.T) {
	for _, day := range []string{"2026-11-10", "2026-11-11", "2027-03-02"} {
		a, b := wrenStock(day), wrenStock(day)
		for i := range a {
			if *a[i].Hat != *b[i].Hat || a[i].Price != b[i].Price {
				t.Fatalf("wren on %s dealt twice differently", day)
			}
		}
		seen := map[hat]bool{}
		for _, l := range a {
			if seen[*l.Hat] {
				t.Errorf("wren has %s twice on %s", l.Name, day)
			}
			seen[*l.Hat] = true
			if l.Price == 0 {
				t.Errorf("a free hat on %s", day)
			}
		}
		if l := larderStock(day, false); l[0].Kind == l[1].Kind {
			t.Errorf("brindle has %s twice on %s", l[0].Kind, day)
		}
	}
}

func TestMossRestocksThroughTheDay(t *testing.T) {
	start := time.Date(2026, 11, 10, 0, 0, 30, 0, pondZone)
	keys := map[string]bool{}
	var last time.Time
	for m := 0; m < 24*60; m += 5 {
		now := start.Add(time.Duration(m) * time.Minute)
		key, next := mossRestock(now)
		keys[key] = true
		if !next.After(now) {
			t.Fatalf("the next restock at %v is not after %v", next, now)
		}
		last = next
	}
	if len(keys) != mossRestocks+1 {
		t.Errorf("%d stocks in a day, want %d", len(keys), mossRestocks+1)
	}
	if want := start.Truncate(time.Hour).AddDate(0, 0, 1); !last.Equal(want) {
		t.Errorf("the last restock of the day is %v, want midnight", last)
	}
}

func TestBuyingFromWren(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	v, err := s.view(id)
	if err != nil {
		t.Fatal(err)
	}
	line := v.Shops[shopWren].Lines[0]
	if _, err := s.buy(id, shopWren, 0); !errors.Is(err, errPoor) {
		t.Fatalf("bought a %d pebble hat with 20: %v", line.Price, err)
	}
	give(t, s, id, 1000)
	b, err := s.buy(id, shopWren, 0)
	if err != nil {
		t.Fatal(err)
	}
	if b.Pond.Pebbles != 1000-line.Price || b.Line.Bought != 1 {
		t.Errorf("after buying %+v the purse is %d", b.Line, b.Pond.Pebbles)
	}
	found := false
	for _, h := range b.Pond.Hats {
		if h.hat == *line.Hat && h.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("%s isn't in the collection", line.Name)
	}
	if _, err := s.buy(id, shopWren, 0); !errors.Is(err, errSoldOut) {
		t.Errorf("bought the one hat twice: %v", err)
	}
	if _, err := s.buy(id, "nobody", 0); !errors.Is(err, errNotHeld) {
		t.Errorf("bought from a shop that isn't there: %v", err)
	}
}

func TestLarderLimitAndStamps(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 10000)
	for range larderEach {
		if _, err := s.buy(id, shopBrindle, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.buy(id, shopBrindle, 0); !errors.Is(err, errSoldOut) {
		t.Errorf("bought past the larder's limit: %v", err)
	}
	// Tomorrow is new stock.
	*clock = clock.Add(24 * time.Hour)
	if _, err := s.buy(id, shopBrindle, 0); err != nil {
		t.Errorf("tomorrow's larder: %v", err)
	}

	parent := lookOf(1207)
	stamped := 0
	for range 2000 {
		if m := breed(parent, randomSeed(), "", "stamp_mask"); m.Pattern == "MASK" {
			stamped++
		}
	}
	if stamped < 1200 || stamped > 1600 {
		t.Errorf("a mask stamp took %d times in 2000", stamped)
	}
}

func TestFennSellsEachOnce(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 5000)
	b, err := s.buy(id, shopFenn, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Pond.Furniture) != 1 || b.Pond.Furniture[0] != "f_lantern" {
		t.Errorf("furniture after buying the lantern: %v", b.Pond.Furniture)
	}
	for _, it := range b.Pond.Items {
		if it.Kind == "f_lantern" {
			t.Errorf("the lantern is in the purse")
		}
	}
	*clock = clock.Add(48 * time.Hour)
	if _, err := s.buy(id, shopFenn, 4); !errors.Is(err, errSoldOut) {
		t.Errorf("bought a second lantern: %v", err)
	}
}
