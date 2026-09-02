package main

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	// The runtime image is FROM scratch, so there is no /usr/share/zoneinfo in
	// it and LoadLocation("America/New_York") would fail there and nowhere
	// else. This embeds the database in the binary instead.
	_ "time/tzdata"
)

// Yahoo's spark endpoint is the one that still answers without a session.
// v7/finance/quote returns 401 Unauthorized to anything that has not carried a
// cookie and crumb through their handshake, and v8/finance/chart works but is
// one request per symbol. spark takes the whole list in a single call and hands
// back the same meta block plus the intraday closes, which is every number on
// this page for the cost of one request.
const sparkURL = "https://query1.finance.yahoo.com/v7/finance/spark"

// Instrument is one card. Cash shows during the US equity session and Future
// replaces it outside one, which is what Isaac asked for and what Yahoo itself
// does on its front page.
type Instrument struct {
	Key         string
	Label       string
	Cash        string
	Future      string
	FutureLabel string
	Decimals    int
}

// The eight cards, in display order.
//
// Gold, crude and bitcoin have no Future because Cash already is the nearly
// round the clock contract, so there is nothing to swap them to. The VIX has no
// tradeable overnight form on Yahoo, so it simply goes stale after the close
// and says so rather than pretending.
//
// ^IXIC is the Nasdaq Composite and NQ=F is the Nasdaq 100. Different baskets,
// so the two halves of that card will not agree to the basis point across a
// session boundary. Yahoo pairs them the same way, and "Nasdaq" means the
// Composite to most people, so the futures label names the index it actually is.
var instruments = []Instrument{
	{Key: "sp500", Label: "S&P 500", Cash: "^GSPC", Future: "ES=F", FutureLabel: "S&P 500 futures", Decimals: 2},
	{Key: "dow", Label: "Dow 30", Cash: "^DJI", Future: "YM=F", FutureLabel: "Dow futures", Decimals: 2},
	{Key: "nasdaq", Label: "Nasdaq", Cash: "^IXIC", Future: "NQ=F", FutureLabel: "Nasdaq 100 futures", Decimals: 2},
	{Key: "russell", Label: "Russell 2000", Cash: "^RUT", Future: "RTY=F", FutureLabel: "Russell 2000 futures", Decimals: 2},
	{Key: "vix", Label: "VIX", Cash: "^VIX", Decimals: 2},
	{Key: "gold", Label: "Gold", Cash: "GC=F", Decimals: 2},
	{Key: "oil", Label: "Crude oil", Cash: "CL=F", Decimals: 2},
	{Key: "bitcoin", Label: "Bitcoin", Cash: "BTC-USD", Decimals: 0},
}

// sparkSymbols is every symbol the poll asks for, cash and futures together.
// Both halves are fetched on every tick regardless of session so a card can
// swap the instant the clock crosses without waiting for the next poll.
func sparkSymbols() []string {
	var out []string
	for _, in := range instruments {
		out = append(out, in.Cash)
		if in.Future != "" {
			out = append(out, in.Future)
		}
	}
	return out
}

// roundClock is a symbol that keeps printing outside the New York session: the
// four index futures, gold, crude and bitcoin. They need a wider fetch and a
// previous close worked out from the bars, because Yahoo dates their day by the
// contract or by UTC and never by the exchange floor. A leading caret is every
// cash index and only cash indexes, and the sector ETFs are on the board poll
// which does not go through here.
func roundClock(symbol string) bool {
	return !strings.HasPrefix(symbol, "^")
}

// splitStrip separates the strip into the two fetches it takes. A cash index is
// quoted on exchange hours and Yahoo's own previous close is already the 4pm
// one, so those keep the single day request they have always had.
func splitStrip() (session, extended []string) {
	for _, s := range sparkSymbols() {
		if roundClock(s) {
			extended = append(extended, s)
		} else {
			session = append(session, s)
		}
	}
	return session, extended
}

type tradingWindow struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type sparkMeta struct {
	Symbol             string  `json:"symbol"`
	Currency           string  `json:"currency"`
	ShortName          string  `json:"shortName"`
	RegularMarketPrice float64 `json:"regularMarketPrice"`
	ChartPreviousClose float64 `json:"chartPreviousClose"`
	PreviousClose      float64 `json:"previousClose"`
	RegularMarketTime  int64   `json:"regularMarketTime"`
	FiftyTwoWeekHigh   float64 `json:"fiftyTwoWeekHigh"`
	FiftyTwoWeekLow    float64 `json:"fiftyTwoWeekLow"`
}

