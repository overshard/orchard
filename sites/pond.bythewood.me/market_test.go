package main

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

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

func lock(t *testing.T, s *store) {
	t.Helper()
	if err := s.lockMarket(); err != nil {
		t.Fatal(err)
	}
}

func rich(t *testing.T, s *store, id int64, n int) {
	t.Helper()
	exec(t, s, `UPDATE ponds SET pebbles = ? WHERE id = ?`, n, id)
}

func TestMarketIgnoresDash(t *testing.T) {
	a, ca := testStore(t)
	b, cb := testStore(t)
	_, ia := newPond(t, a)
	_, ib := newPond(t, b)
	pinDay(a, reading{Session: "regular", Markets: map[string]float64{"sp500": 9, "bitcoin": -20, "gold": 4}})
	for range 3 {
		lock(t, a)
		lock(t, b)
		*ca = ca.Add(5 * time.Hour)
		*cb = cb.Add(5 * time.Hour)
	}
	va, _ := a.view(ia)
	vb, _ := b.view(ib)
	for i, f := range va.Market.Firms {
		if g := vb.Market.Firms[i]; f.Price != g.Price || len(f.Days) != len(g.Days) {
			t.Errorf("%s is %d with dash's markets and %d without", f.Key, f.Price, g.Price)
		}
	}
}

func pinDay(s *store, r reading) {
	r.At = s.now()
	s.day.pinned.Store(true)
	s.day.last.Store(&r)
}

// A pond holding shares off the real markets keeps them, and the first price
// worked out here carries on from the last real one.
func TestPricesCarryOnFromTheRealClose(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	today, yesterday := dayOf(*clock), dayOf(clock.AddDate(0, 0, -1))
	exec(t, s, `INSERT INTO market_days (day, key, close) VALUES (?, 'gold', 131), (?, 'gold', 142), (?, 'bitcoin', 88)`, yesterday, today, yesterday)
	exec(t, s, `INSERT INTO shares (pond_id, key, count, cost) VALUES (?, 'gold', 3, 390), (?, 'bitcoin', 2, 180)`, id, id)
	lock(t, s)
	g := priceOf(t, s, id, "gold")
	if g.Prev != 131 || math.Abs(float64(g.Price-142)) > 1 || g.Held != 3 || g.Cost != 390 {
		t.Errorf("gold after the change: %+v", g)
	}
	c := priceOf(t, s, id, coinKey)
	if c.Prev != 88 || math.Abs(float64(c.Price-88)) > 88*0.25 || c.Held != 2 {
		t.Errorf("the coin after the change: %+v", c)
	}
	// Yesterday's real close is never worked out again.
	*clock = clock.Add(24 * time.Hour)
	lock(t, s)
	var close float64
	if err := s.r.QueryRow(`SELECT close FROM market_days WHERE key = 'bitcoin' AND day = ?`, yesterday).Scan(&close); err != nil || close != 88 {
		t.Errorf("the real close moved to %v: %v", close, err)
	}
	if f := priceOf(t, s, id, "guild"); f.Days[0] != 40 || len(f.Days) != 2 {
		t.Errorf("a new business: %+v", f)
	}
}

func flat(int) (float64, float64) { return 0, 1 }

func TestADayMovesALittle(t *testing.T) {
	day := "2026-11-10"
	start, _ := dayBounds(day)
	heavy, none := flowBook{}, flowBook{}
	for _, a := range assets {
		if a.flow != "" {
			heavy[a.flow] = map[string]*[24]int{day: {0: 100000}}
		}
	}
	rnd := rand.New(rand.NewPCG(1, 2))
	wild := func(int) (float64, float64) { return rnd.NormFloat64() * 4, rnd.Float64() }
	for _, a := range assets {
		t2 := tunes[a.Kind]
		if t2.cap == 0 {
			continue
		}
		for _, b := range []flowBook{heavy, none} {
			for _, at := range []time.Time{start.Add(3 * time.Hour), start.Add(23 * time.Hour)} {
				p, _ := priceAt(a, wild, b, day, a.base, at, false)
				if m := math.Abs(math.Log(p / a.base)); m > t2.cap+1e-9 {
					t.Errorf("%s moved %.1f%% in a day", a.Key, 100*m)
				}
			}
		}
	}
	// A quiet market and an even walk leave a share where it opened.
	if p, _ := priceAt(assets[1], flat, none, day, 100, start.Add(12*time.Hour), false); math.Abs(p-100) > 0.01 {
		t.Errorf("a still day moved a share to %.2f", p)
	}
}

