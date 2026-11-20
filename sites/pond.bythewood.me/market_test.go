package main

import (
	"errors"
	"math"
	"math/rand/v2"
	"strings"
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

func TestTheBoardIsSixThings(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	lock(t, s)
	v, _ := s.view(id)
	var names []string
	for _, f := range v.Market.Firms {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, ", "); got != "Reeds, Clay, Fish, Sticks, Glow coin, Moonmelon" {
		t.Errorf("the board is %s", got)
	}
}

func TestMarketIgnoresDashsMarkets(t *testing.T) {
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

// A row with no open came from the real markets, so the coin carries on from
// the last real close and that close is never worked out again.
func TestPricesCarryOnFromTheRealClose(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	today, yesterday := dayOf(*clock), dayOf(clock.AddDate(0, 0, -1))
	exec(t, s, `INSERT INTO market_days (day, key, close) VALUES (?, 'bitcoin', 88)`, yesterday)
	exec(t, s, `INSERT INTO shares (pond_id, key, count, cost) VALUES (?, 'bitcoin', 2, 180)`, id)
	lock(t, s)
	c := priceOf(t, s, id, coinKey)
	if c.Prev != 88 || math.Abs(float64(c.Price-88)) > 88*0.25 || c.Held != 2 {
		t.Errorf("the coin after the change: %+v", c)
	}
	*clock = clock.Add(24 * time.Hour)
	lock(t, s)
	var close float64
	if err := s.r.QueryRow(`SELECT close FROM market_days WHERE key = 'bitcoin' AND day = ?`, yesterday).Scan(&close); err != nil || close != 88 {
		t.Errorf("the real close moved to %v: %v", close, err)
	}
	if f := priceOf(t, s, id, "g_reeds"); f.Days[0] != 45 || len(f.Days) != 2 || today == "" {
		t.Errorf("reeds: %+v", f)
	}
}

func flat(int) (float64, float64) { return 0, 1 }

// However hard the walk, the flow, the weather and the residents push, a
// good never moves past its cap in a day.
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
			for _, m := range []dayMove{{pushCap, 0.5}, {-pushCap, -0.5}} {
				for _, at := range []time.Time{start.Add(3 * time.Hour), start.Add(23 * time.Hour)} {
					p := priceAt(a, wild, b, day, a.base, at, false, m)
					if m := math.Abs(math.Log(p / a.base)); m > t2.cap+1e-9 {
						t.Errorf("%s moved %.1f%% in a day", a.Key, 100*m)
					}
				}
			}
		}
	}
	reeds, _ := assetOf("g_reeds")
	if p := priceAt(reeds, flat, none, day, 45, start.Add(12*time.Hour), false, dayMove{}); math.Abs(p-45) > 0.01 {
		t.Errorf("a still day moved reeds to %.2f", p)
	}
}

func TestAGlutTakesAGoodDown(t *testing.T) {
	day := "2026-11-10"
	start, _ := dayBounds(day)
	busy, usual := flowBook{"g_clay": {}}, flowBook{"g_clay": {}}
	for i := 1; i <= flowWeek; i++ {
		busy["g_clay"][dayShift(day, -i)] = &[24]int{12: 300}
		usual["g_clay"][dayShift(day, -i)] = &[24]int{12: 300}
	}
	busy["g_clay"][day] = &[24]int{6: 900}
	usual["g_clay"][day] = &[24]int{12: 300}
	clay, _ := assetOf("g_clay")
	end := start.Add(23 * time.Hour)
	glut := priceAt(clay, flat, busy, day, 45, end, true, dayMove{})
	norm := priceAt(clay, flat, usual, day, 45, end, true, dayMove{})
	if glut >= norm*0.97 {
		t.Errorf("a glut of clay left it at %.2f against %.2f", glut, norm)
	}
}