type sparkSeries struct {
	Meta       sparkMeta `json:"meta"`
	Timestamp  []int64   `json:"timestamp"`
	Indicators struct {
		Quote []struct {
			// Pointers because Yahoo writes a literal null for a bar with no
			// trade in it, and a plain float64 would silently read those as
			// zero and drop the sparkline to the axis.
			Close []*float64 `json:"close"`
		} `json:"quote"`
	} `json:"indicators"`
}

type sparkPayload struct {
	Spark struct {
		Result []struct {
			Symbol   string        `json:"symbol"`
			Response []sparkSeries `json:"response"`
		} `json:"result"`
	} `json:"spark"`
}

// Quote is one symbol reduced to what a card needs.
type Quote struct {
	Symbol   string
	Name     string
	Price    float64
	Previous float64
	AsOf     time.Time
	High52   float64
	Low52    float64
	Closes   []float64

	// Unix seconds for each close, so the sparkline can place a bar at the time
	// it printed rather than at its position in the list.
	Times []int64
}

func (q Quote) change() float64 {
	if q.Previous == 0 {
		return 0
	}
	return q.Price - q.Previous
}

func (q Quote) percent() float64 {
	if q.Previous == 0 {
		return 0
	}
	return (q.Price - q.Previous) / q.Previous * 100
}

// sparkBatch is how many symbols one spark request may carry. The endpoint
// answers 400 rather than truncating past its limit, which is how a working
// twelve symbol poll broke the moment rates and sectors took it to twenty seven.
const sparkBatch = 10

// fetchQuotes asks for every symbol the page needs, in as few requests as the
// endpoint allows. The guard paces the batches, so a poll takes a few seconds
// of wall clock and no more requests than the budget expects.
func fetchQuotes(ctx context.Context, g *Guard, symbols []string, rng string) (map[string]Quote, error) {
	out := make(map[string]Quote, len(symbols))

	var firstErr error
	for start := 0; start < len(symbols); start += sparkBatch {
		end := min(start+sparkBatch, len(symbols))

		batch, err := fetchQuoteBatch(ctx, g, symbols[start:end], rng)
		if err != nil {
			// One failed batch costs its own symbols and not the whole page,
			// and the cards it would have filled keep their previous values.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for k, v := range batch {
			out[k] = v
		}
	}

	if len(out) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("yahoo: no series in response")
	}
	return out, nil
}

// The cash indexes only ever need the day they are in. Everything else needs
// enough history to find 4pm yesterday for itself, and over a weekend that is
// three days back, so five days is the smallest range that always holds it.
const (
	sessionRange  = "1d"
	extendedRange = "5d"
)

// fetchStrip is the market poll. It goes out as two requests because the range
// is per request and the two halves of the strip need different ones, which is
// the same number of batches the one range fetch took.
func fetchStrip(ctx context.Context, g *Guard) (map[string]Quote, error) {
	session, extended := splitStrip()

	out, firstErr := fetchQuotes(ctx, g, session, sessionRange)
	if out == nil {
		out = make(map[string]Quote, len(session)+len(extended))
	}

	rest, err := fetchQuotes(ctx, g, extended, extendedRange)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	for k, v := range rest {
		out[k] = v
	}

	if len(out) == 0 {
		return nil, firstErr
	}
	return out, nil
}

func fetchQuoteBatch(ctx context.Context, g *Guard, symbols []string, rng string) (map[string]Quote, error) {
	q := url.Values{}
	q.Set("symbols", strings.Join(symbols, ","))
	q.Set("range", rng)
	q.Set("interval", "5m")
	// Extended hours bars, so a card built from a cash index still draws the
	// pre-market and after-hours tail instead of stopping at the bell.
	q.Set("includePrePost", "true")

	var payload sparkPayload
	if err := getJSON(ctx, g, "yahoo", sparkURL+"?"+q.Encode(), &payload); err != nil {
		return nil, err
	}

	out := make(map[string]Quote, len(symbols))
	for _, r := range payload.Spark.Result {
		if len(r.Response) == 0 {
			continue
		}
		s := r.Response[0]
		m := s.Meta

		prev := m.ChartPreviousClose
		if prev == 0 {
			prev = m.PreviousClose
		}

		var closes []float64
		var times []int64
		if len(s.Indicators.Quote) > 0 {
			for i, c := range s.Indicators.Quote[0].Close {
				if c == nil || math.IsNaN(*c) {
					continue
				}
				closes = append(closes, *c)
				// A null close drops its bar, so the two slices have to be
				// filled together or every point after the first gap is drawn
				// at the wrong time.
				if i < len(s.Timestamp) {
					times = append(times, s.Timestamp[i])
				}
			}
		}

		out[r.Symbol] = Quote{
			Symbol:   r.Symbol,
			Name:     m.ShortName,
			Price:    m.RegularMarketPrice,
			Previous: prev,
			AsOf:     time.Unix(m.RegularMarketTime, 0),
			High52:   m.FiftyTwoWeekHigh,
			Low52:    m.FiftyTwoWeekLow,
			Closes:   closes,
			Times:    times,
		}
	}
	return out, nil
}

