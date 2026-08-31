package main

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// The conditions panel. Isaac sells into declines and buys back on the
// recovery, and his own read of the research is that these signals work far
// better for the buying half than the selling half, so this only ever describes
// how far the market has fallen and never suggests getting out.
//
// It is a readout, not a prediction, and the page says so. Every threshold
// below is arbitrary in the sense that no number is the right one, but they are
// the conventional ones: five percent is a dip, ten a correction, twenty a bear
// market, and a VIX in the thirties is panic.

const signalEvery = time.Hour

type Condition struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Note  string `json:"note"`
	State string `json:"state"`
	Known bool   `json:"known"`
}

type Signal struct {
	// Level is the whole panel in one word, and Headline says what it means in
	// the only terms that matter here, which is whether this is an ordinary
	// week or one of the moments worth adding into.
	Level      string      `json:"level"`
	Headline   string      `json:"headline"`
	Conditions []Condition `json:"conditions"`
}

// history is the daily series the panel needs and the 30 second poll cannot
// give it, since that one only ever asks for a single day.
type history struct {
	closes []float64
	high52 float64
}

func fetchHistory(ctx context.Context, g *Guard, symbol string) (*history, error) {
	q := url.Values{}
	q.Set("symbols", symbol)
	q.Set("range", "1y")
	q.Set("interval", "1d")

	var payload sparkPayload
	if err := getJSON(ctx, g, "yahoo", sparkURL+"?"+q.Encode(), &payload); err != nil {
		return nil, err
	}
	for _, r := range payload.Spark.Result {
		if r.Symbol != symbol || len(r.Response) == 0 || len(r.Response[0].Indicators.Quote) == 0 {
			continue
		}
		h := &history{high52: r.Response[0].Meta.FiftyTwoWeekHigh}
		for _, c := range r.Response[0].Indicators.Quote[0].Close {
			if c != nil {
				h.closes = append(h.closes, *c)
			}
		}
		if len(h.closes) < 30 {
			return nil, fmt.Errorf("yahoo: only %d daily closes for %s", len(h.closes), symbol)
		}
		return h, nil
	}
	return nil, fmt.Errorf("yahoo: no daily series for %s", symbol)
}

func mean(xs []float64) float64 {
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// buildSignal reads the conditions off the daily series and the live quotes.
func buildSignal(h *history, quotes map[string]Quote) Signal {
	var s Signal

	spx, haveSPX := quotes["^GSPC"]
	price := 0.0
	if haveSPX {
		price = spx.Price
	}
	if h != nil && price == 0 && len(h.closes) > 0 {
		price = h.closes[len(h.closes)-1]
	}

	worst := 0

	// How far below the year's high, which is the number Isaac already acts on.
	if h != nil && h.high52 > 0 && price > 0 {
		dd := (price - h.high52) / h.high52 * 100
		state, rank := band(dd, -20, -10, -5)
		worst = max(worst, rank)
		s.Conditions = append(s.Conditions, Condition{
			Label: "OFF 52W HIGH", Value: fmt.Sprintf("%.1f%%", dd),
			Note: "DRAWDOWN", State: state, Known: true,
		})
	}

	// The short shock, which is the case Isaac described: a fast drop over a
	// few days rather than a long grind down.
	if h != nil && len(h.closes) > 5 && price > 0 {
		ref := h.closes[len(h.closes)-6]
		if ref > 0 {
			five := (price - ref) / ref * 100
			state, rank := band(five, -8, -5, -3)
			worst = max(worst, rank)
			s.Conditions = append(s.Conditions, Condition{
				Label: "5 DAY", Value: signed(five, 1) + "%",
				Note: "SHORT SHOCK", State: state, Known: true,
			})
		}
	}

	// Volatility, the market's own price for insurance and the fastest of these
	// to move.
	if vix, ok := quotes["^VIX"]; ok && vix.Price > 0 {
		state, rank := band(-vix.Price, -35, -28, -22)
		worst = max(worst, rank)
		s.Conditions = append(s.Conditions, Condition{
			Label: "VIX", Value: fmt.Sprintf("%.1f", vix.Price),
			Note: vixNote(vix.Price), State: state, Known: true,
		})
	}

	// The regime. Below the 200 day average is the line most trend rules use,
	// and it is here as context rather than as a trigger: it stays red through
	// the whole of a recovery, which is exactly when Isaac is buying.
	if h != nil && len(h.closes) >= 200 && price > 0 {
		ma := mean(h.closes[len(h.closes)-200:])
		gap := (price - ma) / ma * 100
		state := "calm"
		note := "ABOVE 200DMA"
		if gap < 0 {
			state, note = "watch", "BELOW 200DMA"
		}
		s.Conditions = append(s.Conditions, Condition{
			Label: "TREND", Value: fmt.Sprintf("%+.1f%%", gap),
			Note: note, State: state, Known: true,
		})
	}

	s.Level, s.Headline = verdict(worst)
	return s
}

// band grades a value against three increasingly bad thresholds, all of which
// are negative here so that more negative is worse. It returns the state name
// and a rank so the panel can take the worst of everything it measured.
func band(v, deep, mid, mild float64) (string, int) {
	switch {
	case v <= deep:
		return "deep", 3
	case v <= mid:
		return "stress", 2
	case v <= mild:
		return "dip", 1
	default:
		return "calm", 0
	}
}

func vixNote(v float64) string {
	switch {
	case v >= 35:
		return "PANIC"
	case v >= 28:
		return "STRESSED"
	case v >= 22:
		return "ELEVATED"
	case v >= 15:
		return "NORMAL"
	default:
		return "COMPLACENT"
	}
}

// verdict is the one line at the top of the panel. It is worded as an
// observation about the market rather than as an instruction, because a
// dashboard that tells someone to buy is a dashboard that will eventually be
// wrong at the worst possible moment.
func verdict(worst int) (level, headline string) {
	switch worst {
	case 3:
		return "deep", "HISTORICALLY THE BEST DCA ZONE, AND THE HARDEST TO ACT IN"
	case 2:
		return "stress", "CORRECTION TERRITORY, THE RANGE ISAAC ADDS INTO"
	case 1:
		return "dip", "A DIP, SHALLOW BY HISTORICAL STANDARDS"
	default:
		return "calm", "NOTHING UNUSUAL, ORDINARY DCA CONDITIONS"
	}
}

// signalSymbol is the one series the panel needs beyond the market poll.
const signalSymbol = "^GSPC"
