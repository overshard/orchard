package main

import (
	"errors"
	"testing"
	"time"
)

func pinDay(s *store, r reading) {
	r.At = s.now()
	s.day.pinned.Store(true)
	s.day.last.Store(&r)
}

func priceOf(t *testing.T, s *store, id int64, key string) firmView {
	t.Helper()
	v, err := s.view(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range v.Market.Firms {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("no firm %s", key)
	return firmView{}
}

func TestMarketFollowsTheRealDay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if f := priceOf(t, s, id, "sp500"); f.Price != 100 || len(f.Days) != 0 {
		t.Fatalf("launch: %+v", f)
	}

	// Before the bell dash shows futures, which don't move the stocks, but
	// Glowworm Coin trades all the time.
	pinDay(s, reading{Session: "pre", Markets: map[string]float64{"sp500": 1, "bitcoin": -2}})
	if err := s.lockMarket(); err != nil {
		t.Fatal(err)
	}
	if f := priceOf(t, s, id, "sp500"); f.Price != 100 {
		t.Errorf("pre-market moved the S&P: %+v", f)
	}
	if f := priceOf(t, s, id, "bitcoin"); f.Price != 94 {
		t.Errorf("bitcoin at -2%% x3 is %d, want 94", f.Price)
	}

	// The last reading of the day is its close.
	pinDay(s, reading{Session: "regular", Markets: map[string]float64{"sp500": 1}})
	s.lockMarket()
	pinDay(s, reading{Session: "regular", Markets: map[string]float64{"sp500": 2}})
	s.lockMarket()
	if f := priceOf(t, s, id, "sp500"); f.Price != 106 || f.Prev != 100 {
		t.Errorf("after the day: %+v", f)
	}

	// The next day builds on that close, and a day dash is away stays put.
	*clock = clock.Add(24 * time.Hour)
	if f := priceOf(t, s, id, "sp500"); f.Price != 106 || f.Prev != 106 {
		t.Errorf("a quiet next day: %+v", f)
	}
	pinDay(s, reading{Session: "regular", Markets: map[string]float64{"sp500": -10}})
	s.lockMarket()
	if f := priceOf(t, s, id, "sp500"); f.Price != 74 || len(f.Days) != 2 {
		t.Errorf("a crash day: %+v", f)
	}
}

func TestSharesCostAFeeEachWay(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.w.Exec(`UPDATE ponds SET pebbles = 1000 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	v, err := s.buyShares(id, "gold", 2)
	if err != nil || v.Pebbles != 1000-204 {
		t.Fatalf("bought two at 100 and have %d: %v", v.Pebbles, err)
	}
	if _, err := s.buyShares(id, "gold", 1); !errors.Is(err, errCap) {
		t.Errorf("went past the cap of %d: %v", v.Market.Cap, err)
	}
	if _, err := s.buyShares(id, "unicorns", 1); !errors.Is(err, errFirm) {
		t.Errorf("bought a made up business: %v", err)
	}
	if _, err := s.sellShares(id, "gold", 3); !errors.Is(err, errNotHeld) {
		t.Errorf("sold more than held: %v", err)
	}
	v, err = s.sellShares(id, "gold", 1)
	if err != nil || v.Pebbles != 1000-204+98 {
		t.Fatalf("sold one back and have %d: %v", v.Pebbles, err)
	}
	f := priceOf(t, s, id, "gold")
	if f.Held != 1 || f.Cost != 102 || f.Worth >= f.Cost {
		t.Errorf("what's left: %+v", f)
	}
	if _, err := s.buyShares(id, "nasdaq", 0); !errors.Is(err, errCount) {
		t.Errorf("bought none: %v", err)
	}
}

func TestMarketTalk(t *testing.T) {
	if marketMood(marketView{Worth: 120, WorthPrev: 100}) == "" || marketMood(marketView{Worth: 80, WorthPrev: 100}) == "" {
		t.Error("a big day said nothing")
	}
	if marketMood(marketView{Worth: 104, WorthPrev: 100}) != "" || marketMood(marketView{Worth: 30, WorthPrev: 20}) != "" {
		t.Error("a small day or a small purse said something")
	}
	up := firmView{business: businesses[0], Price: 110, Prev: 100}
	if firmTalk(up, 1) == "" || firmTalk(firmView{business: businesses[0], Price: 101, Prev: 100}, 1) != "" {
		t.Error("residents talk about the wrong days")
	}
	for _, l := range []string{firmTalk(up, 0), firmTalk(up, 1), marketMood(marketView{Worth: 80, WorthPrev: 100})} {
		if len(l) > 110 {
			t.Errorf("too long: %q", l)
		}
	}
}
