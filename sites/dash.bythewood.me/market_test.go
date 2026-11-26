package main

import (
	"testing"
	"time"
)

// at builds a New York wall clock time, the clock every session boundary here
// is expressed in.
func at(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02 15:04", s, easternTime())
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return ts
}

func TestEquitySession(t *testing.T) {
	// 2026-08-27 is a Thursday and 2026-08-29 a Saturday.
	cases := []struct {
		when string
		want string
	}{
		{"2026-08-27 03:59", "closed"},
		{"2026-08-27 04:00", "pre"},
		{"2026-08-27 09:29", "pre"},
		{"2026-08-27 09:30", "regular"},
		{"2026-08-27 15:59", "regular"},
		{"2026-08-27 16:00", "post"},
		{"2026-08-27 19:59", "post"},
		{"2026-08-27 20:00", "closed"},
		{"2026-08-29 12:00", "closed"},
		{"2026-08-30 12:00", "closed"},
	}

	for _, c := range cases {
		if got, _ := equitySession(at(t, c.when)); got != c.want {
			t.Errorf("%s: session %q, want %q", c.when, got, c.want)
		}
	}
	// The badge already says closed, so the phase says when it opens.
	if _, phase := equitySession(at(t, "2026-08-27 20:30")); phase != "Opens in 7h 30m" {
		t.Errorf("thursday night: %q", phase)
	}
	if _, phase := equitySession(at(t, "2026-08-28 21:00")); phase != "Opens Monday" {
		t.Errorf("friday night: %q", phase)
	}
}

func TestBuildMarketSwapsToFuturesOutsideRegularHours(t *testing.T) {
	session := at(t, "2026-08-27 10:00")
	quotes := map[string]Quote{
		"^GSPC": {Symbol: "^GSPC", Price: 100, Previous: 99, AsOf: session, Closes: []float64{99, 100}},
		"ES=F":  {Symbol: "ES=F", Price: 101, Previous: 99, AsOf: session, Closes: []float64{99, 101}},
	}

	regular := buildMarket(quotes, session)
	if got := regular.Cards[0].Symbol; got != "^GSPC" {
		t.Errorf("during the session the S&P card is %q, want ^GSPC", got)
	}
	if regular.Cards[0].Note != "" {
		t.Errorf("cash card carries note %q, want none", regular.Cards[0].Note)
	}

	overnight := buildMarket(quotes, at(t, "2026-08-27 21:00"))
	if got := overnight.Cards[0].Symbol; got != "ES=F" {
		t.Errorf("after hours the S&P card is %q, want ES=F", got)
	}
	if overnight.Cards[0].Note == "" {
		t.Error("a futures card has to say it is futures")
	}
}

// A market holiday falls inside regular hours by the clock, and there is no
// holiday calendar here, so the quote's own age catches it.
func TestBuildMarketTreatsAStaleCashQuoteAsClosed(t *testing.T) {
	now := at(t, "2026-08-27 14:00")

	fresh := map[string]Quote{
		"^GSPC": {Price: 100, Previous: 99, AsOf: now.Add(-2 * time.Minute)},
	}
	if got, _ := buildMarket(fresh, now).Session, ""; got != "regular" {
		t.Errorf("a fresh quote at 2pm reads %q, want regular", got)
	}

	stale := map[string]Quote{
		"^GSPC": {Price: 100, Previous: 99, AsOf: now.Add(-4 * time.Hour)},
	}
	m := buildMarket(stale, now)
	if m.Session != "closed" {
		t.Errorf("a four hour old quote at 2pm reads %q, want closed", m.Session)
	}
	if m.Cards[0].Symbol != "ES=F" {
		t.Errorf("a closed session shows %q, want the future", m.Cards[0].Symbol)
	}
}

func TestBuildMarketMarksAMissingSymbolUnavailable(t *testing.T) {
	m := buildMarket(map[string]Quote{}, at(t, "2026-08-27 10:00"))
	for _, c := range m.Cards {
		if !c.Unavailable {
			t.Errorf("%s should be unavailable with no quotes behind it", c.Key)
		}
	}
}

