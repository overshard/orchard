package main

import (
	"errors"
	"slices"
	"testing"
)

func TestSedgeSellsBaitForTheHook(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	rich(t, s, id, 1000)
	for range 3 {
		if _, err := s.buy(id, shopSedge, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.buy(id, shopSedge, 0); !errors.Is(err, errSoldOut) {
		t.Errorf("a fourth tin today: %v", err)
	}
	v, _ := s.view(id)
	if l := v.Shops[shopSedge].Lines[0]; l.Held != 3 || l.Bought != 3 {
		t.Errorf("worms on the shelf: %+v", l)
	}
	plain := s.fishTableAt(s.now())
	st, err := s.arcadeStart(id, "fish")
	if err != nil {
		t.Fatal(err)
	}
	if st.Bait != "bait_worms" {
		t.Fatalf("the bait on the hook: %q", st.Bait)
	}
	for k, w := range plain {
		f, _ := fishOf(k)
		if want := w * max(1, baits[0].by*float64(one(f.tier >= 1))); st.Fish[k] != want {
			t.Errorf("%s bites %.1f with worms, want %.1f", k, st.Fish[k], want)
		}
	}
	if len(st.Fish) != len(plain) {
		t.Errorf("worms brought %d kinds where there were %d", len(st.Fish), len(plain))
	}
	if v, _ := s.view(id); v.Shops[shopSedge].Lines[0].Held != 2 {
		t.Errorf("a round took %d tins", 3-v.Shops[shopSedge].Lines[0].Held)
	}
}

// A round lands only so many fish and pays only so much a day, so what a
// bait adds to a round's points is worth well under its price.
func TestBaitNeverPaysForItself(t *testing.T) {
	g, _ := arcadeOf("fish")
	const landed = 10
	days := []fishDay{{}, {night: true}, {rain: true, cold: true}, {warm: true, clear: true}, {moon: true, night: true}}
	for _, b := range baits {
		worst := 0.0
		for _, d := range days {
			table := fishTable(d)
			gain := (meanPoints(baited(table, b.kind)) - meanPoints(table)) * landed / float64(g.per)
			worst = max(worst, gain)
		}
		t.Logf("%s at %d adds at most %.1f pebbles to a round", b.name, b.price, worst)
		if worst >= float64(b.price)/2 {
			t.Errorf("%s adds %.1f pebbles a round for a price of %d", b.name, worst, b.price)
		}
	}
}

func meanPoints(table map[string]float64) float64 {
	sum, w := 0.0, 0.0
	for k, x := range table {
		f, _ := fishOf(k)
		sum += x * float64(f.pts)
		w += x
	}
	return sum / w
}

func TestMarshSellsEachFrameOnce(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	rich(t, s, id, 2000)
	b, err := s.buy(id, shopMarsh, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(b.Pond.Frames, "pc_stamp") || b.Pond.Pebbles != 2000-frames[1].price {
		t.Errorf("after buying the stamp: %v, %d", b.Pond.Frames, b.Pond.Pebbles)
	}
	if _, err := s.buy(id, shopMarsh, 1); !errors.Is(err, errSoldOut) {
		t.Errorf("the same frame twice: %v", err)
	}
	if l := b.Pond.Shops[shopMarsh].Lines[1]; l.Bought != 1 {
		t.Errorf("the shelf after: %+v", l)
	}
	for _, x := range b.Pond.Bag {
		if isFrame(x.Kind) {
			t.Errorf("a frame in the bag to sell: %+v", x)
		}
	}
}

func TestTheNewShopsMoveTheirShares(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	rich(t, s, id, 2000)
	for _, shop := range []string{shopSedge, shopMarsh} {
		if _, err := s.buy(id, shop, 0); err != nil {
			t.Fatal(err)
		}
		var n int
		s.r.QueryRow(`SELECT SUM(n) FROM market_flow WHERE key = ?`, "shop:"+shop).Scan(&n)
		if n != 1 {
			t.Errorf("%s's flow is %d after a sale", shop, n)
		}
	}
	for _, k := range []string{"tackle", "frames"} {
		if a, ok := assetOf(k); !ok || a.Kind != kindShare || a.flow == "" {
			t.Errorf("no share for %s: %+v", k, a)
		}
	}
}
