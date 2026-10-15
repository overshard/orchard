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

// spark answers without a session, where v7/finance/quote answers 401 to
// anything that hasn't been through Yahoo's cookie and crumb handshake. It
// takes every symbol in one call, v8/finance/chart one each.
const sparkURL = "https://query1.finance.yahoo.com/v7/finance/spark"

// Instrument is one card. Cash shows during the US equity session and Future
// replaces it outside one, the way Yahoo's own front page does.
type Instrument struct {
	Key         string
	Label       string
	Cash        string
	Future      string
	FutureLabel string
	Decimals    int
}

// Gold, crude and bitcoin already trade nearly round the clock and the VIX has
// no overnight form on Yahoo, so none of them has a Future. NQ=F is the Nasdaq
// 100 and not the Composite, so its label says so.
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

// roundClock is a symbol that keeps printing outside the New York session.
// Those need a wider fetch and a previous close off the bars, since Yahoo dates
// their day by the contract or by UTC. A leading caret marks a cash index.
func roundClock(symbol string) bool {
	return !strings.HasPrefix(symbol, "^")
}

// splitStrip separates the strip into its two fetches. A cash index is quoted
// on exchange hours and Yahoo's previous close for it is already the 4pm one,
// so it only needs the day it is in.
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

	// Unix seconds for each close, so the sparkline places a bar at the time it
	// printed instead of at its position in the list.
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

// sparkBatch is the most symbols one spark request may carry, since past it the
// endpoint answers 400 instead of truncating.
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

// fetchStrip is the market poll. It goes out as two requests since the range is
// per request and the two halves of the strip need different ones.
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

	// Unexported to keep it out of the JSON. carrySparks reads it to tell
	// whether the next poll is the same trading day.
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

	notices []Notice
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

	// Price known, close it is measured from not known. Different from missing,
	// which is no price at all.
	nobase bool
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

// resolveRows picks each card's symbol and puts its bars on the New York
// trading day, so every card starts at 9:30 and measures from 4pm. The previous
// close is read before trimming, since it's the print before the open.
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

		// Anything with no overnight form freezes at the close, so the card
		// says when its number is from instead of passing it off as live.
		if useFutures && in.Future == "" && in.Key == "vix" {
			r.note = "as of " + q.AsOf.In(easternTime()).Format("Mon 3:04pm")
		}

		axis := sessionAxis(q.Times)
		if n := len(q.Times); n > 0 && roundClock(r.symbol) {
			// Yahoo's own previous close on this batch is relative to the five
			// day range, so a card with no 4pm in its bars says it has no
			// baseline instead of measuring from last week.
			q.Previous, r.nobase = 0, true
			if prev, found := previousSessionClose(q.Closes, q.Times, time.Unix(q.Times[n-1], 0)); found {
				q.Previous, r.nobase = prev, false
			}
		}

		r.quote = q
		r.closes, r.times, r.axis = sessionBars(q.Closes, q.Times, axis)
		rows = append(rows, r)
	}
	return rows
}

// stripAxis is the one window the strip is read across, the latest open any
// card reached and the latest bar printed against it, so the right edge of
// every card is the same hour.
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

// stripLive is the latest bar anywhere on the strip, the edge a card has to
// reach to count as still trading. The axis end can't stand in for it, since
// through the session that's the scheduled 16:00 and every card is short of it.
func stripLive(rows []stripRow) int64 {
	var live int64
	for _, r := range rows {
		if n := len(r.times); n > 0 && r.times[n-1] > live {
			live = r.times[n-1]
		}
	}
	return live
}

// Bars are five minutes apart and the symbols do not all print on the same tick,
// so a card counts as shut only once it is further behind than that spread. Half
// an hour clears the ten minutes the futures usually trail bitcoin by.
const shutAfter = 30 * time.Minute

func (r stripRow) shutBy(live int64) bool {
	n := len(r.times)
	if live == 0 || n == 0 {
		return false
	}
	return live-r.times[n-1] > int64(shutAfter/time.Second)
}