// Card is one instrument as the page renders it.
type Card struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Symbol      string `json:"symbol"`
	Price       string `json:"price"`
	Change      string `json:"change"`
	Percent     string `json:"percent"`
	Direction   string `json:"direction"`
	Note        string `json:"note"`
	Spark       Spark  `json:"spark"`
	Unavailable bool   `json:"unavailable"`

	// Unexported, so it stays out of the JSON the page is patched from. It is
	// only here so one poll can tell whether the next one is the same trading
	// day. See carrySparks.
	asOf time.Time
}

// Market is the whole panel.
type Market struct {
	Cards       []Card  `json:"cards"`
	Session     string  `json:"session"`
	Phase       string  `json:"phase"`
	Drawdown    string  `json:"drawdown"`
	DrawdownPct float64 `json:"drawdown_pct"`
	Updated     string  `json:"updated"`
	Stale       bool    `json:"stale"`

	// The one figure the browser tab carries, so a backgrounded tab still says
	// what the market is doing. The S&P while the session is open and bitcoin
	// once it shuts, since bitcoin is the one on this page that never stops.
	Ticker string `json:"ticker"`
}

// stripRow is one card between picking its symbol and drawing it, which takes two
// passes because the window every card shares cannot be known until each has
// worked out its own.
type stripRow struct {
	in      Instrument
	symbol  string
	note    string
	quote   Quote
	closes  []float64
	times   []int64
	axis    tradingAxis
	missing bool
}

// axisOr takes the strip's shared window when this card belongs to the same
// session, and its own otherwise. A VIX that stopped at Friday's bell has no
// business being stretched over a window that runs to Monday night.
func (r stripRow) axisOr(strip tradingAxis) tradingAxis {
	if strip.ok && r.axis.ok && r.axis.start == strip.start {
		return strip
	}
	return r.axis
}

// resolveRows picks each card's symbol and puts its bars on the New York trading
// day, so the eight of them start their line at the same 9:30 and measure it
// from the same 4pm. The previous close is read before the bars are trimmed,
// since it is the print before the open they are trimmed to.
func resolveRows(quotes map[string]Quote, useFutures bool) []stripRow {
	rows := make([]stripRow, 0, len(instruments))

	for _, in := range instruments {
		r := stripRow{in: in, symbol: in.Cash}
		if useFutures && in.Future != "" {
			r.symbol, r.note = in.Future, in.FutureLabel
		}

		q, ok := quotes[r.symbol]
		if !ok || q.Price == 0 {
			r.missing = true
			rows = append(rows, r)
			continue
		}

		// Anything with no overnight form freezes at the close, so say when the
		// number is from rather than showing a stale figure as a live one.
		if useFutures && in.Future == "" && in.Key == "vix" {
			r.note = "as of " + q.AsOf.In(easternTime()).Format("Mon 3:04pm")
		}

		axis := sessionAxis(q.Times)
		if axis.ok && roundClock(r.symbol) {
			if prev, found := previousSessionClose(q.Closes, q.Times, time.Unix(axis.start, 0)); found {
				q.Previous = prev
			}
		}

		r.quote = q
		r.closes, r.times, r.axis = sessionBars(q.Closes, q.Times, axis)
		rows = append(rows, r)
	}
	return rows
}

// stripAxis is the one window the strip is read across: the latest open any card
// reached, and the latest bar printed against it. Without a shared end the right
// edge of a card that stopped at 4pm is a different hour than the right edge of
// the one beside it that is still trading, and reading the eight together is the
// only reason they sit in a row.
func stripAxis(rows []stripRow) tradingAxis {
	var strip tradingAxis
	for _, r := range rows {
		if r.axis.ok && r.axis.start > strip.start {
			strip = tradingAxis{start: r.axis.start, ok: true}
		}
	}
	if !strip.ok {
		return strip
	}
	for _, r := range rows {
		if r.axis.ok && r.axis.start == strip.start && r.axis.end > strip.end {
			strip.end = r.axis.end
		}
	}
	return strip
}