func TestBuildMarketDrawdownIsSignedAgainstTheHigh(t *testing.T) {
	now := at(t, "2026-08-27 10:00")
	quotes := map[string]Quote{
		"^GSPC": {Price: 90, Previous: 89, High52: 100, AsOf: now},
	}
	m := buildMarket(quotes, now)
	if m.DrawdownPct > -9.9 || m.DrawdownPct < -10.1 {
		t.Errorf("drawdown %.2f, want about -10", m.DrawdownPct)
	}
}

// Every symbol either half of a card can ask for has to be in the one request
// the poller makes, or a session flip shows an unavailable card until the next
// tick.
func TestSparkSymbolsCoversBothHalvesOfEveryCard(t *testing.T) {
	asked := map[string]bool{}
	for _, s := range sparkSymbols() {
		asked[s] = true
	}
	for _, in := range instruments {
		if !asked[in.Cash] {
			t.Errorf("%s: cash symbol %s is never fetched", in.Key, in.Cash)
		}
		if in.Future != "" && !asked[in.Future] {
			t.Errorf("%s: future %s is never fetched", in.Key, in.Future)
		}
	}
}

func TestQuoteChangeIsZeroWithNoPreviousClose(t *testing.T) {
	q := Quote{Price: 100}
	if q.change() != 0 || q.percent() != 0 {
		t.Errorf("with no previous close change is %v and percent %v, want zero for both", q.change(), q.percent())
	}
}

// Yahoo's spark endpoint intermittently returns two closes for a symbol that
// had hundreds a minute earlier, and a straight line between two points is not
// a sparkline.
func TestCarrySparksHoldsTheLastGoodShape(t *testing.T) {
	day := at(t, "2026-08-27 14:00")

	full := buildMarket(map[string]Quote{
		"^GSPC": {Price: 100, Previous: 99, AsOf: day, Closes: []float64{99, 100, 101, 102, 103, 104}, Times: ending(day, 6)},
	}, day)
	if full.Cards[0].Spark.Points != 6 {
		t.Fatalf("fixture drew %d points, want 6", full.Cards[0].Spark.Points)
	}

	degraded := buildMarket(map[string]Quote{
		"^GSPC": {Price: 105, Previous: 99, AsOf: day.Add(time.Minute), Closes: []float64{104, 105}, Times: ending(day, 2)},
	}, day)

	got := carrySparks(degraded, full)
	if got.Cards[0].Spark.Points != 6 {
		t.Errorf("kept %d points, want the previous 6", got.Cards[0].Spark.Points)
	}
	// The figures are still the fresh ones, only the shape is held back.
	if got.Cards[0].Price != "105.00" {
		t.Errorf("price %q, want the fresh 105.00", got.Cards[0].Price)
	}
}

func TestCarrySparksTakesAHealthyPoll(t *testing.T) {
	day := at(t, "2026-08-27 14:00")

	prev := buildMarket(map[string]Quote{
		"^GSPC": {Price: 100, Previous: 99, AsOf: day, Closes: []float64{99, 100}, Times: ending(day, 2)},
	}, day)
	next := buildMarket(map[string]Quote{
		"^GSPC": {Price: 101, Previous: 99, AsOf: day.Add(time.Minute), Closes: []float64{99, 100, 101, 102, 103}, Times: ending(day, 5)},
	}, day)

	if got := carrySparks(next, prev).Cards[0].Spark.Points; got != 5 {
		t.Errorf("kept %d points, want the fresh 5", got)
	}
}

