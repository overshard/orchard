package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Notice is something a browser with notifications switched on should hear
// about once. The id is built from the thing itself and never from when it was
// seen, so every poll and every restart names the same event the same way and
// the browser only has to remember which ids it has already shown.
type Notice struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // market, weather, earnings, news, live
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
}

// setNotices replaces one kind's notices and leaves the rest. It builds a new
// slice rather than filtering in place, since a snapshot handed out earlier
// shares the old backing array.
func (st *State) setNotices(kind string, ns []Notice) {
	kept := make([]Notice, 0, len(st.Notices)+len(ns))
	for _, n := range st.Notices {
		if n.Kind != kind {
			kept = append(kept, n)
		}
	}
	st.Notices = append(kept, ns...)
}

// severeAlert is a watch or a warning, or anything the NWS itself grades severe.
// Advisories and statements are on the panel and stay off the phone.
func severeAlert(a Alert) bool {
	if a.cancel {
		return false
	}
	if a.Severity == "extreme" || a.Severity == "severe" {
		return true
	}
	return strings.HasSuffix(a.Event, " WARNING") || strings.HasSuffix(a.Event, " WATCH")
}

func alertNotices(alerts []Alert) []Notice {
	var out []Notice
	for _, a := range alerts {
		if !severeAlert(a) || a.key == "" {
			continue
		}
		var parts []string
		if a.Area != "" {
			parts = append(parts, a.Area)
		}
		if a.Starts != "" {
			parts = append(parts, "from "+a.Starts)
		}
		if a.Until != "" {
			parts = append(parts, "until "+a.Until)
		}
		body := strings.Join(parts, ", ")
		if a.Headline != "" {
			body = a.Headline + "\n" + body
		}
		out = append(out, Notice{ID: "weather:" + a.key, Kind: "weather", Title: a.name, Body: body})
	}
	return out
}

func onAirNotices(o OnAir) []Notice {
	if !o.Live || o.session == "" {
		return nil
	}
	return []Notice{{
		ID:    "live:" + o.session,
		Kind:  "live",
		Title: o.Name + " is live on " + o.Platform,
		Body:  o.Title,
		URL:   o.URL,
	}}
}

func earningsNotices(rows []Earning) []Notice {
	var out []Notice
	for _, r := range rows {
		if r.Verdict == "" || r.date == "" {
			continue
		}
		verb := map[string]string{"BEAT": "beat", "MISS": "missed", "MET": "met"}[r.Verdict]
		if verb == "" {
			continue
		}
		out = append(out, Notice{
			ID:    "earnings:" + r.Symbol + ":" + r.date,
			Kind:  "earnings",
			Title: r.Symbol + " " + verb,
			Body:  fmt.Sprintf("%s, %s a share against %s expected", r.Name, r.Actual, r.Forecast),
		})
	}
	return out
}

// The move on each card worth hearing about, in percent. day is measured from
// the close and swing inside the last swingWindow. Each is the first rung of a
// ladder that notifies again every half step further, so a slide from 2% to 4%
// is three notifications and not one per poll. The Dow and the Russell are left
// to the S&P and the Nasdaq, and the VIX follows the S&P rather than leading it.
var moveBands = map[string]struct{ day, swing float64 }{
	"sp500":   {2, 1.5},
	"nasdaq":  {2.5, 2},
	"gold":    {2.5, 2},
	"oil":     {5, 3},
	"bitcoin": {6, 4},
}

const swingWindow = 2 * time.Hour

// rung is how far up the ladder a move is, and 2 is the first one that counts.
func rung(pct, first float64) int {
	return int(math.Abs(pct) / (first / 2))
}

// swing is the largest move from the high or the low of the last window to the
// latest bar, signed, so a fall from a morning high reads as negative however
// the day as a whole is doing.
func swing(closes []float64, times []int64, window time.Duration) (float64, bool) {
	n := len(closes)
	if n < 2 || len(times) != n {
		return 0, false
	}
	last, from := closes[n-1], times[n-1]-int64(window/time.Second)
	hi, lo := last, last
	for i := n - 1; i >= 0 && times[i] >= from; i-- {
		hi, lo = max(hi, closes[i]), min(lo, closes[i])
	}
	if hi <= 0 || lo <= 0 {
		return 0, false
	}
	fall, rise := (last-hi)/hi*100, (last-lo)/lo*100
	if -fall > rise {
		return fall, true
	}
	return rise, true
}

func moveNotices(rows []stripRow, session string) []Notice {
	byKey := map[string]stripRow{}
	for _, r := range rows {
		byKey[r.in.Key] = r
	}

	var out []Notice
	for _, r := range rows {
		band, ok := moveBands[r.in.Key]
		n := len(r.quote.Times)
		if !ok || r.missing || n == 0 {
			continue
		}
		name := r.in.Label
		if r.symbol == r.in.Future {
			name = r.in.FutureLabel
		}
		date := time.Unix(r.quote.Times[n-1], 0).In(easternTime()).Format("2006-01-02")
		body := moveContext(r, byKey)

		if !r.nobase {
			pct := r.quote.percent()
			if step := rung(pct, band.day); step >= 2 {
				since := "since the close"
				if session == "regular" && !roundClock(r.symbol) {
					since = "today"
				}
				out = append(out, Notice{
					ID:    fmt.Sprintf("market:%s:%s:day:%s:%d", r.symbol, date, direction(pct), step),
					Kind:  "market",
					Title: fmt.Sprintf("%s %s %.1f%% %s", name, upDown(pct), math.Abs(pct), since),
					Body:  body,
				})
			}
		}

		if pct, ok := swing(r.quote.Closes, r.quote.Times, swingWindow); ok {
			if step := rung(pct, band.swing); step >= 2 {
				out = append(out, Notice{
					ID:    fmt.Sprintf("market:%s:%s:swing:%s:%d", r.symbol, date, direction(pct), step),
					Kind:  "market",
					Title: fmt.Sprintf("%s %s %.1f%% in two hours", name, upDown(pct), math.Abs(pct)),
					Body:  body,
				})
			}
		}
	}
	return out
}

func upDown(pct float64) string {
	if pct < 0 {
		return "down"
	}
	return "up"
}

// moveContext is the rest of the market beside a move, since one index falling
// alone and all four falling together are different afternoons.
func moveContext(r stripRow, byKey map[string]stripRow) string {
	switch r.in.Key {
	case "sp500", "nasdaq":
	default:
		return "Now " + formatNumber(r.quote.Price, r.in.Decimals)
	}
	var parts []string
	for _, k := range []string{"sp500", "nasdaq", "dow", "russell"} {
		c, ok := byKey[k]
		if !ok || c.missing || c.nobase {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s%%", shortName[k], signed(c.quote.percent(), 1)))
	}
	if v, ok := byKey["vix"]; ok && !v.missing {
		parts = append(parts, "VIX "+formatNumber(v.quote.Price, 1))
	}
	return strings.Join(parts, ", ")
}

var shortName = map[string]string{"sp500": "S&P", "nasdaq": "Nasdaq", "dow": "Dow", "russell": "Russell"}
