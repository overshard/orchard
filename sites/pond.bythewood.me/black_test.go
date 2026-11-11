package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func nyAt(day string, h, m int) time.Time {
	t, _ := time.ParseInLocation("2006-01-02", day, pondZone)
	return time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, pondZone)
}

func TestTheBackDoorKeepsEveningHours(t *testing.T) {
	cases := []struct {
		h, m   int
		open   bool
		window string
	}{
		{6, 0, false, "2026-11-12 evening"},
		{15, 59, false, "2026-11-12 evening"},
		{16, 0, true, "2026-11-12 evening"},
		{20, 29, true, "2026-11-12 evening"},
		{20, 30, false, "2026-11-13 evening"},
		{23, 0, false, "2026-11-13 evening"},
	}
	for _, c := range cases {
		now := nyAt("2026-11-12", c.h, c.m)
		w, open, edge := blackWindow(now)
		if open != c.open || w != c.window || !edge.After(now) {
			t.Errorf("%02d:%02d: %s open %v until %v", c.h, c.m, w, open, edge)
		}
	}
}

func TestDealsAreTheSameForEveryPondAndHidden(t *testing.T) {
	s, clock := testStore(t)
	_, a := newPond(t, s)
	_, b := newPond(t, s)
	*clock = nyAt("2026-11-10", 17, 30)
	va, _ := s.view(a)
	vb, _ := s.view(b)
	if !va.Black.Open || len(va.Black.Deals) != blackDeals {
		t.Fatalf("at half five: %+v", va.Black)
	}
	ja, _ := json.Marshal(va.Black.Deals)
	jb, _ := json.Marshal(vb.Black.Deals)
	if string(ja) != string(jb) {
		t.Errorf("two ponds see different deals:\n%+v\n%+v", va.Black.Deals, vb.Black.Deals)
	}
	// Another pepper deals something else, so the source alone can't say.
	other := dealsOf([]byte("some other pepper altogether"), "2026-11-10 evening", 100)
	mine := dealsOf(s.pepper, "2026-11-10 evening", 100)
	if fmt.Sprint(other) == fmt.Sprint(mine) {
		t.Error("two peppers dealt the same night")
	}
	raw, _ := json.Marshal(va.Black)
	for _, w := range []string{dealGreat, dealScam, dealFake, `"outcome"`, `"got"`} {
		if strings.Contains(string(raw), w) {
			t.Errorf("the page can read %s before buying: %s", w, raw)
		}
	}
	// Shut, there's nothing on the crate at all.
	*clock = nyAt("2026-11-11", 12, 0)
	v, _ := s.view(a)
	if v.Black.Open || len(v.Black.Deals) != 0 || v.Black.Back != "4pm" || v.Black.Edge != 4*3600 {
		t.Errorf("at noon: %+v", v.Black)
	}
}