// Yesterday's shape on today's card would be a chart of the wrong day, so the
// carry only applies within one session.
func TestCarrySparksStopsAtADayBoundary(t *testing.T) {
	yesterday := at(t, "2026-08-26 14:00")
	today := at(t, "2026-08-27 09:35")

	prev := buildMarket(map[string]Quote{
		"^GSPC": {Price: 100, Previous: 99, AsOf: yesterday, Closes: []float64{99, 100, 101, 102, 103, 104}, Times: ending(yesterday, 6)},
	}, yesterday)
	next := buildMarket(map[string]Quote{
		"^GSPC": {Price: 101, Previous: 100, AsOf: today, Closes: []float64{100, 101}, Times: ending(today, 2)},
	}, today)

	if got := carrySparks(next, prev).Cards[0].Spark.Points; got != 2 {
		t.Errorf("kept %d points, want today's 2 rather than yesterday's shape", got)
	}
}

// A card that swapped between cash and futures is a different instrument, so
// the shape must not follow it across.
func TestCarrySparksStopsAtASymbolChange(t *testing.T) {
	day := at(t, "2026-08-27 15:55")
	after := at(t, "2026-08-27 16:05")

	prev := buildMarket(map[string]Quote{
		"^GSPC": {Price: 100, Previous: 99, AsOf: day, Closes: []float64{99, 100, 101, 102, 103, 104}, Times: ending(day, 6)},
	}, day)
	next := buildMarket(map[string]Quote{
		"ES=F": {Price: 101, AsOf: after, Closes: []float64{99, 100, 101},
			Times: append(ending(at(t, "2026-08-26 15:55"), 1), ending(after, 2)...)},
	}, after)

	if prev.Cards[0].Symbol == next.Cards[0].Symbol {
		t.Fatalf("fixture did not swap: both cards are %s", prev.Cards[0].Symbol)
	}
	if got := carrySparks(next, prev).Cards[0].Spark.Points; got != 2 {
		t.Errorf("kept %d points, want the futures card's own 2 since 4pm", got)
	}
}

// The spark endpoint answers 400 instead of truncating past its symbol limit,
// so every symbol the page needs has to arrive in a batch small enough to be
// accepted.
func TestSparkSymbolsBatchWithinTheLimit(t *testing.T) {
	symbols := sparkSymbols()
	if len(symbols) <= sparkBatch {
		t.Skip("the whole list fits in one request, so batching is untested here")
	}

	var batched int
	for start := 0; start < len(symbols); start += sparkBatch {
		end := min(start+sparkBatch, len(symbols))
		if size := end - start; size > sparkBatch {
			t.Errorf("batch of %d exceeds the limit of %d", size, sparkBatch)
		}
		batched += end - start
	}
	if batched != len(symbols) {
		t.Errorf("batching covered %d of %d symbols", batched, len(symbols))
	}
}

// Rates and sectors have their own slower poll, so between the two lists every
// symbol the page shows has to be fetched by something.
func TestEverySymbolIsFetchedBySomePoll(t *testing.T) {
	asked := map[string]bool{}
	for _, s := range sparkSymbols() {
		asked[s] = true
	}
	for _, s := range rateAndSectorSymbols() {
		asked[s] = true
	}

	for _, r := range rates {
		if !asked[r.Symbol] {
			t.Errorf("rate %s is never fetched", r.Symbol)
		}
	}
	for _, sec := range sectors {
		if !asked[sec.Symbol] {
			t.Errorf("sector %s is never fetched", sec.Symbol)
		}
	}
	for _, in := range instruments {
		if !asked[in.Cash] {
			t.Errorf("%s cash symbol %s is never fetched", in.Key, in.Cash)
		}
	}
}