// The day's push comes in over the morning and then holds, like a story in
// the paper, and is never worth a trade there and back.
func TestThePushComesInOverTheMorning(t *testing.T) {
	day := "2026-11-10"
	start, _ := dayBounds(day)
	reeds, _ := assetOf("g_reeds")
	for _, c := range []struct {
		h    int
		want float64
	}{{0, 0}, {3, 0.02}, {6, 0.04}, {20, 0.04}} {
		at := start.Add(time.Duration(c.h) * time.Hour)
		up := priceAt(reeds, flat, nil, day, 45, at, false, dayMove{push: 0.04})
		if got := math.Log(up / 45); math.Abs(got-c.want) > 0.002 {
			t.Errorf("at %d:00 the push moved reeds %.4f, want %.4f", c.h, got, c.want)
		}
	}
	if pushCap >= 2*goodFee {
		t.Errorf("a push moves a good %.2f and a trade each way costs %.2f", pushCap, 2*goodFee)
	}
}

func TestWeatherAndSeasonPushGoods(t *testing.T) {
	rain := pushesOn(&reading{Cond: "Light rain"}, "2026-11-10", nil)
	if p := rain["g_reeds"]; p.by <= 0 || !strings.Contains(p.why, "Rain") {
		t.Errorf("rain pushed reeds %+v", p)
	}
	if p := rain["g_clay"]; p.by >= 0 || p.why != "Burrow season is over" {
		t.Errorf("November pushed clay %+v", p)
	}
	cold := pushesOn(&reading{Temp: 20, HasTemp: true, Cond: "Clear"}, "2027-01-12", nil)
	if p := cold["g_fish"]; p.by <= 0 || p.why != "Fishing's poor in the cold" {
		t.Errorf("a cold January day pushed fish %+v", p)
	}
	if p := cold["g_sticks"]; p.by != pushCap {
		t.Errorf("a cold January day pushed sticks %+v, past the cap of %.2f", p, pushCap)
	}
	if p := pushesOn(nil, "2026-07-01", nil)[coinKey]; p.by != 0 {
		t.Errorf("the coin was pushed %+v", p)
	}
	for _, r := range []*reading{nil, {Cond: "Thunderstorm"}, {Temp: 99, HasTemp: true}} {
		if p := pushesOn(r, "2026-07-05", nil)[melonKey]; p.by != 0 {
			t.Errorf("a melon was pushed %+v", p)
		}
	}
}

// reedDays is a week of usual reed cutting and then a day before of n.
func reedDays(day string, n int) flowBook {
	b := flowBook{"g_reeds": {}}
	for i := 2; i <= flowWeek+1; i++ {
		b["g_reeds"][dayShift(day, -i)] = &[24]int{12: 200}
	}
	b["g_reeds"][dayShift(day, -1)] = &[24]int{12: n}
	b["g_reeds"][day] = &[24]int{}
	return b
}

func TestThePaperPushesAGood(t *testing.T) {
	day := "2026-11-10"
	if n := reedDays(day, 600).newsOn(day); n == nil || n.Key != "g_reeds" || n.Up || n.Line != newsLines["g_reeds"][0] {
		t.Fatalf("a glut of reeds: %+v", n)
	}
	if n := reedDays(day, 40).newsOn(day); n == nil || n.Up != true || n.Line != newsLines["g_reeds"][1] {
		t.Fatalf("a dead day: %+v", n)
	}
	if n := reedDays(day, 230).newsOn(day); n != nil {
		t.Fatalf("an ordinary day made the paper: %+v", n)
	}
	if p := pushesOn(nil, day, reedDays(day, 600))["g_reeds"]; p.by != -newsBump || p.why != newsLines["g_reeds"][0] {
		t.Errorf("the glut pushed reeds %+v", p)
	}
}