// buildMarket turns a quote map into the panel. now is a parameter so the
// session boundaries are testable without waiting for one.
func buildMarket(quotes map[string]Quote, now time.Time) Market {
	session, phase := equitySession(now)

	// A cash index that has not printed in half an hour during what the clock
	// calls regular hours means the clock is wrong, and the cheapest way that
	// happens is a market holiday. There is no holiday calendar here, on
	// purpose, so the quote's own age stands in for one.
	if session == "regular" {
		// A zero AsOf means Yahoo sent no regularMarketTime, which reads as
		// 1970 and would make every session look like a holiday.
		if q, ok := quotes["^GSPC"]; ok && !q.AsOf.IsZero() && now.Sub(q.AsOf) > 30*time.Minute {
			session, phase = "closed", "Holiday or halted"
		}
	}

	useFutures := session != "regular"

	rows := resolveRows(quotes, useFutures)
	axis := stripAxis(rows)

	m := Market{Session: session, Phase: phase}
	for _, r := range rows {
		if r.missing {
			m.Cards = append(m.Cards, Card{
				Key: r.in.Key, Label: r.in.Label, Symbol: r.symbol, Unavailable: true,
			})
			continue
		}

		q := r.quote
		change, pct := q.change(), q.percent()
		m.Cards = append(m.Cards, Card{
			Key:       r.in.Key,
			Label:     r.in.Label,
			Symbol:    r.symbol,
			Price:     formatNumber(q.Price, r.in.Decimals),
			Change:    signed(change, r.in.Decimals),
			Percent:   signed(pct, 2) + "%",
			Direction: direction(change),
			Note:      r.note,
			Spark:     buildSpark(r.closes, r.times, q.Previous, r.axisOr(axis)),
			asOf:      q.AsOf,
		})
	}

	m.Ticker = tabTicker(m)

	// Drawdown from the 52 week high, which is the number Isaac acts on. It is
	// the 52 week high and not the all time high because that is what the same
	// payload already carries, and the label says so.
	if q, ok := quotes["^GSPC"]; ok && q.High52 > 0 {
		m.DrawdownPct = (q.Price - q.High52) / q.High52 * 100
		m.Drawdown = fmt.Sprintf("%.1f%%", m.DrawdownPct)
	}

	return m
}

// A poll that comes back with almost no points to draw is the upstream having a
// moment, not the market: Yahoo's spark endpoint will return two closes for a
// symbol that had 287 a minute earlier. A straight line between two points is
// worse than the real shape a minute late, so the old one is carried forward.
// The price and the change on the card are always the fresh ones.
const sparkFloor = 5

func carrySparks(next, prev Market) Market {
	previous := make(map[string]Card, len(prev.Cards))
	for _, c := range prev.Cards {
		previous[c.Key] = c
	}

	for i, c := range next.Cards {
		old, ok := previous[c.Key]
		switch {
		case !ok, c.Unavailable, old.Unavailable:
			continue
		case c.Spark.Points >= sparkFloor:
			continue
		case old.Spark.Points <= c.Spark.Points:
			continue
		// A different symbol is a different instrument, and a different day is
		// a different session, so neither shape belongs on this card.
		case old.Symbol != c.Symbol, !sameTradingDay(old.asOf, c.asOf):
			continue
		}
		next.Cards[i].Spark = old.Spark
	}
	return next
}

// The day here is the trading day and not the calendar one, or every card would
// look like it had rolled over at midnight while the session ran on until 9:30.
func sameTradingDay(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	return sessionStart(a).Equal(sessionStart(b))
}

func direction(change float64) string {
	switch {
	case change > 0:
		return "up"
	case change < 0:
		return "down"
	default:
		return "flat"
	}
}

var eastern *time.Location

func easternTime() *time.Location {
	if eastern == nil {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			// Cannot happen with time/tzdata linked in, and UTC is a wrong
			// clock rather than a crashed process if it somehow does.
			loc = time.UTC
		}
		eastern = loc
	}
	return eastern
}

// equitySession is the US equity clock: pre from 4:00, regular from 9:30 to
// 16:00, post until 20:00, all New York time, weekends closed.
//
// There is no exchange holiday calendar, which is the same call finance made:
// a hardcoded list goes stale silently and a fetched one is another endpoint to
// guard. buildMarket catches the holiday case from the quote's own age instead.
func equitySession(now time.Time) (session, phase string) {
	t := now.In(easternTime())

	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return "closed", "Weekend"
	}

	mins := t.Hour()*60 + t.Minute()
	const (
		preOpen     = 4 * 60
		regularOpen = 9*60 + 30
		regularShut = 16 * 60
		postShut    = 20 * 60
	)

	switch {
	case mins < preOpen:
		return "closed", "Opens " + until(mins, preOpen)
	case mins < regularOpen:
		return "pre", "Pre-market, opens " + until(mins, regularOpen)
	case mins < regularShut:
		return "regular", "Open, closes " + until(mins, regularShut)
	case mins < postShut:
		return "post", "After hours, ends " + until(mins, postShut)
	default:
		return "closed", "Closed"
	}
}