// The fast poll is the one that runs every thirty seconds, so what rides it is
// the thing that decides how hard Yahoo gets hit. Rates and sectors do not
// belong on it.
func TestTheFastPollCarriesOnlyTheStrip(t *testing.T) {
	fast := map[string]bool{}
	for _, s := range sparkSymbols() {
		fast[s] = true
	}

	for _, r := range rates {
		if fast[r.Symbol] {
			t.Errorf("rate %s rides the 30 second poll", r.Symbol)
		}
	}
	for _, sec := range sectors {
		if fast[sec.Symbol] {
			t.Errorf("sector %s rides the 30 second poll", sec.Symbol)
		}
	}

	// Two batches at thirty seconds is 240 requests an hour, and the budget
	// has to leave room for the slower polls beside it.
	session, extended := splitStrip()
	batches := (len(session)+sparkBatch-1)/sparkBatch + (len(extended)+sparkBatch-1)/sparkBatch
	if perHour := batches * 120; perHour > budgets["yahoo"].perHour/2 {
		t.Errorf("the fast poll alone would spend %d of a %d budget", perHour, budgets["yahoo"].perHour)
	}
}

// The strip goes out as two requests, and each half still has to fit the
// endpoint's symbol limit or it answers 400 instead of truncating.
func TestSplitStripCoversTheWholeStripInAcceptableBatches(t *testing.T) {
	session, extended := splitStrip()

	seen := map[string]bool{}
	for _, half := range [][]string{session, extended} {
		if len(half) > sparkBatch {
			t.Errorf("half of %d symbols needs batching past the limit of %d", len(half), sparkBatch)
		}
		for _, s := range half {
			seen[s] = true
		}
	}

	for _, s := range sparkSymbols() {
		if !seen[s] {
			t.Errorf("%s is in neither half of the strip fetch", s)
		}
	}
	for _, s := range session {
		if roundClock(s) {
			t.Errorf("%s trades around the clock and needs the wider range", s)
		}
	}
}

// A cash index keeps Yahoo's own previous close, which is already the 4pm one
// and is what every other site quotes. Only the symbols Yahoo dates by UTC or by
// contract get the close read off their bars.
func TestBuildMarketOnlyRecomputesThePreviousCloseForRoundTheClockSymbols(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)
	now := open.Add(2 * time.Hour)

	series := func(from time.Time, n int, price float64) ([]float64, []int64) {
		var closes []float64
		var times []int64
		for i := range n {
			closes = append(closes, price)
			times = append(times, from.Add(time.Duration(i)*30*time.Minute).Unix())
		}
		return closes, times
	}

	// A day of bars before the open at 50, then the session itself at 100.
	oldCloses, oldTimes := series(open.AddDate(0, 0, -1), 20, 50)
	newCloses, newTimes := series(open, 4, 100)

	quotes := map[string]Quote{
		"^GSPC": {
			Symbol: "^GSPC", Price: 100, Previous: 80, AsOf: now,
			Closes: append(oldCloses, newCloses...), Times: append(oldTimes, newTimes...),
		},
		"BTC-USD": {
			Symbol: "BTC-USD", Price: 100, Previous: 80, AsOf: now,
			Closes: append(oldCloses, newCloses...), Times: append(oldTimes, newTimes...),
		},
	}

	cards := map[string]Card{}
	for _, c := range buildMarket(quotes, now).Cards {
		cards[c.Key] = c
	}

	if got := cards["sp500"].Percent; got != "+25.00%" {
		t.Errorf("the S&P measured %s, want +25.00%% off Yahoo's own 80", got)
	}
	if got := cards["bitcoin"].Percent; got != "+100.00%" {
		t.Errorf("bitcoin measured %s, want +100.00%% off 4pm yesterday's 50", got)
	}
}

// Yahoo's own previous close is relative to the range asked for, so on the five
// day batch it's five days old. A card with no 4pm in its bars says it has no
// baseline instead of quietly measuring from last week.
func TestRoundTheClockCardWithNoBellWithholdsTheChange(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)
	now := open.Add(2 * time.Hour)

	// Bars start after the open, so there is no previous 4pm anywhere in them.
	var closes []float64
	var times []int64
	for i := range 4 {
		closes = append(closes, 100)
		times = append(times, open.Add(time.Duration(i)*30*time.Minute).Unix())
	}

	quotes := map[string]Quote{
		"BTC-USD": {
			Symbol: "BTC-USD", Price: 100, Previous: 80, AsOf: now,
			Closes: closes, Times: times,
		},
	}

	var card Card
	for _, c := range buildMarket(quotes, now).Cards {
		if c.Key == "bitcoin" {
			card = c
		}
	}

	if card.Unavailable {
		t.Fatal("the price is known, so the card is not a NO SIGNAL")
	}
	if card.Percent != "" {
		t.Errorf("percent is %q, want it withheld rather than measured from Yahoo's stale 80", card.Percent)
	}
	if card.Spark.HasBase {
		t.Error("a card with no baseline should not rule a line across")
	}
}