func TestIndustriesMoveWithWork(t *testing.T) {
	day := "2026-11-10"
	start, _ := dayBounds(day)
	busy := flowBook{"job:bugs": {}, "g_bugs": {}}
	for i := 1; i <= flowWeek; i++ {
		busy["job:bugs"][dayShift(day, -i)] = &[24]int{12: 200}
		busy["g_bugs"][dayShift(day, -i)] = &[24]int{12: 300}
	}
	usual := flowBook{"job:bugs": {}, "g_bugs": {}}
	for d, h := range busy["job:bugs"] {
		usual["job:bugs"][d], usual["g_bugs"][d] = h, busy["g_bugs"][d]
	}
	busy["job:bugs"][day] = &[24]int{6: 600}
	busy["g_bugs"][day] = &[24]int{6: 900}
	usual["job:bugs"][day] = &[24]int{12: 200}
	usual["g_bugs"][day] = &[24]int{12: 300}
	guild, _ := assetOf("guild")
	bugs, _ := assetOf("g_bugs")
	end := start.Add(23 * time.Hour)
	up, divUp := priceAt(guild, flat, busy, day, 40, end, true)
	same, divSame := priceAt(guild, flat, usual, day, 40, end, true)
	if up <= same*1.02 || divUp <= divSame {
		t.Errorf("three times the bugs caught took the guild to %.2f and %.3f, against %.2f and %.3f", up, divUp, same, divSame)
	}
	glut, _ := priceAt(bugs, flat, busy, day, 60, end, true)
	norm, _ := priceAt(bugs, flat, usual, day, 60, end, true)
	if glut >= norm*0.97 {
		t.Errorf("a glut of bugs left them at %.2f against %.2f", glut, norm)
	}
	// Lilypad moves with everything, so a busy guild moves it a little.
	lily, _ := priceAt(assets[0], flat, busy, day, 100, end, true)
	if lily <= 100 || lily >= up/40*100 {
		t.Errorf("Lilypad went to %.2f with the guild at %.2f", lily, up)
	}
	// By morning the day so far is weighed against the same hours of the week.
	if _, d := priceAt(guild, flat, usual, day, 40, start.Add(13*time.Hour), false); math.Abs(d-divYield*40) > 1e-4 {
		t.Errorf("a usual day pays %.4f a share, want %.4f", d, divYield*40)
	}
}

func TestSharesCostAFeeEachWay(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	rich(t, s, id, 1000)
	v, err := s.trade(id, "gold", 2, false)
	if err != nil || v.Pebbles != 1000-204 {
		t.Fatalf("bought two at 100 and have %d: %v", v.Pebbles, err)
	}
	if _, err := s.trade(id, "gold", 3, false); !errors.Is(err, errCap) {
		t.Errorf("went past the cap of %d: %v", v.Market.Cap, err)
	}
	if _, err := s.trade(id, "unicorns", 1, false); !errors.Is(err, errFirm) {
		t.Errorf("bought a made up business: %v", err)
	}
	if _, err := s.trade(id, "gold", 3, true); !errors.Is(err, errNotHeld) {
		t.Errorf("sold more than held: %v", err)
	}
	v, err = s.trade(id, "gold", 1, true)
	if err != nil || v.Pebbles != 1000-204+98 {
		t.Fatalf("sold one back and have %d: %v", v.Pebbles, err)
	}
	f := priceOf(t, s, id, "gold")
	if f.Held != 1 || f.Cost != 102 || f.Worth >= f.Cost {
		t.Errorf("what's left: %+v", f)
	}
	if _, err := s.trade(id, "nasdaq", 0, false); !errors.Is(err, errCount) {
		t.Errorf("bought none: %v", err)
	}
}

func TestTheCapGrowsWithThePond(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	small := priceOf(t, s, id, "gold")
	v, _ := s.view(id)
	exec(t, s, `UPDATE ponds SET gravel_level = 5, garden_level = 5, dye_level = 5, rank_top = 8 WHERE id = ?`, id)
	w, _ := s.view(id)
	if v.Market.Cap != 500 || w.Market.Cap != 7500 {
		t.Errorf("the cap runs from %d to %d", v.Market.Cap, w.Market.Cap)
	}
	rich(t, s, id, 10000)
	if _, err := s.trade(id, "gold", 70, false); err != nil {
		t.Errorf("a grown pond can't buy %d of gold: %v", 70*small.Buy, err)
	}
}

