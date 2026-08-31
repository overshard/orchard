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
func fetchQuotes(ctx context.Context, g *Guard, symbols []string) (map[string]Quote, error) {
	out := make(map[string]Quote, len(symbols))

	var firstErr error
	for start := 0; start < len(symbols); start += sparkBatch {
		end := min(start+sparkBatch, len(symbols))

		batch, err := fetchQuoteBatch(ctx, g, symbols[start:end])
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

func fetchQuoteBatch(ctx context.Context, g *Guard, symbols []string) (map[string]Quote, error) {
	q := url.Values{}
	q.Set("symbols", strings.Join(symbols, ","))
	q.Set("range", "1d")
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
		if len(s.Indicators.Quote) > 0 {
			for _, c := range s.Indicators.Quote[0].Close {
				if c != nil && !math.IsNaN(*c) {
					closes = append(closes, *c)
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

	m := Market{Session: session, Phase: phase}
	for _, in := range instruments {
		symbol, label, note := in.Cash, in.Label, ""
		if useFutures && in.Future != "" {
			symbol, note = in.Future, in.FutureLabel
		}

		q, ok := quotes[symbol]
		if !ok || q.Price == 0 {
			m.Cards = append(m.Cards, Card{
				Key: in.Key, Label: label, Symbol: symbol, Unavailable: true,
			})
			continue
		}

		// Anything with no overnight form freezes at the close, so say when the
		// number is from rather than showing a stale figure as a live one.
		if useFutures && in.Future == "" && in.Key == "vix" {
			note = "as of " + q.AsOf.In(easternTime()).Format("Mon 3:04pm")
		}

		change, pct := q.change(), q.percent()
		m.Cards = append(m.Cards, Card{
			Key:       in.Key,
			Label:     label,
			Symbol:    symbol,
			Price:     formatNumber(q.Price, in.Decimals),
			Change:    signed(change, in.Decimals),
			Percent:   signed(pct, 2) + "%",
			Direction: direction(change),
			Note:      note,
			Spark:     buildSpark(q.Closes, q.Previous),
			asOf:      q.AsOf,
		})
	}

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

func sameTradingDay(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	ay, am, ad := a.In(easternTime()).Date()
	by, bm, bd := b.In(easternTime()).Date()
	return ay == by && am == bm && ad == bd
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