// The board is the sectors plus the index they are read against, which also
// makes it a complete grid.
func TestSectorBoardCarriesTheBenchmark(t *testing.T) {
	quotes := map[string]Quote{}
	for _, s := range append(sectors, benchmark) {
		quotes[s.Symbol] = Quote{Price: 100, Previous: 100}
	}

	cells := buildSectors(quotes)
	if len(cells) != 12 {
		t.Fatalf("%d cells, want 12", len(cells))
	}

	var marked int
	for _, c := range cells {
		if c.Benchmark {
			marked++
		}
	}
	if marked != 1 {
		t.Errorf("%d cells marked as the benchmark, want 1", marked)
	}

	asked := map[string]bool{}
	for _, s := range rateAndSectorSymbols() {
		asked[s] = true
	}
	if !asked[benchmark.Symbol] {
		t.Errorf("%s is on the board but never fetched", benchmark.Symbol)
	}
}

// The tab leads with whatever is still moving, the S&P while the session is
// open and bitcoin once it shuts. The closed branch is the only one visible
// most of the day, so the open one would otherwise ship unread.
func TestTabTickerFollowsTheSession(t *testing.T) {
	cards := []Card{
		{Key: "sp500", Percent: "+0.42%"},
		{Key: "bitcoin", Percent: "+1.10%"},
	}

	for _, c := range []struct{ session, want string }{
		{"regular", "S&P +0.42%"},
		{"pre", "BTC +1.10%"},
		{"post", "BTC +1.10%"},
		{"closed", "BTC +1.10%"},
	} {
		if got := tabTicker(Market{Session: c.session, Cards: cards}); got != c.want {
			t.Errorf("session %q gave %q, want %q", c.session, got, c.want)
		}
	}
}

// A card with no price must not put a bare label in the tab.
func TestTabTickerSkipsAnUnavailableCard(t *testing.T) {
	m := Market{Session: "closed", Cards: []Card{{Key: "bitcoin", Unavailable: true}}}
	if got := tabTicker(m); got != "" {
		t.Errorf("tabTicker = %q, want nothing", got)
	}
}

// The cards are read across, so the right edge of one has to mean the same hour
// as the right edge of the next. Bitcoin trades through the evening and the VIX
// stops at the bell, and without a shared end both would fill their card.
func TestStripAxisSharesOneWindowAndLeavesAFrozenCardShort(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)
	now := open.Add(11 * time.Hour)

	series := func(until time.Time) ([]float64, []int64) {
		var closes []float64
		var times []int64
		for at := open.AddDate(0, 0, -1); !at.After(until); at = at.Add(15 * time.Minute) {
			closes = append(closes, 100)
			times = append(times, at.Unix())
		}
		return closes, times
	}

	vixCloses, vixTimes := series(time.Date(2026, 8, 31, 16, 0, 0, 0, et))
	btcCloses, btcTimes := series(now)

	quotes := map[string]Quote{
		"^VIX":    {Symbol: "^VIX", Price: 100, Previous: 100, AsOf: now, Closes: vixCloses, Times: vixTimes},
		"BTC-USD": {Symbol: "BTC-USD", Price: 100, Previous: 100, AsOf: now, Closes: btcCloses, Times: btcTimes},
	}

	cards := map[string]Card{}
	for _, c := range buildMarket(quotes, now).Cards {
		cards[c.Key] = c
	}

	if got := cards["bitcoin"].Spark.Span; got < 99 {
		t.Errorf("bitcoin is still printing and spans %v, want the full card", got)
	}
	// 4am to 8:30pm is 16.5 hours and the VIX stopped 12 hours in.
	if got := cards["vix"].Spark.Span; got < 70 || got > 75 {
		t.Errorf("the VIX stopped at 4pm and spans %v, want about 73", got)
	}
	if !cards["vix"].Spark.Closed {
		t.Error("the VIX shut at the bell and its dead stretch has to be marked")
	}
	if cards["bitcoin"].Spark.Closed {
		t.Error("bitcoin is still printing and must not be marked shut")
	}
}

