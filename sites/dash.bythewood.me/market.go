package main

import (
	"context"
	"fmt"
	"maps"
	"math"
	"net/url"
	"slices"
	"sort"
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
		maps.Copy(out, batch)
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
	maps.Copy(out, rest)

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
	base    float64
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

// resolveRows picks each card's symbol and puts its bars on the New York day,
// 4am to 8pm. The previous close is read before trimming, since it's the print
// before the day.
func resolveRows(quotes map[string]Quote, useFutures bool, now time.Time) []stripRow {
	rows := make([]stripRow, 0, len(instruments))

	for _, in := range instruments {
		if in.Future != "" {
			rows = append(rows, indexRow(in, quotes, useFutures, now))
			continue
		}

		r := stripRow{in: in, symbol: in.Cash}
		q, ok := quotes[r.symbol]
		if !ok || q.Price == 0 {
			r.missing = true
			rows = append(rows, r)
			continue
		}

		// Anything with no overnight form freezes at the close, so the card
		// says when its number is from instead of passing it off as live.
		if useFutures && in.Key == "vix" {
			r.note = "as of " + q.AsOf.In(easternTime()).Format("Mon 3:04pm")
		}

		axis := barAxis(q.Times)
		if len(q.Times) > 0 && roundClock(r.symbol) {
			// Yahoo's own previous close on this batch is relative to the five
			// day range, so a card with no 4pm in its bars says it has no
			// baseline instead of measuring from last week.
			q.Previous, r.nobase = 0, true
			if prev, found := previousSessionClose(q.Closes, q.Times, lastBell(time.Unix(axis.start, 0))); found {
				q.Previous, r.nobase = prev, false
			}
		}

		r.quote, r.base = q, q.Previous
		r.closes, r.times, r.axis = dayBars(q.Closes, q.Times, axis)
		rows = append(rows, r)
	}
	return rows
}

// indexRow is a card with a future behind it. Its line is the stitched day and
// its figure is the cash index during the session and the future outside it,
// held at 8pm through the night.
func indexRow(in Instrument, quotes map[string]Quote, useFutures bool, now time.Time) stripRow {
	r := stripRow{in: in, symbol: in.Cash}
	cash, fut := quotes[in.Cash], quotes[in.Future]
	axis := dayAxis(equityDay(now))

	futBase, futFound := previousSessionClose(fut.Closes, fut.Times, weekdayBell(time.Unix(axis.start, 0)))
	if !futFound {
		futBase = 0
	}
	r.closes, r.times = stitchDay(cash, fut, cash.Previous, futBase, axis)
	if len(r.times) > 0 {
		r.axis, r.base = axis, 100
	}

	if !useFutures && cash.Price > 0 {
		r.quote = cash
		return r
	}

	r.symbol, r.note = in.Future, in.FutureLabel
	if fut.Price == 0 {
		r.missing = true
		return r
	}

	// Past 8pm the card holds the day's last print, as the line does, and says
	// so, since the future itself is still trading.
	if now.Unix() >= axis.end {
		for i, v := range slices.Backward(fut.Times) {
			if v <= axis.end && v >= axis.start && i < len(fut.Closes) {
				fut.Price, fut.AsOf = fut.Closes[i], time.Unix(v, 0)
				r.note = in.FutureLabel + ", 8pm"
				break
			}
		}
	}

	fut.Previous, r.nobase = futBase, !futFound
	r.quote = fut
	return r
}

// stripAxis is the one window the strip is read across, the latest open any
// card reached and the latest bar printed against it, so the right edge of
// every card is the same hour.
func stripAxis(rows []stripRow) tradingAxis {
	var strip tradingAxis
	for _, r := range rows {
		if r.axis.ok && r.axis.start > strip.start {
			strip = tradingAxis{start: r.axis.start, open: r.axis.open, bell: r.axis.bell, ok: true}
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

	rows := resolveRows(quotes, useFutures, now)
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
		spark := buildSpark(r.closes, r.times, r.base, r.axisOr(axis))
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
	m.notices = marketNotices(m.Cards, session)

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

// The day here rolls at 4am and not at midnight, or every card would look like
// it had rolled over while the evening it shows was still on the card.
func sameTradingDay(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	return dayStart(a).Equal(dayStart(b))
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
	case t.Weekday() == time.Friday:
		return "closed", "Opens Monday"
	default:
		return "closed", "Opens " + until(mins, 24*60+preOpen)
	}
}

func until(from, to int) string {
	d := to - from
	if h := d / 60; h > 0 {
		return fmt.Sprintf("in %dh %dm", h, d%60)
	}
	return fmt.Sprintf("in %dm", d)
}

// tradingAxis is the day a sparkline is drawn against, in unix seconds, with
// the regular session inside it so the card can shade it.
type tradingAxis struct {
	start, end int64
	open, bell int64
	ok         bool
}

// The day every card is drawn against runs from the 4am pre-market to the 8pm
// end of after hours, New York time, the window Yahoo draws a stock's day in.
const (
	dayStartHour    = 4
	dayEndHour      = 20
	sessionOpenHour = 9
	sessionOpenMin  = 30
	sessionBellHour = 16
)

// dayStart is the 4am New York morning t belongs to, so the small hours still
// belong to the evening before. Weekends get one too, since gold and bitcoin
// trade on Saturday.
func dayStart(t time.Time) time.Time {
	et := easternTime()
	y, m, d := t.In(et).Date()
	start := time.Date(y, m, d, dayStartHour, 0, 0, 0, et)
	if t.Before(start) {
		start = start.AddDate(0, 0, -1)
	}
	return start
}

// equityDay is the weekday the index cards show at t, which overnight and over
// a weekend is still the last one, so they hold its 8pm until the next 4am.
func equityDay(t time.Time) time.Time {
	start := dayStart(t)
	for weekend(start) {
		start = start.AddDate(0, 0, -1)
	}
	return start
}

// dayAxis builds every boundary off the date in Eastern instead of adding hours,
// so each lands on its wall clock time across a daylight saving change.
func dayAxis(start time.Time) tradingAxis {
	et := easternTime()
	y, m, d := start.In(et).Date()
	at := func(h, min int) int64 { return time.Date(y, m, d, h, min, 0, 0, et).Unix() }
	return tradingAxis{
		start: at(dayStartHour, 0),
		open:  at(sessionOpenHour, sessionOpenMin),
		bell:  at(sessionBellHour, 0),
		end:   at(dayEndHour, 0),
		ok:    true,
	}
}

// barAxis is the day a series is drawn against, anchored on its last bar so
// the VIX at 6am shows the session it traded, and stretched past 8pm for
// whatever is still printing.
func barAxis(times []int64) tradingAxis {
	if len(times) == 0 {
		return tradingAxis{}
	}
	last := times[len(times)-1]
	axis := dayAxis(dayStart(time.Unix(last, 0)))
	if last > axis.end {
		axis.end = last
	}
	return axis
}

// dayBars drops what printed before the day instead of clamping it, which
// would smear yesterday up the left edge of the card.
func dayBars(closes []float64, times []int64, axis tradingAxis) ([]float64, []int64, tradingAxis) {
	if !axis.ok || len(times) != len(closes) {
		return closes, times, tradingAxis{}
	}
	var keptCloses []float64
	var keptTimes []int64
	for i, t := range times {
		if t >= axis.start {
			keptCloses = append(keptCloses, closes[i])
			keptTimes = append(keptTimes, t)
		}
	}
	if len(keptTimes) == 0 {
		return closes, times, tradingAxis{}
	}
	return keptCloses, keptTimes, axis
}

// lastBell is the most recent 4pm New York strictly before t, built in Eastern
// so it lands on 4pm across a daylight saving change.
func lastBell(t time.Time) time.Time {
	et := easternTime()
	y, m, d := t.In(et).Date()
	bell := time.Date(y, m, d, sessionBellHour, 0, 0, 0, et)
	if !t.After(bell) {
		bell = bell.AddDate(0, 0, -1)
	}
	return bell
}

// weekdayBell is lastBell skipping the weekend, since a future trades until 5pm
// Friday and its Sunday 4pm print would be Friday's 4:55.
func weekdayBell(t time.Time) time.Time {
	bell := lastBell(t)
	for weekend(bell) {
		bell = bell.AddDate(0, 0, -1)
	}
	return bell
}

// previousSessionClose is the last print at or before cut, read off the bars
// since Yahoo dates bitcoin's day by UTC and a future's by its contract.
func previousSessionClose(closes []float64, times []int64, cut time.Time) (float64, bool) {
	var prev float64
	var found bool
	for i, t := range times {
		if t > cut.Unix() {
			break
		}
		if i < len(closes) {
			prev, found = closes[i], true
		}
	}
	return prev, found
}

// stitchDay is an index card's day as one line, the future outside the session
// and the cash index in it, each a percent off its own 4pm close so the swap
// doesn't step. With no cash bars in the session the future fills it.
func stitchDay(cash, fut Quote, cashBase, futBase float64, axis tradingAxis) ([]float64, []int64) {
	useCash := false
	if cashBase > 0 {
		for _, t := range cash.Times {
			if t >= axis.open && t < axis.bell {
				useCash = true
				break
			}
		}
	}

	type bar struct {
		t int64
		v float64
	}
	var bars []bar
	if futBase > 0 {
		for i, t := range fut.Times {
			if t < axis.start || t > axis.end || i >= len(fut.Closes) {
				continue
			}
			if useCash && t >= axis.open && t < axis.bell {
				continue
			}
			bars = append(bars, bar{t, fut.Closes[i] / futBase * 100})
		}
	}
	if useCash {
		for i, t := range cash.Times {
			if t >= axis.open && t < axis.bell && i < len(cash.Closes) {
				bars = append(bars, bar{t, cash.Closes[i] / cashBase * 100})
			}
		}
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].t < bars[j].t })

	closes := make([]float64, len(bars))
	times := make([]int64, len(bars))
	for i, b := range bars {
		closes[i], times[i] = b.v, b.t
	}
	return closes, times
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