// buildMarket turns a quote map into the panel. now is a parameter so the
// session boundaries are testable without waiting for one.
func buildMarket(quotes map[string]Quote, now time.Time) Market {
	session, phase := equitySession(now)

	// A cash index that hasn't printed in half an hour of what the clock calls
	// regular hours means the clock is wrong, usually on a market holiday, and
	// with no holiday calendar the quote's own age stands in for one.
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
	live := stripLive(rows)

	m := Market{Session: session, Phase: phase}
	for _, r := range rows {
		if r.missing {
			m.Cards = append(m.Cards, Card{
				Key: r.in.Key, Label: r.in.Label, Symbol: r.symbol, Unavailable: true,
			})
			continue
		}

		q := r.quote
		spark := buildSpark(r.closes, r.times, q.Previous, r.axisOr(axis))
		spark.Closed = r.shutBy(live) && spark.Span < sparkWidth

		change, pct := q.change(), q.percent()
		pts, percent := signed(change, r.in.Decimals), signed(pct, 2)+"%"
		// The price is real and its baseline isn't, so the card shows the one
		// and withholds the other instead of reading +0.00%.
		if r.nobase {
			pts, percent = "\u2014", ""
		}

		m.Cards = append(m.Cards, Card{
			Key:       r.in.Key,
			Label:     r.in.Label,
			Symbol:    r.symbol,
			Price:     formatNumber(q.Price, r.in.Decimals),
			Change:    pts,
			Percent:   percent,
			Direction: direction(change),
			Note:      r.note,
			Spark:     spark,
			asOf:      q.AsOf,
		})
	}

	m.Ticker = tabTicker(m)
	m.notices = moveNotices(rows, session)

	// Drawdown from the 52 week high, the number Isaac acts on. The payload
	// carries the 52 week high and not the all time one, so that's what the
	// label says.
	if q, ok := quotes["^GSPC"]; ok && q.High52 > 0 {
		m.DrawdownPct = (q.Price - q.High52) / q.High52 * 100
		m.Drawdown = fmt.Sprintf("%.1f%%", m.DrawdownPct)
	}

	return m
}

// A poll with fewer points than this is Yahoo having a moment and not the
// market, so the last shape is carried forward, since a minute late beats a
// straight line. The price and the change are always fresh.
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
			// Can't happen with time/tzdata linked in, and if it somehow does,
			// UTC is a wrong clock instead of a crashed process.
			loc = time.UTC
		}
		eastern = loc
	}
	return eastern
}

// equitySession is the US equity clock in New York time, pre from 4:00, regular
// 9:30 to 16:00, post until 20:00. A holiday calendar goes stale silently, so
// buildMarket catches a holiday from the quote's age.
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

// The New York trading day every card on the strip is drawn against. A symbol
// that trades through the night keeps printing past the close.
const (
	sessionOpenHour = 9
	sessionOpenMin  = 30
	regularHours    = 6*time.Hour + 30*time.Minute
)

// sessionStart is the 9:30 New York morning t belongs to, so the trading day
// runs open to open. Weekends get an open too, since gold and bitcoin trade on
// Saturday and every card resets at the same hour.
func sessionStart(t time.Time) time.Time {
	et := easternTime()
	y, m, d := t.In(et).Date()
	open := time.Date(y, m, d, sessionOpenHour, sessionOpenMin, 0, 0, et)
	if t.Before(open) {
		open = open.AddDate(0, 0, -1)
	}
	return open
}

// sessionAxis is the window a series is drawn against, its open through the
// close, stretched to hold whatever printed after the bell. It's anchored on
// the last bar so the VIX at midnight shows the session it traded.
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

// sessionBars drops what printed before the open instead of clamping it, which
// would smear the VIX's overnight prints up the left edge. Bars after the close
// and none inside are futures reopened Sunday evening, given the full width.
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

// lastBell is the most recent 4pm New York strictly before t. It's built from
// the open instead of a 16 so the two stay in step, and in Eastern so it lands
// on 4pm across a daylight saving change.
func lastBell(t time.Time) time.Time {
	et := easternTime()
	y, m, d := t.In(et).Date()
	bell := time.Date(y, m, d, sessionOpenHour, sessionOpenMin, 0, 0, et).Add(regularHours)
	if !t.After(bell) {
		bell = bell.AddDate(0, 0, -1)
	}
	return bell
}

// previousSessionClose is the last print at or before the latest 4pm, since
// Yahoo dates bitcoin's day by UTC and a future's by contract. The cut is the
// 4pm before the newest bar's day, so the evening futures keep the day's move.
func previousSessionClose(closes []float64, times []int64, last time.Time) (float64, bool) {
	y, m, d := last.In(easternTime()).Date()
	cut := lastBell(time.Date(y, m, d, 0, 0, 0, 0, easternTime())).Unix()

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