// The symbols do not print on the same tick, and the futures normally trail
// bitcoin by about ten minutes, so a card is only shut once it is further behind
// than that spread.
func TestShutByIgnoresTheNormalSpreadBetweenSymbols(t *testing.T) {
	live := time.Date(2026, 8, 31, 20, 30, 0, 0, easternTime()).Unix()

	behind := func(d time.Duration) stripRow {
		return stripRow{times: []int64{live - int64(d/time.Second)}}
	}

	if behind(10 * time.Minute).shutBy(live) {
		t.Error("ten minutes behind is the usual spread, not a shut market")
	}
	if !behind(4 * time.Hour).shutBy(live) {
		t.Error("four hours behind is a market that shut")
	}
	if (stripRow{}).shutBy(live) {
		t.Error("a card with no bars cannot be judged shut")
	}
}

// A card whose last session is a different day from the rest keeps its own
// window, or a VIX frozen on Friday would be stretched across the weekend.
func TestStripAxisLeavesAStaleCardOnItsOwnSession(t *testing.T) {
	et := easternTime()
	stale := dayAxis(time.Date(2026, 8, 28, 4, 0, 0, 0, et))
	live := dayAxis(time.Date(2026, 8, 31, 4, 0, 0, 0, et))

	strip := stripAxis([]stripRow{{axis: stale}, {axis: live}})
	if strip != live {
		t.Fatalf("strip window is %v, want Monday's", strip)
	}
	if got := (stripRow{axis: stale}).axisOr(strip); got != stale {
		t.Errorf("the stale card took the strip's window, want its own Friday")
	}
	if got := (stripRow{axis: live}).axisOr(strip); got != strip {
		t.Errorf("a card on the current session should take the shared window")
	}
}

// bars is a price every five minutes from from until to, stepping by step.
func bars(from, to time.Time, price, step float64) ([]float64, []int64) {
	var closes []float64
	var times []int64
	for at := from; at.Before(to); at = at.Add(5 * time.Minute) {
		closes = append(closes, price)
		times = append(times, at.Unix())
		price += step
	}
	return closes, times
}

// The future trades at a premium to the index, so the line is drawn in percent
// off each one's own 4pm close and doesn't step at the open.
func TestIndexCardStitchesTheFutureAndCashWithoutAStep(t *testing.T) {
	et := easternTime()
	day := func(h, m int) time.Time { return time.Date(2026, 8, 31, h, m, 0, 0, et) }
	now := day(11, 0)

	// Friday ends at 100 for the index and 101 for the future, and Monday
	// opens both 1% up.
	futCloses, futTimes := bars(time.Date(2026, 8, 28, 15, 0, 0, 0, et), time.Date(2026, 8, 28, 16, 5, 0, 0, et), 101, 0)
	monCloses, monTimes := bars(day(4, 0), now, 102.01, 0)
	cashCloses, cashTimes := bars(day(9, 30), now, 101, 0)

	quotes := map[string]Quote{
		"^GSPC": {Symbol: "^GSPC", Price: 101, Previous: 100, AsOf: now, Closes: cashCloses, Times: cashTimes},
		"ES=F": {Symbol: "ES=F", Price: 102.01, AsOf: now,
			Closes: append(futCloses, monCloses...), Times: append(futTimes, monTimes...)},
	}

	rows := resolveRows(quotes, false, now, equityDay(now))
	r := rows[0]
	if r.symbol != "^GSPC" {
		t.Fatalf("the session card is %q, want the cash index", r.symbol)
	}
	for i, c := range r.closes {
		if c < 100.99 || c > 101.01 {
			t.Fatalf("bar at %v is %v, want 101 all day with no step at 9:30",
				time.Unix(r.times[i], 0).In(et), c)
		}
	}
	if first := time.Unix(r.times[0], 0).In(et); !first.Equal(day(4, 0)) {
		t.Errorf("line starts at %v, want 4am", first)
	}
}

