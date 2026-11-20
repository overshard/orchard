package main

import (
	"math"
	"time"
)

// A good that came in far more or less than usual the day before makes the
// next morning's paper, and its price follows the story. Both read the same
// full day of flow, so the story always comes first.
const (
	newsHigh = 1.5
	newsLow  = 0.6
	newsBump = 0.03
	// A day's push comes in over its first hours.
	pushHours = 6
	// Pushes for one good on one day add up to no more than this.
	pushCap = 0.06
)

// What the paper says when a good's day before was big, and when it was dead.
// A big day is a glut, so it's the line that takes the price down.
var newsLines = map[string][2]string{
	"g_reeds":  {"Reed cutters can't keep up with the reeds", "Reed cutters down tools for the day"},
	"g_clay":   {"The clay pits hit a rich seam", "Nobody dug anything at the clay pits"},
	"g_fish":   {"The fishers have their best day in ages", "The nets come up empty all day"},
	"g_sticks": {"Stick gatherers bring home a forest", "A slow day for the stick gatherers"},
}

type news struct {
	Key string `json:"key"`
	// Up is the price going up, which a dead day does.
	Up   bool   `json:"up"`
	Line string `json:"line"`
}

// newsOn is the story in the paper on day, off the day before's flow.
func (b flowBook) newsOn(day string) *news {
	before := dayShift(day, -1)
	var best *news
	most := 0.0
	for _, a := range assets {
		lines, ok := newsLines[a.Key]
		if a.flow == "" || !ok {
			continue
		}
		r := b.ratio(a.flow, before, time.Time{}, true)
		if r < newsHigh && r > newsLow {
			continue
		}
		if m := math.Abs(math.Log(r)); m > most {
			best, most = &news{Key: a.Key, Up: r <= newsLow}, m
			best.Line = lines[0]
			if best.Up {
				best.Line = lines[1]
			}
		}
	}
	return best
}

// newsFor is today's story for the view, or nothing.
func newsFor(q querier, day string) (*news, error) {
	book, err := loadFlows(q, day)
	if err != nil {
		return nil, err
	}
	return book.newsOn(day), nil
}

// push is how far a day's weather, season and news move one thing on the
// board, and the line that says why.
type push struct {
	by  float64
	why string
}

type nudge struct {
	key string
	by  float64
	why string
}

// seasonNudges are the calendar's, by month.
func seasonNudges(m time.Month) []nudge {
	switch m {
	case time.December, time.January, time.February:
		return []nudge{{"g_sticks", 0.03, "Cold nights, and everybody wants a fire"}, {"g_fish", 0.02, "The fish keep to the deep in winter"}}
	case time.March, time.April, time.May:
		return []nudge{{"g_clay", 0.03, "Spring building, and every burrow wants clay"}, {"g_sticks", -0.02, "Nobody needs a fire in spring"}}
	case time.June, time.July, time.August:
		return []nudge{{"g_reeds", 0.03, "Summer roofs want reeds"}, {"g_fish", -0.02, "The fish are biting all summer"}}
	}
	return []nudge{{"g_clay", -0.03, "Burrow season is over"}, {"g_sticks", 0.02, "Stacking sticks for winter"}}
}

// weatherNudges are the day's, off the real weather.
func weatherNudges(r *reading) []nudge {
	if r == nil {
		return nil
	}
	var out []nudge
	if condHas(r, "rain", "drizzle", "shower", "thunder") || r.Tmr != nil && r.Tmr.Rain >= 60 {
		out = append(out, nudge{"g_reeds", 0.04, "Rain on the way, and the gardens want reeds"})
	}
	if condHas(r, "thunder") {
		out = append(out, nudge{"g_sticks", -0.04, "The storm brought sticks down everywhere"})
	}
	if r.HasTemp && r.Temp <= 35 {
		out = append(out, nudge{"g_fish", 0.04, "Fishing's poor in the cold"}, nudge{"g_sticks", 0.03, "A cold snap, and the fires are lit"})
	}
	if r.HasTemp && r.Temp >= 88 {
		out = append(out, nudge{"g_clay", -0.03, "Clay bakes hard in the heat"})
	}
	return out
}

// pushesOn adds up a day's nudges for each good, the paper's story with them,
// and keeps the biggest one's line as the reason.
func pushesOn(r *reading, day string, book flowBook) map[string]push {
	t, _ := time.ParseInLocation("2006-01-02", day, pondZone)
	all := append(weatherNudges(r), seasonNudges(t.Month())...)
	if n := book.newsOn(day); n != nil {
		by := -newsBump
		if n.Up {
			by = newsBump
		}
		all = append(all, nudge{n.Key, by, n.Line})
	}
	out := map[string]push{}
	most := map[string]float64{}
	for _, n := range all {
		p := out[n.key]
		p.by += n.by
		if math.Abs(n.by) > most[n.key] {
			p.why, most[n.key] = n.why, math.Abs(n.by)
		}
		out[n.key] = p
	}
	for k, p := range out {
		p.by = max(-pushCap, min(pushCap, p.by))
		out[k] = p
	}
	return out
}

// whyOf is the line under a thing's name on the board.
func whyOf(a asset, p push, day string) string {
	switch {
	case a.Kind == kindMelon && melonDay(day):
		return "Fresh off the cart, and they spoil Saturday night"
	case a.Kind == kindMelon:
		return "Spoils Saturday night, sells till then"
	case p.why != "":
		return p.why
	case a.Kind == kindCoin:
		return "Jumpy, as ever"
	}
	return "Steady"
}