func TestADealIsBoughtOnceWithCoin(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET staged = 0, rank_top = 8, gravel_level = 5, garden_level = 5, dye_level = 5 WHERE id = ?`, id)
	*clock = nyAt("2026-11-10", 12, 0)
	if _, err := s.blackBuy(id, 0); !errors.Is(err, errBlackShut) {
		t.Errorf("at noon: %v", err)
	}
	*clock = nyAt("2026-11-10", 17, 10)
	if _, err := s.blackBuy(id, 0); !errors.Is(err, errNoCoin) {
		t.Errorf("with no coin: %v", err)
	}
	v, _ := s.view(id)
	d := v.Black.Deals[0]
	rich(t, s, id, 1_000_000)
	if _, err := s.trade(id, coinKey, min(100, d.Coins+5), false); err != nil {
		t.Fatal(err)
	}
	before, _ := s.view(id)
	got, err := s.blackBuy(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	g := got.Deal.Got
	if g == nil || got.Pond.Black.Coins != before.Black.Coins-d.Coins || got.Pond.Pebbles < before.Pebbles {
		t.Fatalf("bought %+v, coins %d to %d", g, before.Black.Coins, got.Pond.Black.Coins)
	}
	if g.Kind == "peb" {
		if got.Pond.Pebbles != before.Pebbles+1 {
			t.Errorf("a pebble fake paid %d", got.Pond.Pebbles-before.Pebbles)
		}
	} else if h, ok := hatOfKind(g.Kind); ok {
		if !slices.ContainsFunc(got.Pond.Hats, func(x hatView) bool { return x.hat == h && x.Count > 0 }) {
			t.Errorf("no %s in the hats", g.Name)
		}
	} else if got.Pond.held[g.Kind] < g.N {
		t.Errorf("no %d %s in the bag", g.N, g.Kind)
	}
	if got.Pond.Black.Deals[0].Got == nil || got.Pond.Black.Bought != 1 || got.Pond.Black.Steals+got.Pond.Black.Stung > 1 {
		t.Errorf("after buying: %+v", got.Pond.Black)
	}
	if _, err := s.blackBuy(id, 0); !errors.Is(err, errDealt) {
		t.Errorf("the same deal twice: %v", err)
	}
	if _, err := s.blackBuy(id, blackDeals); !errors.Is(err, errNotHeld) {
		t.Errorf("a deal that isn't there: %v", err)
	}
}

// What a sharp eye can see of a deal: whether the name's spelt right, what
// sort of patter it came with, and the price against what the shops ask.
type eye struct {
	misspelt bool
	talk     int
	ratio    float64
}

func eyeOn(d offer, coin float64) eye {
	e := eye{misspelt: d.misspelt, ratio: float64(d.Coins) * coin / float64(d.worth)}
	for i, pool := range patter {
		if slices.Contains(pool, d.Patter) {
			e.talk = i
		}
	}
	return e
}

var blackPlans = []struct {
	name string
	buy  func(e eye) bool
}{
	{"everything", func(eye) bool { return true }},
	{"under worth", func(e eye) bool { return e.ratio < 1 }},
	{"spelt right", func(e eye) bool { return !e.misspelt }},
	{"shifty talk", func(e eye) bool { return e.talk == 0 }},
	{"no pushing", func(e eye) bool { return e.talk != 1 && e.ratio < 2 }},
	{"sharpest eye", func(e eye) bool { return e.ratio < 1 && !e.misspelt && e.talk != 1 }},
	{"shifty, cheap", func(e eye) bool { return e.ratio < 1 && e.talk == 0 && !e.misspelt }},
}

// resaleOf is what a thing sells on for in a day at the going rate, the
// stall's for most things and the market's for a lot of goods.
func resaleOf(kind string, n int) int {
	if kind == "peb" {
		return n
	}
	for _, a := range assets {
		if a.Kind == kindGood && a.Item == kind {
			return sellPays(a.base, n/lot, goodFee)
		}
	}
	w, _ := worthOf(kind)
	return n * w
}

// Over years of windows no way of reading the deals comes out ahead even
// counting what came at full shop price, and buying blind loses heavily.
func TestTheBackDoorCostsInTheLongRun(t *testing.T) {
	const coin = 100.0
	for _, pepper := range []string{"one pepper for the back door", "and a second one to be sure"} {
		day := "2026-01-01"
		paid, worth, resale, n := make([]float64, len(blackPlans)), make([]float64, len(blackPlans)), make([]float64, len(blackPlans)), make([]int, len(blackPlans))
		odds := map[string]int{}
		for range 4000 {
			for _, h := range blackHours {
				for _, d := range dealsOf([]byte(pepper), day+" "+h.tag, coin) {
					odds[d.outcome]++
					e := eyeOn(d, coin)
					for i, p := range blackPlans {
						if p.buy(e) {
							paid[i] += float64(d.Coins) * coin * (1 + coinFee)
							worth[i] += float64(worthIn(d.gotKind, d.gotN))
							resale[i] += float64(resaleOf(d.gotKind, d.gotN))
							n[i]++
						}
					}
				}
			}
			day = dayShift(day, 1)
		}
		t.Logf("outcomes %v", odds)
		for i, p := range blackPlans {
			back, sold := worth[i]/paid[i], resale[i]/paid[i]
			t.Logf("%-13s buys %5d deals, %5.0f pebbles a deal, back %.2f at shop prices and %.2f sold on", p.name, n[i], paid[i]/float64(n[i]), back, sold)
			if i == 0 && back > 0.5 || back > 1.15 || sold > 0.5 {
				t.Errorf("%s gets back %.2f of what it paid, %.2f sold on", p.name, back, sold)
			}
		}
	}
}

// Ninety days of a pond buying every deal it can pay for, with the coin
// bought as it goes. Even with everything it got sold on, it ends behind.
func TestNinetyDaysAtTheBackDoor(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	const purse = 200000
	exec(t, s, `UPDATE ponds SET pebbles = ?, staged = 0, rank_top = 8, gravel_level = 5, garden_level = 5, dye_level = 5 WHERE id = ?`, purse, id)
	day := "2026-11-11"
	for range 90 {
		for _, at := range [][2]int{{16, 10}} {
			*clock = nyAt(day, at[0], at[1])
			lock(t, s)
			v, _ := s.view(id)
			for _, d := range v.Black.Deals {
				if short := d.Coins - v.Black.Coins; short > 0 {
					if short > 100 {
						continue
					}
					if _, err := s.trade(id, coinKey, short, false); errors.Is(err, errCap) || errors.Is(err, errPoor) {
						continue
					} else if err != nil {
						t.Fatal(err)
					}
				}
				if _, err := s.blackBuy(id, d.Slot); err != nil {
					t.Fatal(err)
				}
				v, _ = s.view(id)
			}
		}
		day = dayShift(day, 1)
	}
	v, _ := s.view(id)
	if v.Black.Coins > 0 {
		if _, err := s.trade(id, coinKey, min(100, v.Black.Coins), true); err != nil {
			t.Fatal(err)
		}
	}
	v, _ = s.view(id)
	rows, err := s.r.Query(`SELECT outcome, kind, n FROM black_buys WHERE pond_id = ?`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	sold, shop, odds := 0, 0, map[string]int{}
	for rows.Next() {
		var o, k string
		var n int
		rows.Scan(&o, &k, &n)
		odds[o]++
		if k != "peb" {
			sold += resaleOf(k, n)
			shop += worthIn(k, n)
		}
	}
	spent := purse - v.Pebbles
	t.Logf("%d deals %v for %d pebbles, worth %d at shop prices and %d sold on, so %d behind", v.Black.Bought, odds, spent, shop, sold, spent-sold)
	if v.Black.Bought < 100 || spent <= sold || shop >= spent {
		t.Errorf("%d deals cost %d and sell on for %d", v.Black.Bought, spent, sold)
	}
}