// After 8pm the index cards hold the day's last print, line and figure both,
// until the next pre-market.
func TestIndexCardHoldsAtEightOvernight(t *testing.T) {
	et := easternTime()
	day := func(h, m int) time.Time { return time.Date(2026, 8, 31, h, m, 0, 0, et) }
	now := day(22, 0)

	prevCloses, prevTimes := bars(time.Date(2026, 8, 28, 15, 0, 0, 0, et), time.Date(2026, 8, 28, 16, 5, 0, 0, et), 100, 0)
	monCloses, monTimes := bars(day(4, 0), now, 100, 0.01)

	quotes := map[string]Quote{
		"ES=F": {Symbol: "ES=F", Price: 999, AsOf: now,
			Closes: append(prevCloses, monCloses...), Times: append(prevTimes, monTimes...)},
	}

	m := buildMarket(quotes, now)
	c := m.Cards[0]
	if c.Symbol != "ES=F" {
		t.Fatalf("overnight the card is %q, want the future", c.Symbol)
	}
	if c.Price == "999.00" {
		t.Error("the card took the live price, want the 8pm print")
	}
	if c.Note != "S&P 500 futures, 8pm" {
		t.Errorf("note %q, want it to say the figure is from 8pm", c.Note)
	}
	if c.Spark.Partial {
		t.Errorf("the held day should fill the card, span %v", c.Spark.Span)
	}
}

// ending is n bar times five minutes apart, the last at end.
func ending(end time.Time, n int) []int64 {
	out := make([]int64, n)
	for i := range n {
		out[i] = end.Add(-time.Duration(n-1-i) * 5 * time.Minute).Unix()
	}
	return out
}

// On a holiday the cash index's last print is the session before, so the index
// cards draw that session instead of an empty day.
func TestIndexCardHoldsTheLastSessionOnAHoliday(t *testing.T) {
	et := easternTime()
	wed := func(h, m int) time.Time { return time.Date(2026, 11, 25, h, m, 0, 0, et) }

	tueCloses, tueTimes := bars(time.Date(2026, 11, 24, 15, 0, 0, 0, et), time.Date(2026, 11, 24, 16, 5, 0, 0, et), 100, 0)
	futCloses, futTimes := bars(wed(4, 0), wed(20, 0), 100, 0.01)
	cashCloses, cashTimes := bars(wed(9, 30), wed(15, 55), 50, 0.01)

	quotes := map[string]Quote{
		"^GSPC": {Symbol: "^GSPC", Price: 51, Previous: 50, AsOf: wed(16, 0), Closes: cashCloses, Times: cashTimes},
		"ES=F": {Symbol: "ES=F", Price: 103, AsOf: wed(20, 0),
			Closes: append(tueCloses, futCloses...), Times: append(tueTimes, futTimes...)},
	}

	for _, clock := range []string{"2026-11-26 11:45", "2026-11-26 17:00", "2026-11-26 22:00"} {
		m := buildMarket(quotes, at(t, clock))
		if m.Session != "closed" {
			t.Errorf("%s: session %q, want closed", clock, m.Session)
		}
		if c := m.Cards[0]; c.Spark.Points < sparkFloor {
			t.Errorf("%s: the S&P card drew %d points, want Wednesday's day", clock, c.Spark.Points)
		}
	}
}