func TestGoodsTradeAsItems(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	rich(t, s, id, 1000)
	f := priceOf(t, s, id, "g_sticks")
	v, err := s.trade(id, "g_sticks", 2, false)
	if err != nil || v.Pebbles != 1000-buyCost(45, 2, goodFee) {
		t.Fatalf("bought two lots of sticks and have %d: %v", v.Pebbles, err)
	}
	if g := priceOf(t, s, id, "g_sticks"); g.Held != 2*lot || g.Cost != 0 || v.Market.In != 0 {
		t.Errorf("the sticks went in the bag as %+v, with %d in shares", g, v.Market.In)
	}
	before := v.Pebbles
	v, err = s.trade(id, "g_sticks", 1, true)
	if err != nil || v.Pebbles-before != f.Sell || f.Sell >= f.Buy || v.Market.Room != marketRoom-f.Sell {
		t.Errorf("sold a lot back for %d with %d room left, a lot is %d to buy and %d to sell: %v", v.Pebbles-before, v.Market.Room, f.Buy, f.Sell, err)
	}
	if _, err := s.trade(id, "g_clay", 1, true); !errors.Is(err, errNoLot) {
		t.Errorf("sold clay it never had: %v", err)
	}
	// Jobs bring goods in without end, so the market only pays its room a day.
	exec(t, s, `INSERT INTO items (pond_id, kind, count) VALUES (?, 'clay', 500)`, id)
	paid := 0
	for range 50 {
		before := v.Pebbles
		v, err = s.trade(id, "g_clay", 1, true)
		if errors.Is(err, errMarketFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		paid += v.Pebbles - before
	}
	if paid+f.Sell > marketRoom || paid+f.Sell < marketRoom-60 {
		t.Errorf("a day of selling clay paid %d, against a room of %d", paid+f.Sell, marketRoom)
	}
}

// Buying a good can't come out ahead anywhere else. Two of it at the lowest
// price beat any dish made only of goods, and a lot at its highest still
// sells for less than the larder asks.
func TestGoodsCantMint(t *testing.T) {
	for _, a := range assets {
		if a.Kind != kindGood {
			continue
		}
		ask := float64(buyCost(a.base*goodLow, 1, goodFee)) / lot
		bid := float64(sellPays(a.base*goodHigh, 1, goodFee)) / lot
		for _, b := range assets {
			if b.Kind != kindGood {
				continue
			}
			ia, ok1 := ingredientOf(a.Item)
			ib, ok2 := ingredientOf(b.Item)
			if !ok1 || !ok2 {
				continue
			}
			other := float64(buyCost(b.base*goodLow, 1, goodFee)) / lot
			if d := dishOf(ia, ib); float64(dishWorth[d.tier]) >= ask+other {
				t.Errorf("%s from %s and %s sells for %d, and they cost %.1f", d.name, a.Item, b.Item, dishWorth[d.tier], ask+other)
			}
		}
		if p, ok := shopFloor(a.Item); ok && bid >= float64(p) {
			t.Errorf("%s sells to the market for %.1f and a shop has it for %d", a.Item, bid, p)
		}
		if w, ok := worthOf(a.Item); ok && ask <= float64(w) {
			t.Errorf("%s costs %.1f and the stall rates it at %d", a.Item, ask, w)
		}
	}
}

func TestDividendsArePaidOnceADay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	rich(t, s, id, 1000)
	exec(t, s, `UPDATE ponds SET staged = 0 WHERE id = ?`, id)
	if _, err := s.trade(id, "guild", 10, false); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.view(id); v.Market.Due != 0 {
		t.Errorf("shares bought today have %d waiting", v.Market.Due)
	}
	today := dayOf(*clock)
	exec(t, s, `INSERT INTO market_days (day, key, close, open, div) VALUES (?, 'guild', 40, 40, 0.15)
		ON CONFLICT (key, day) DO UPDATE SET div = 0.15`, today)
	*clock = clock.Add(24 * time.Hour)
	tomorrow := dayOf(*clock)
	exec(t, s, `INSERT INTO market_days (day, key, close, open, div) VALUES (?, 'guild', 40, 40, 0.15)`, tomorrow)
	v, _ := s.view(id)
	if v.Market.Due != 1 || !hasTodo(v, "dividend") {
		t.Errorf("a day of ten guild shares has %d waiting", v.Market.Due)
	}
	paid, err := s.collectDividends(id)
	if err != nil || paid.Pebbles != 1 {
		t.Fatalf("collected %d: %v", paid.Pebbles, err)
	}
	if paid, _ = s.collectDividends(id); paid.Pebbles != 0 || hasTodo(paid.Pond, "dividend") {
		t.Errorf("collected twice in a day, %d the second time", paid.Pebbles)
	}
	// The half left over waits, and a week away is all paid on coming back.
	for i := 1; i <= 7; i++ {
		exec(t, s, `INSERT INTO market_days (day, key, close, open, div) VALUES (?, 'guild', 40, 40, 0.15)`, dayOf(clock.AddDate(0, 0, i)))
	}
	*clock = clock.Add(7 * 24 * time.Hour)
	if paid, _ = s.collectDividends(id); paid.Pebbles != 11 || paid.Pond.Market.Earned != 12 {
		t.Errorf("a week away paid %d, want 11, and %d in all", paid.Pebbles, paid.Pond.Market.Earned)
	}
}