func TestThePaperRunsTheStory(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	day := dayOf(*clock)
	for i := 2; i <= flowWeek+1; i++ {
		exec(t, s, `INSERT INTO market_flow (key, day, hour, n) VALUES ('g_fish', ?, 12, 100)`, dayShift(day, -i))
	}
	exec(t, s, `INSERT INTO market_flow (key, day, hour, n) VALUES ('g_fish', ?, 12, 500)`, dayShift(day, -1))
	lock(t, s)
	v, _ := s.view(id)
	if v.Market.News == nil || v.Market.News.Key != "g_fish" || v.Gazette.Business != newsLines["g_fish"][0] {
		t.Errorf("the paper says %q with %+v", v.Gazette.Business, v.Market.News)
	}
	if f := firmOf(v, "g_fish"); f.Why != newsLines["g_fish"][0] {
		t.Errorf("the board says %q under fish", f.Why)
	}
}

// The residents' trades come off the pepper and the day alone, so every
// replica and every restart deals the same, and they show on the tape.
func TestResidentsTradeTheSameEverywhere(t *testing.T) {
	pepper := []byte("a pepper")
	day := "2026-11-10"
	push := map[string]push{"g_reeds": {0.04, "Rain on the way, and the gardens want reeds"}}
	a, b := folkTradesOn(pepper, day, push), folkTradesOn(pepper, day, push)
	if len(a) < 30 || len(a) > 90 || len(a) != len(b) {
		t.Fatalf("%d and %d trades in a day", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("trade %d is %+v and %+v", i, a[i], b[i])
		}
	}
	if c := folkTradesOn([]byte("another"), day, push); len(c) == len(a) && c[0] == a[0] && c[1] == a[1] {
		t.Error("another pepper deals the same trades")
	}
	// Sedge stocks reeds before the rain, and the reeds go up with it.
	sedge, buys := 0, 0
	for d := range 30 {
		for _, tr := range folkTradesOn(pepper, dayShift(day, d), push) {
			if tr.Who == 0 && tr.Key == "g_reeds" {
				sedge++
				if tr.Buy {
					buys++
				}
			}
		}
	}
	if sedge < 10 || buys*10 < sedge*7 {
		t.Errorf("Sedge bought reeds %d times in %d before the rain", buys, sedge)
	}
	if x := folkImpact(a, "g_reeds", 24*60); x <= 0 {
		t.Errorf("a rainy day's trades moved reeds %.4f", x)
	}
	if folkImpact(a, coinKey, 24*60) != 0 || folkImpact(a, melonKey, 24*60) != 0 {
		t.Error("the residents moved the coin or the melon")
	}
	noon, _ := dayBounds(day)
	tape := tapeOf(pepper, day, push, noon.Add(12*time.Hour))
	if len(tape) != tapeLen || tape[0].Name == "" || tape[0].Line == "" || tape[0].At == "" {
		t.Errorf("the tape is %+v", tape)
	}
	for _, r := range tape {
		if strings.ContainsAny(r.Line, ";—") || strings.HasSuffix(r.Line, " ") {
			t.Errorf("a tape line reads %q", r.Line)
		}
	}
}

// Big Bean sells fish when there are too many about.
func TestBigBeanSellsAGlut(t *testing.T) {
	pepper := []byte("a pepper")
	glut := map[string]push{"g_fish": {-0.04, newsLines["g_fish"][0]}}
	bean, sold, said := 0, 0, false
	for d := range 30 {
		for _, tr := range folkTradesOn(pepper, dayShift("2026-11-10", d), glut) {
			if tr.Who == 6 && tr.Key == "g_fish" {
				bean++
				if !tr.Buy {
					sold++
				}
				said = said || strings.HasSuffix(tradeLine(tr), "since he has too many")
			}
		}
	}
	if bean < 10 || sold*10 < bean*7 || !said {
		t.Errorf("Big Bean sold fish %d times in %d in a glut", sold, bean)
	}
}

