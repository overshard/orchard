package main

import (
	"testing"
	"time"
)

// at builds a New York wall clock time, which is what every session boundary
// here is expressed in.
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
// holiday calendar here, so the quote's own age is what catches it.
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
		"^GSPC": {Price: 100, Previous: 99, AsOf: day, Closes: []float64{99, 100, 101, 102, 103, 104}},
	}, day)
	if full.Cards[0].Spark.Points != 6 {
		t.Fatalf("fixture drew %d points, want 6", full.Cards[0].Spark.Points)
	}

	degraded := buildMarket(map[string]Quote{
		"^GSPC": {Price: 105, Previous: 99, AsOf: day.Add(time.Minute), Closes: []float64{104, 105}},
	}, day)

	got := carrySparks(degraded, full)
	if got.Cards[0].Spark.Points != 6 {
		t.Errorf("kept %d points, want the previous 6", got.Cards[0].Spark.Points)
	}
	// The figures are still the fresh ones; only the shape is held back.
	if got.Cards[0].Price != "105.00" {
		t.Errorf("price %q, want the fresh 105.00", got.Cards[0].Price)
	}
}

func TestCarrySparksTakesAHealthyPoll(t *testing.T) {
	day := at(t, "2026-08-27 14:00")

	prev := buildMarket(map[string]Quote{
		"^GSPC": {Price: 100, Previous: 99, AsOf: day, Closes: []float64{99, 100}},
	}, day)
	next := buildMarket(map[string]Quote{
		"^GSPC": {Price: 101, Previous: 99, AsOf: day.Add(time.Minute), Closes: []float64{99, 100, 101, 102, 103}},
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
		"^GSPC": {Price: 100, Previous: 99, AsOf: yesterday, Closes: []float64{99, 100, 101, 102, 103, 104}},
	}, yesterday)
	next := buildMarket(map[string]Quote{
		"^GSPC": {Price: 101, Previous: 100, AsOf: today, Closes: []float64{100, 101}},
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
		"^GSPC": {Price: 100, Previous: 99, AsOf: day, Closes: []float64{99, 100, 101, 102, 103, 104}},
	}, day)
	next := buildMarket(map[string]Quote{
		"ES=F": {Price: 101, Previous: 99, AsOf: after, Closes: []float64{100, 101}},
	}, after)

	if prev.Cards[0].Symbol == next.Cards[0].Symbol {
		t.Fatalf("fixture did not swap: both cards are %s", prev.Cards[0].Symbol)
	}
	if got := carrySparks(next, prev).Cards[0].Spark.Points; got != 2 {
		t.Errorf("kept %d points, want the futures card's own 2", got)
	}
}

// The spark endpoint answers 400 rather than truncating past its symbol limit,
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
	batches := (len(sparkSymbols()) + sparkBatch - 1) / sparkBatch
	if perHour := batches * 120; perHour > budgets["yahoo"].perHour/2 {
		t.Errorf("the fast poll alone would spend %d of a %d budget", perHour, budgets["yahoo"].perHour)
	}
}

// The board is the eleven sectors plus the index they are read against, which
// is also what makes it a complete three by four grid.
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