func until(from, to int) string {
	d := to - from
	if h := d / 60; h > 0 {
		return fmt.Sprintf("in %dh %dm", h, d%60)
	}
	return fmt.Sprintf("in %dm", d)
}

// tradingAxis is the session a sparkline is drawn against, in unix seconds.
type tradingAxis struct {
	start, end int64
	ok         bool
}

// The New York trading day, which every card on the strip is now drawn against.
// The exchanges open at 9:30 and shut at 16:00, and a symbol that trades through
// the night keeps printing past the close.
const (
	sessionOpenHour = 9
	sessionOpenMin  = 30
	regularHours    = 6*time.Hour + 30*time.Minute
)

// sessionStart is the 9:30 New York morning that t belongs to, so the trading
// day runs from one open to the next rather than from midnight. Weekends get an
// open like any other day, since gold and bitcoin do not take Saturday off and
// the whole point is that every card resets at the same hour.
func sessionStart(t time.Time) time.Time {
	et := easternTime()
	y, m, d := t.In(et).Date()
	open := time.Date(y, m, d, sessionOpenHour, sessionOpenMin, 0, 0, et)
	if t.Before(open) {
		open = open.AddDate(0, 0, -1)
	}
	return open
}

// sessionAxis is the window a series is drawn against: the open it belongs to
// through the close, stretched to hold whatever printed after the bell. The
// anchor comes from the last bar rather than from the wall clock, so the VIX at
// midnight still shows the session it actually traded instead of an empty box.
func sessionAxis(times []int64) tradingAxis {
	if len(times) == 0 {
		return tradingAxis{}
	}

	last := times[len(times)-1]
	open := sessionStart(time.Unix(last, 0))
	axis := tradingAxis{start: open.Unix(), end: open.Add(regularHours).Unix(), ok: true}
	if last > axis.end {
		axis.end = last
	}
	return axis
}

// sessionBars drops everything before the open. They used to be clamped to the
// edge instead, which stacked the VIX's 3:15am prints into a vertical smear
// against the left of its card.
//
// A series with bars after the close and none inside it is a market that has
// reopened before its own next open, which is futures between Sunday evening and
// Monday morning. There is no session to lay those over, so they take the full
// width the way they always did.
func sessionBars(closes []float64, times []int64, axis tradingAxis) ([]float64, []int64, tradingAxis) {
	if !axis.ok || len(times) != len(closes) {
		return closes, times, tradingAxis{}
	}

	regularEnd := time.Unix(axis.start, 0).Add(regularHours).Unix()
	var keptCloses []float64
	var keptTimes []int64
	var inRegular int
	for i, t := range times {
		if t < axis.start {
			continue
		}
		if t <= regularEnd {
			inRegular++
		}
		keptCloses = append(keptCloses, closes[i])
		keptTimes = append(keptTimes, t)
	}

	if inRegular == 0 {
		return closes, times, tradingAxis{}
	}
	return keptCloses, keptTimes, axis
}

// previousSessionClose is the last print at or before 4pm the day before the
// open, which is what "yesterday's close" has to mean once every card is on one
// clock. Yahoo dates bitcoin's day by UTC and a future's by its contract, so
// their own previous close measures from a different moment than the S&P's and
// the eight cards disagree about what day it is. Reading it off the bars puts
// them all on the same 4pm.
func previousSessionClose(closes []float64, times []int64, open time.Time) (float64, bool) {
	cut := open.AddDate(0, 0, -1).Add(regularHours).Unix()

	var prev float64
	var found bool
	for i, t := range times {
		if t > cut {
			break
		}
		if i < len(closes) {
			prev, found = closes[i], true
		}
	}
	return prev, found
}

// tabTicker picks the figure the browser tab leads with. Labels are short
// because a tab truncates and this sits in front of the site's own name.
func tabTicker(m Market) string {
	key, label := "bitcoin", "BTC"
	if m.Session == "regular" {
		key, label = "sp500", "S&P"
	}
	for _, c := range m.Cards {
		if c.Key == key && !c.Unavailable {
			return label + " " + c.Percent
		}
	}
	return ""
}