func TestTheCoinCostsAFeeEachWay(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	rich(t, s, id, 1000)
	v, err := s.trade(id, coinKey, 2, false)
	if err != nil || v.Pebbles != 1000-206 {
		t.Fatalf("bought two at 100 and have %d: %v", v.Pebbles, err)
	}
	if _, err := s.trade(id, coinKey, 3, false); !errors.Is(err, errCap) {
		t.Errorf("went past the cap of %d: %v", v.Market.Cap, err)
	}
	for _, k := range []string{"unicorns", "gold", "guild"} {
		if _, err := s.trade(id, k, 1, false); !errors.Is(err, errFirm) {
			t.Errorf("bought %s: %v", k, err)
		}
	}
	if _, err := s.trade(id, coinKey, 3, true); !errors.Is(err, errNotHeld) {
		t.Errorf("sold more than held: %v", err)
	}
	v, err = s.trade(id, coinKey, 1, true)
	if err != nil || v.Pebbles != 1000-206+97 {
		t.Fatalf("sold one back and have %d: %v", v.Pebbles, err)
	}
	f := priceOf(t, s, id, coinKey)
	if f.Held != 1 || f.Cost != 103 || f.Worth >= f.Cost {
		t.Errorf("what's left: %+v", f)
	}
	if _, err := s.trade(id, coinKey, 0, false); !errors.Is(err, errCount) {
		t.Errorf("bought none: %v", err)
	}
}

