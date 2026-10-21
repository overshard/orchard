package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Notice is something a browser with notifications on should hear about once.
// The id is built from the thing and never from when it was seen, so every poll
// and every restart names an event the same way.
type Notice struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // market, weather, news, live
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
}

// setNotices replaces one kind's notices and leaves the rest. It builds a new
// slice instead of filtering in place, since a snapshot handed out earlier
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

// bigStory is the impact a story every newsroom leads on needs to be a notice,
// since a story can be everywhere and change nothing, like a celebrity's death.
const bigStory = 4

// briefNotices is TODAY's lead story on a major day. Its title and link are the
// outlet's own, so nothing the model wrote reaches the phone.
func briefNotices(b Brief) []Notice {
	r := b.Read
	if r == nil || r.Level != "major" || r.Impact < bigStory || len(r.Links) == 0 {
		return nil
	}
	lead := r.Links[0]
	return []Notice{{
		ID:    "news:" + lead.URL,
		Kind:  "news",
		Title: lead.Title,
		Body:  lead.Source + ", " + strings.ToLower(r.Spread),
		URL:   lead.URL,
	}}
}

// marketNotices is the act bars on the live cards. The S&P and the Nasdaq share
// a notice per session and direction, as a selloff takes both across within
// minutes.
func marketNotices(cards []Card, session string) []Notice {
	body := marketContext(cards)
	var out []Notice
	var ids []string
	moves := map[string][]string{}
	since := map[string]string{}
	for _, l := range marketLevels(cards) {
		c := l.card
		if l.level != levelAct || c.asOf.IsZero() {
			continue
		}
		from := movedFrom(c.asOf)
		if c.Key == "vix" {
			// A VIX that closed past the bar is still there the next morning,
			// and only the session that takes it across is news.
			was := cardFigure(c.Price) - cardFigure(c.Change)
			if was < marketBars["vix"].act {
				out = append(out, Notice{
					ID:    "market:vix:" + from.Format("2006-01-02"),
					Kind:  "market",
					Title: fmt.Sprintf("VIX up to %s from %.2f", c.Price, was),
					Body:  body,
				})
			}
			continue
		}
		id := "market:stocks:" + from.Format("2006-01-02") + ":" + upDown(l.value)
		if moves[id] == nil {
			ids = append(ids, id)
			since[id] = "since " + from.Format("Monday") + "'s close"
			if session == "regular" && !roundClock(c.Symbol) {
				since[id] = "today"
			}
		}
		moves[id] = append(moves[id], fmt.Sprintf("%s %s %.1f%%", cardName(c), upDown(l.value), math.Abs(l.value)))
	}
	for _, id := range ids {
		out = append(out, Notice{ID: id, Kind: "market", Title: strings.Join(moves[id], " and ") + " " + since[id], Body: body})
	}
	return out
}

// movedFrom is the weekday of the close a card's move is measured from, and so
// which move it is. Evening futures still carry the day just gone, and Sunday
// night's carry Friday's.
func movedFrom(asOf time.Time) time.Time {
	t := asOf.In(easternTime())
	from := lastBell(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()))
	for weekend(from) {
		from = from.AddDate(0, 0, -1)
	}
	return from
}

func cardName(c Card) string {
	for _, in := range instruments {
		if in.Key == c.Key && in.Future != "" && c.Symbol == in.Future {
			return in.FutureLabel
		}
	}
	return c.Label
}

func upDown(pct float64) string {
	if pct < 0 {
		return "down"
	}
	return "up"
}

// marketContext is the rest of the market beside a move, since one index
// falling alone and every index falling together are different afternoons.
func marketContext(cards []Card) string {
	var parts []string
	for _, c := range cards {
		name, ok := shortName[c.Key]
		switch {
		case !ok || c.Unavailable:
		case c.Key == "vix":
			parts = append(parts, name+" "+c.Price)
		case c.Percent != "":
			parts = append(parts, name+" "+c.Percent)
		}
	}
	return strings.Join(parts, ", ")
}

var shortName = map[string]string{"sp500": "S&P", "nasdaq": "Nasdaq", "dow": "Dow", "russell": "Russell", "vix": "VIX"}

const (
	levelWatch = "watch"
	levelAct   = "act"
)

// marketBar is how far a card has to go to count as watch and then act, in percent on the day, or for the VIX in its level. A bar with no
// act only ever gets to watch.
type marketBar struct {
	name       string
	watch, act float64
	level      bool
}

var marketBars = map[string]marketBar{
	"sp500": {name: "the S&P 500", watch: 1, act: 2},
	// The Nasdaq runs about a third hotter than the S&P, so a tech selloff can
	// be a big day while the S&P is only down a percent.
	"nasdaq":  {name: "the Nasdaq", watch: 1.5, act: 2.5},
	"vix":     {name: "the VIX", watch: 22, act: 30, level: true},
	"bitcoin": {name: "bitcoin", watch: 7},
}

// cardLevel is a card past its watch bar, and value is the figure the bar read.
type cardLevel struct {
	card  Card
	level string
	value float64
	why   string
}

// marketLevels reads the cards against marketBars.
func marketLevels(cards []Card) []cardLevel {
	var out []cardLevel
	for _, c := range cards {
		bar, ok := marketBars[c.Key]
		if !ok || c.Unavailable {
			continue
		}
		v := cardFigure(c.Percent)
		if bar.level {
			v = cardFigure(c.Price)
		}
		var level string
		switch {
		case bar.act > 0 && math.Abs(v) >= bar.act:
			level = levelAct
		case math.Abs(v) >= bar.watch:
			level = levelWatch
		default:
			continue
		}
		why := fmt.Sprintf("%s moved %+.2f%%", bar.name, v)
		if bar.level {
			why = fmt.Sprintf("%s is %.1f", bar.name, v)
		}
		out = append(out, cardLevel{card: c, level: level, value: v, why: why})
	}
	return out
}

// cardFigure reads a card's "+1.23%" or "7,859.75" back into a number.
func cardFigure(s string) float64 {
	v, _ := strconv.ParseFloat(strings.NewReplacer("+", "", "%", "", ",", "").Replace(s), 64)
	return v
}