func hasTodo(v *view, kind string) bool {
	for _, d := range todoOf(v) {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

// The coin swings far more than a share and holding it is worth about what
// it cost, so the fee to get in and out makes it a gamble and not a plan.
func TestTheCoinIsAGamble(t *testing.T) {
	coin, _ := assetOf(coinKey)
	day := "2026-11-10"
	rnd := rand.New(rand.NewPCG(7, 9))
	d := func(int) (float64, float64) { return rnd.NormFloat64(), rnd.Float64() }
	const paths, days = 4000, 30
	sum, moves, spikes, crashes := 0.0, 0.0, 0, 0
	for range paths {
		p := 100.0
		for range days {
			q, _ := priceAt(coin, d, nil, day, p, time.Time{}, true)
			switch r := q / p; {
			case r > 1.15:
				spikes++
			case r < 0.8:
				crashes++
			}
			moves += math.Abs(math.Log(q / p))
			p = q
		}
		sum += p / 100
	}
	mean, daily := sum/paths, moves/(paths*days)
	t.Logf("a month of the coin is worth %.3f of what it cost, moves %.1f%% a day, spikes one day in %.0f and crashes one in %.0f",
		mean, 100*daily, float64(paths*days)/float64(spikes), float64(paths*days)/float64(crashes))
	if math.Abs(mean-1) > 0.03 {
		t.Errorf("a month of the coin comes to %.3f", mean)
	}
	if daily < 0.04 || spikes == 0 || crashes == 0 {
		t.Errorf("the coin moves %.1f%% a day with %d spikes and %d crashes", 100*daily, spikes, crashes)
	}
}

// A strategy is one pond's way of playing the market for a day.
type strategy struct {
	name string
	day  func(t *testing.T, s *store, id int64, v *view, day int)
}

func buyAll(t *testing.T, s *store, id int64, v *view, keys []string) {
	t.Helper()
	each := (v.Market.Cap - v.Market.In) / len(keys)
	for _, k := range keys {
		f := firmOf(v, k)
		if n := min(100, each/max(1, f.Buy)); n > 0 {
			if _, err := s.trade(id, k, n, false); err != nil && !errors.Is(err, errCap) && !errors.Is(err, errPoor) {
				t.Fatal(err)
			}
		}
	}
}

func sellAll(t *testing.T, s *store, id int64, v *view) {
	t.Helper()
	for _, f := range v.Market.Firms {
		n := f.Held
		if f.Kind == kindGood {
			n = f.Held / lot
		}
		if n > 0 {
			if _, err := s.trade(id, f.Key, min(n, 100), true); err != nil && !errors.Is(err, errMarketFull) {
				t.Fatal(err)
			}
		}
	}
}

func firmOf(v *view, key string) firmView {
	for _, f := range v.Market.Firms {
		if f.Key == key {
			return f
		}
	}
	return firmView{}
}

func shareKeys() []string {
	var out []string
	for _, a := range assets {
		if a.Kind == kindShare {
			out = append(out, a.Key)
		}
	}
	return out
}

var strategies = []strategy{
	{"hold a spread", func(t *testing.T, s *store, id int64, v *view, day int) {
		if day == 0 {
			buyAll(t, s, id, v, shareKeys())
		}
	}},
	{"buy the dip", func(t *testing.T, s *store, id int64, v *view, day int) {
		for _, f := range v.Market.Firms {
			if f.Kind == kindGood {
				continue
			}
			if f.Held > 0 && f.Worth > f.Cost*21/20 {
				if _, err := s.trade(id, f.Key, min(f.Held, 100), true); err != nil {
					t.Fatal(err)
				}
			} else if f.Price*50 < f.Prev*49 {
				buyAll(t, s, id, v, []string{f.Key})
				v, _ = s.view(id)
			}
		}
	}},
	{"churn goods", func(t *testing.T, s *store, id int64, v *view, day int) {
		for _, f := range v.Market.Firms {
			if f.Kind != kindGood {
				continue
			}
			if f.Held >= lot && float64(f.Price) > f.base {
				if _, err := s.trade(id, f.Key, f.Held/lot, true); err != nil && !errors.Is(err, errMarketFull) {
					t.Fatal(err)
				}
			} else if float64(f.Price) < f.base*0.95 && f.Held < 5*lot {
				if _, err := s.trade(id, f.Key, 1, false); err != nil && !errors.Is(err, errPoor) {
					t.Fatal(err)
				}
			}
		}
	}},
	{"all in on coin", func(t *testing.T, s *store, id int64, v *view, day int) {
		if day == 0 {
			buyAll(t, s, id, v, []string{coinKey})
		}
	}},
	{"trade the coin", func(t *testing.T, s *store, id int64, v *view, day int) {
		f := firmOf(v, coinKey)
		switch {
		case f.Held > 0 && f.Worth > f.Cost*6/5:
			if _, err := s.trade(id, coinKey, f.Held, true); err != nil {
				t.Fatal(err)
			}
		case f.Held == 0 && f.Price*10 < f.Prev*9:
			buyAll(t, s, id, v, []string{coinKey})
		}
	}},
}

// TestMarketStrategies plays ponds with a grown cap through 90 days of the
// market with every pond busy at work, and holds each to well under what the
// side games pay a day. Holding a spread for a month comes out a little ahead.
func TestMarketStrategies(t *testing.T) {
	side := diveCap
	for _, g := range arcadeGames {
		side += g.cap
	}
	const days, purse = 90, 20000
	for run, pepper := range []string{"one pepper for the market sims", "and another, so it isn't one lucky walk"} {
		s, err := openStore(t.TempDir()+"/pond.db", []byte(pepper))
		if err != nil {
			t.Fatal(err)
		}
		clock := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
		s.now = func() time.Time { return clock }
		ids := make([]int64, len(strategies)+1)
		for i := range ids {
			_, ids[i] = newPond(t, s)
			exec(t, s, `UPDATE ponds SET pebbles = ?, staged = 0, gravel_level = 3, garden_level = 3, dye_level = 3, rank_top = 4 WHERE id = ?`, purse, ids[i])
		}
		rnd := rand.New(rand.NewPCG(uint64(run), 3))
		month := 0
		for day := range days {
			for _, a := range assets {
				if a.flow != "" {
					exec(t, s, `INSERT OR IGNORE INTO market_flow (key, day, hour, n) VALUES (?, ?, 12, ?)`,
						a.flow, dayOf(clock), int(200*math.Exp(rnd.NormFloat64()*0.4)))
				}
			}
			lock(t, s)
			for i, st := range strategies {
				if _, err := s.collectDividends(ids[i]); err != nil {
					t.Fatal(err)
				}
				v, _ := s.view(ids[i])
				st.day(t, s, ids[i], v, day)
			}
			// The last pond holds a spread for a month and sells.
			last := ids[len(strategies)]
			if _, err := s.collectDividends(last); err != nil {
				t.Fatal(err)
			}
			if v, _ := s.view(last); day == 0 {
				buyAll(t, s, last, v, shareKeys())
			} else if day == 30 {
				sellAll(t, s, last, v)
				w, _ := s.view(last)
				month = w.Pebbles - purse
			}
			clock = clock.Add(24 * time.Hour)
		}
		lock(t, s)
		for i, st := range strategies {
			v, _ := s.view(ids[i])
			sellAll(t, s, ids[i], v)
			v, _ = s.view(ids[i])
			net := v.Pebbles - purse
			// Goods the market's room couldn't take today are counted at its price.
			for _, f := range v.Market.Firms {
				if f.Kind == kindGood {
					net += f.Held / lot * f.Sell
				}
			}
			t.Logf("%-15s %+6d over %d days, %+5.1f a day, against %d a day from the side games", st.name, net, days, float64(net)/days, side)
			if net/days > side/10 {
				t.Errorf("%s makes %d a day", st.name, net/days)
			}
		}
		t.Logf("%-15s %+6d over 30 days", "a month held", month)
		if month <= 0 || month > 30*side/20 {
			t.Errorf("a month holding a spread came to %d", month)
		}
		s.Close()
	}
}