func TestTheCapGrowsWithThePond(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	exec(t, s, `UPDATE ponds SET gravel_level = 5, garden_level = 5, dye_level = 5, rank_top = 8 WHERE id = ?`, id)
	w, _ := s.view(id)
	if v.Market.Cap != 500 || w.Market.Cap != 7500 {
		t.Errorf("the cap runs from %d to %d", v.Market.Cap, w.Market.Cap)
	}
	rich(t, s, id, 10000)
	if _, err := s.trade(id, coinKey, 70, false); err != nil {
		t.Errorf("a grown pond can't buy 70 coin: %v", err)
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
		t.Errorf("the sticks went in the bag as %+v, with %d held", g, v.Market.In)
	}
	before := v.Pebbles
	v, err = s.trade(id, "g_sticks", 1, true)
	if err != nil || v.Pebbles-before != f.Sell || f.Sell >= f.Buy || v.Market.Room != marketRoom-f.Sell {
		t.Errorf("sold a lot back for %d with %d room left, a lot is %d to buy and %d to sell: %v", v.Pebbles-before, v.Market.Room, f.Buy, f.Sell, err)
	}
	if _, err := s.trade(id, "g_clay", 1, true); !errors.Is(err, errNoLot) {
		t.Errorf("sold clay it never had: %v", err)
	}
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

func hasTodo(v *view, kind string) bool {
	for _, d := range todoOf(v) {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

// The coin swings far more than a good and holding it is worth about what
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
			q := priceAt(coin, d, nil, day, p, time.Time{}, true, dayMove{})
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

// ---------------------------------------------------------------- melons

// sunday is the clock at a time of day on the Sunday of the test week.
func sunday(h int) time.Time { return nyAt("2026-11-15", h, 0) }

func TestMelonsAreBoughtOnSundayOnly(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	rich(t, s, id, 5000)
	*clock = nyAt("2026-11-14", 12, 0)
	lock(t, s)
	if _, err := s.trade(id, melonKey, 1, false); !errors.Is(err, errMelonShut) {
		t.Fatalf("bought a melon on a Saturday: %v", err)
	}
	*clock = sunday(9)
	lock(t, s)
	f := priceOf(t, s, id, melonKey)
	if math.Abs(float64(f.Price)-30) > 30*0.1 || f.Prev != 30 {
		t.Errorf("Sunday's melons open at %+v", f)
	}
	v, err := s.trade(id, melonKey, 5, false)
	if err != nil || v.Market.Melon.Held != 5 || v.Market.Melon.Left != melonMax-5 || !v.Market.Melon.Open || v.Market.Melon.Days != 7 {
		t.Fatalf("bought five: %+v %v", v.Market.Melon, err)
	}
	if v.Market.In != firmOf(v, melonKey).Cost || v.Market.In == 0 {
		t.Errorf("melons cost %d and %d counts toward the cap", firmOf(v, melonKey).Cost, v.Market.In)
	}
	if _, err := s.trade(id, melonKey, melonMax-4, false); !errors.Is(err, errMelonMax) {
		t.Errorf("bought past the week's %d: %v", melonMax, err)
	}
	// They sell all week, and not past Saturday.
	*clock = nyAt("2026-11-18", 12, 0)
	lock(t, s)
	before := priceOf(t, s, id, melonKey)
	v, err = s.trade(id, melonKey, 2, true)
	if err != nil || firmOf(v, melonKey).Held != 3 || v.Market.Melon.Open || v.Market.Melon.Days != 4 {
		t.Fatalf("sold two on Wednesday: %+v %v", v.Market.Melon, err)
	}
	if _, err := s.trade(id, melonKey, 1, false); !errors.Is(err, errMelonShut) {
		t.Errorf("bought one on Wednesday: %v", err)
	}
	_ = before
}

func TestMelonsSpoilSaturdayNight(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	rich(t, s, id, 5000)
	*clock = sunday(10)
	lock(t, s)
	if _, err := s.trade(id, melonKey, 4, false); err != nil {
		t.Fatal(err)
	}
	*clock = nyAt("2026-11-21", 23, 50)
	lock(t, s)
	if f := priceOf(t, s, id, melonKey); f.Held != 4 || f.Worth == 0 {
		t.Errorf("late Saturday the melons are %+v", f)
	}
	*clock = nyAt("2026-11-22", 0, 20)
	lock(t, s)
	v, _ := s.view(id)
	if f := firmOf(v, melonKey); f.Held != 0 || v.Market.In != 0 {
		t.Errorf("Sunday the old melons are %+v, %d held", f, v.Market.In)
	}
	pebbles := v.Pebbles
	if _, err := s.trade(id, melonKey, 1, true); !errors.Is(err, errNotHeld) {
		t.Errorf("sold a spoiled melon: %v", err)
	}
	*clock = clock.Add(5 * time.Hour)
	g, err := s.greet(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if !awaySays(g, "4 moonmelons spoiled on Saturday night") || g.Pond.Pebbles < pebbles {
		t.Errorf("the away list says %v", g.Missed)
	}
	if g, _ = s.greet(id, false); awaySays(g, "spoiled") {
		t.Error("the spoiled melons were said twice")
	}
	// A fresh week buys afresh.
	if v, err := s.trade(id, melonKey, 2, false); err != nil || v.Market.Melon.Held != 2 || v.Market.Melon.Left != melonMax-2 {
		t.Errorf("the next Sunday: %+v %v", v.Market.Melon, err)
	}
}

func awaySays(g *greeting, part string) bool {
	for _, m := range g.Missed {
		if strings.Contains(m[1], part) {
			return true
		}
	}
	return false
}

// A melon's price is a fair coin over the week, so buying on Sunday and
// selling at the best moment there's a rule for still loses the fee.
func TestTheMelonIsATimedGamble(t *testing.T) {
	melon, _ := assetOf(melonKey)
	rnd := rand.New(rand.NewPCG(5, 11))
	d := func(int) (float64, float64) { return rnd.NormFloat64(), rnd.Float64() }
	const weeks = 20000
	hold, take := 0.0, 0.0
	for range weeks {
		p, sold := melon.base, 0.0
		for day := range 7 {
			p = priceAt(melon, d, nil, "2026-11-15", p, time.Time{}, true, dayMove{})
			if sold == 0 && (p > melon.base*1.15 || day == 6) {
				sold = p
			}
		}
		hold += p
		take += sold
	}
	buy := float64(buyCost(melon.base, 1, melonFee))
	back := take / weeks * (1 - melonFee)
	t.Logf("a melon costs %.1f, held to Saturday it's %.1f, sold at 15%% up or on Saturday it brings %.1f", buy, hold/weeks, back)
	if back >= buy || math.Abs(hold/weeks-melon.base) > 0.5 {
		t.Errorf("a melon costs %.1f and brings %.1f", buy, back)
	}
}

// ---------------------------------------------------------------- shares go

// Shares and the dividends owed on them are paid out once at their last
// price with no fee, and the pond is told what they came to.
func TestSharesArePaidOutOnce(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	_, other := newPond(t, s)
	today := dayOf(*clock)
	yesterday, before := dayShift(today, -1), dayShift(today, -2)
	exec(t, s, `UPDATE ponds SET pebbles = 100, div_day = ?, div_carry = 0.5, div_total = 3 WHERE id = ?`, before, id)
	exec(t, s, `UPDATE ponds SET pebbles = 100 WHERE id = ?`, other)
	exec(t, s, `INSERT INTO market_days (day, key, open, close, div) VALUES
		(?, 'gold', 100, 110, 0.3), (?, 'gold', 110, 120.4, 0.3), (?, 'guild', 40, 41, 0.1), (?, 'guild', 41, 43, 0.1),
		(?, 'g_reeds', 45, 47, 0)`, before, yesterday, before, yesterday, yesterday)
	exec(t, s, `INSERT INTO shares (pond_id, key, count, cost) VALUES (?, 'gold', 3, 300), (?, 'guild', 10, 400),
		(?, 'bitcoin', 4, 400), (?, 'fishery', 2, 80)`, id, id, id, other)
	exec(t, s, `DELETE FROM migrations`)
	if err := payShares(s.w, *clock); err != nil {
		t.Fatal(err)
	}
	// 3 gold at 120.4 and 10 guild at 43 is 791.2, and the dividends are
	// half a pebble carried and two days of 0.9 and 1, so 4.3.
	var pebbles, total int
	var carry float64
	s.r.QueryRow(`SELECT pebbles, div_total, div_carry FROM ponds WHERE id = ?`, id).Scan(&pebbles, &total, &carry)
	if pebbles != 100+792+5 || total != 3+5 || carry != 0 {
		t.Errorf("paid out to %d with %d in dividends and %.2f carried", pebbles, total, carry)
	}
	s.r.QueryRow(`SELECT pebbles FROM ponds WHERE id = ?`, other).Scan(&pebbles)
	if pebbles != 100+80 {
		t.Errorf("two fishery shares at their launch price came to %d", pebbles-100)
	}
	var left, coins int
	s.r.QueryRow(`SELECT COUNT(*) FROM shares WHERE key != 'bitcoin'`).Scan(&left)
	s.r.QueryRow(`SELECT count FROM shares WHERE key = 'bitcoin' AND pond_id = ?`, id).Scan(&coins)
	if left != 0 || coins != 4 {
		t.Errorf("%d shares left and %d coin", left, coins)
	}
	// Again, with the marker or without it, pays nothing more.
	if err := payShares(s.w, *clock); err != nil {
		t.Fatal(err)
	}
	exec(t, s, `DELETE FROM migrations`)
	if err := payShares(s.w, *clock); err != nil {
		t.Fatal(err)
	}
	s.r.QueryRow(`SELECT pebbles FROM ponds WHERE id = ?`, id).Scan(&pebbles)
	if pebbles != 100+792+5 {
		t.Errorf("a second run took it to %d", pebbles)
	}
	*clock = clock.Add(5 * time.Hour)
	g, err := s.greet(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if !awaySays(g, "your 13 shares came to 792 pebbles, paid in full with no fee, and 5 pebbles more for the dividends") {
		t.Errorf("the away list says %v", g.Missed)
	}
	if g, _ := s.greet(other, false); !awaySays(g, "your 2 shares came to 80 pebbles, paid in full with no fee") || awaySays(g, "dividends") {
		t.Errorf("the other pond's away list says %v", g.Missed)
	}
}

// A store opened on a database with shares in it pays them out as it opens.
func TestOpeningPaysTheShares(t *testing.T) {
	path := t.TempDir() + "/pond.db"
	s, err := openStore(path, []byte("pepper"))
	if err != nil {
		t.Fatal(err)
	}
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET pebbles = 0 WHERE id = ?`, id)
	exec(t, s, `INSERT INTO shares (pond_id, key, count, cost) VALUES (?, 'dow', 2, 200)`, id)
	exec(t, s, `DELETE FROM migrations`)
	s.Close()
	for range 2 {
		s, err = openStore(path, []byte("pepper"))
		if err != nil {
			t.Fatal(err)
		}
		var pebbles int
		s.r.QueryRow(`SELECT pebbles FROM ponds WHERE id = ?`, id).Scan(&pebbles)
		if pebbles != 200 {
			t.Errorf("two Old Bullfrog shares at launch came to %d", pebbles)
		}
		s.Close()
	}
}

// ---------------------------------------------------------------- the rest

// The market keepsakes and Big Bean's sale read the real day, so they carry
// on whatever the board does.
func TestTheRealMarketStillCounts(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	pinDay(s, reading{SP: -2, HasSP: true, VIX: 34, Markets: map[string]float64{"sp500": -2}})
	*clock = clock.Add(5 * time.Hour)
	g, err := s.greet(id, false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, k := range g.Pond.Keepsakes {
		got[k.Kind] = k.First != ""
	}
	if !got["k_penny"] || !got["k_kite"] || got["k_lily"] {
		t.Errorf("a red day with the VIX at 34 stamped %v", got)
	}
	if !g.Pond.Shops[shopBrindle].Sale {
		t.Error("Big Bean has no sale on a down day")
	}
	pinDay(s, reading{SP: 1.8, HasSP: true, VIX: 12, Markets: map[string]float64{"sp500": 1.8}})
	*clock = clock.Add(24 * time.Hour)
	g, _ = s.greet(id, false)
	for _, k := range g.Pond.Keepsakes {
		got[k.Kind] = k.First != ""
	}
	if !got["k_lily"] || g.Pond.Shops[shopBrindle].Sale {
		t.Errorf("a green day stamped %v with a sale %v", got, g.Pond.Shops[shopBrindle].Sale)
	}
}

// ---------------------------------------------------------------- the purse

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
		n := min(100, each/max(1, f.Buy))
		if k == melonKey {
			n = min(n, v.Market.Melon.Left)
		}
		if n > 0 {
			if _, err := s.trade(id, k, n, false); err != nil && !errors.Is(err, errCap) && !errors.Is(err, errPoor) && !errors.Is(err, errMelonShut) {
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

func sellGoods(t *testing.T, s *store, id int64, f firmView) {
	t.Helper()
	if f.Held < lot {
		return
	}
	if _, err := s.trade(id, f.Key, min(100, f.Held/lot), true); err != nil && !errors.Is(err, errMarketFull) {
		t.Fatal(err)
	}
}

var strategies = []strategy{
	{"churn goods", func(t *testing.T, s *store, id int64, v *view, day int) {
		for _, f := range v.Market.Firms {
			if f.Kind != kindGood {
				continue
			}
			if float64(f.Price) > f.base {
				sellGoods(t, s, id, f)
			} else if float64(f.Price) < f.base*0.95 && f.Held < 5*lot {
				if _, err := s.trade(id, f.Key, 1, false); err != nil && !errors.Is(err, errPoor) {
					t.Fatal(err)
				}
			}
		}
	}},
	// Buys whatever the weather or the paper pushes up and sells it the next day.
	{"ride the push", func(t *testing.T, s *store, id int64, v *view, day int) {
		for _, f := range v.Market.Firms {
			if f.Kind != kindGood {
				continue
			}
			sellGoods(t, s, id, f)
			if f.Why != "Steady" && f.Price > f.Prev {
				if _, err := s.trade(id, f.Key, 3, false); err != nil && !errors.Is(err, errPoor) {
					t.Fatal(err)
				}
			}
		}
	}},
	// Follows the residents, buying what they bought most of today.
	{"follow the tape", func(t *testing.T, s *store, id int64, v *view, day int) {
		net := map[string]int{}
		for _, r := range v.Market.Tape {
			if r.Buy {
				net[r.Key]++
			} else {
				net[r.Key]--
			}
		}
		for _, f := range v.Market.Firms {
			switch {
			case f.Kind == kindGood && net[f.Key] > 1:
				if _, err := s.trade(id, f.Key, 2, false); err != nil && !errors.Is(err, errPoor) {
					t.Fatal(err)
				}
			case f.Kind == kindGood && net[f.Key] < 0:
				sellGoods(t, s, id, f)
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
	// Fills up on melons every Sunday and sells once they're a fifth up, or on Saturday.
	{"melon gambler", func(t *testing.T, s *store, id int64, v *view, day int) {
		f := firmOf(v, melonKey)
		switch {
		case v.Market.Melon.Open && f.Held == 0:
			buyAll(t, s, id, v, []string{melonKey})
		case f.Held > 0 && (f.Worth > f.Cost*6/5 || v.Market.Melon.Days == 1):
			if _, err := s.trade(id, melonKey, f.Held, true); err != nil {
				t.Fatal(err)
			}
		}
	}},
}

// skyOn deals a day's weather for the purse test, rain one day in four and a
// cold snap one in six, so the pushes come and go.
func skyOn(rnd *rand.Rand) reading {
	r := reading{Cond: "Clear", Temp: 55, HasTemp: true}
	if rnd.IntN(4) == 0 {
		r.Cond = "Rain"
	}
	if rnd.IntN(6) == 0 {
		r.Temp = 25
	}
	if rnd.IntN(10) == 0 {
		r.Cond = "Thunderstorm"
	}
	return r
}

// TestMarketStrategies plays ponds with a grown cap through 90 days of the
// night market, with every pond busy at work and the weather changing, and
// holds each to well under what the side games pay a day.
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
		ids := make([]int64, len(strategies))
		for i := range ids {
			_, ids[i] = newPond(t, s)
			exec(t, s, `UPDATE ponds SET pebbles = ?, staged = 0, gravel_level = 3, garden_level = 3, dye_level = 3, rank_top = 4 WHERE id = ?`, purse, ids[i])
		}
		rnd := rand.New(rand.NewPCG(uint64(run), 3))
		for day := range days {
			pinDay(s, skyOn(rnd))
			for _, a := range assets {
				if a.flow != "" {
					exec(t, s, `INSERT OR IGNORE INTO market_flow (key, day, hour, n) VALUES (?, ?, 12, ?)`,
						a.flow, dayOf(clock), int(200*math.Exp(rnd.NormFloat64()*0.4)))
				}
			}
			lock(t, s)
			for i, st := range strategies {
				v, _ := s.view(ids[i])
				st.day(t, s, ids[i], v, day)
			}
			clock = clock.Add(24 * time.Hour)
		}
		lock(t, s)
		for i, st := range strategies {
			v, _ := s.view(ids[i])
			sellAll(t, s, ids[i], v)
			v, _ = s.view(ids[i])
			net := v.Pebbles - purse
			for _, f := range v.Market.Firms {
				if f.Kind == kindGood {
					net += f.Held / lot * f.Sell
				}
			}
			t.Logf("%-16s %+6d over %d days, %+5.1f a day, against %d a day from the side games", st.name, net, days, float64(net)/days, side)
			if net/days > side/10 {
				t.Errorf("%s makes %d a day", st.name, net/days)
			}
		}
		s.Close()
	}
}
