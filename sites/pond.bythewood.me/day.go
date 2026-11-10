package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// The pond follows the real day, read off dash, which already polls the
// weather, the market and the news. It's the same day in every pond.
const (
	dashState = "http://orchard-dash:8000/api/state"
	dayEvery  = 10 * time.Minute
	// A reading older than this is dropped, and the pond is an ordinary day.
	dayStale = 3 * time.Hour
)

// reading is what the pond takes from dash, and nothing more.
type reading struct {
	At      time.Time `json:"at"`
	Temp    int       `json:"temp"`
	HasTemp bool      `json:"has_temp"`
	Cond    string    `json:"cond"`
	Rain    int       `json:"rain"`
	Sunrise string    `json:"sunrise"`
	Sunset  string    `json:"sunset"`
	SP      float64   `json:"sp"`
	HasSP   bool      `json:"has_sp"`
	VIX     float64   `json:"vix"`
	Front   string    `json:"front"`
	Swarm   bool      `json:"swarm"`
	Major   bool      `json:"major"`
	Down    string    `json:"down"`
	Alert   string    `json:"alert"`
	High    int       `json:"high"`
	Low     int       `json:"low"`
	HasHL   bool      `json:"has_hl"`
	Pollen  int       `json:"pollen"`
	// Tomorrow's high, low, chance of rain and sky, for the weatherman.
	Tmr   *tomorrow `json:"tomorrow,omitempty"`
	Shows []string  `json:"shows,omitempty"`
	Games []string  `json:"games,omitempty"`
}

type tomorrow struct {
	High int    `json:"high"`
	Low  int    `json:"low"`
	Rain int    `json:"rain"`
	Cond string `json:"cond"`
}

type dashShape struct {
	Market struct {
		Cards []struct {
			Key         string `json:"key"`
			Percent     string `json:"percent"`
			Unavailable bool   `json:"unavailable"`
		} `json:"cards"`
	} `json:"market"`
	Signal struct {
		Conditions []struct {
			Label string `json:"label"`
			Value string `json:"value"`
			Known bool   `json:"known"`
		} `json:"conditions"`
	} `json:"signal"`
	Briefs struct {
		News struct {
			Read struct {
				Level string `json:"level"`
			} `json:"read"`
		} `json:"news"`
	} `json:"briefs"`
	HNPulse struct {
		Level string `json:"level"`
	} `json:"hn_pulse"`
	Weather struct {
		Temperature string `json:"temperature"`
		High        string `json:"high"`
		Low         string `json:"low"`
		Tomorrow    *struct {
			High      string `json:"high"`
			Low       string `json:"low"`
			Rain      int    `json:"rain"`
			Condition string `json:"condition"`
		} `json:"tomorrow"`
		Rain        string `json:"rain"`
		Condition   string `json:"condition"`
		Sunrise     string `json:"sunrise"`
		Sunset      string `json:"sunset"`
		Unavailable bool   `json:"unavailable"`
	} `json:"weather"`
	Air struct {
		PollenKnown bool `json:"pollen_known"`
		PollenLevel int  `json:"pollen_level"`
	} `json:"air"`
	Steam []struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	} `json:"steam"`
	Streaming []struct {
		Name string `json:"name"`
	} `json:"streaming"`
	Alerts []struct {
		Event    string `json:"event"`
		Severity string `json:"severity"`
	} `json:"alerts"`
	Systems struct {
		Rows []struct {
			Label string `json:"label"`
			State string `json:"state"`
		} `json:"rows"`
	} `json:"systems"`
	Ahead []struct {
		Label string `json:"label"`
		When  string `json:"when"`
	} `json:"ahead"`
}

func readDash(raw []byte, now time.Time) (reading, error) {
	var d dashShape
	if err := json.Unmarshal(raw, &d); err != nil {
		return reading{}, err
	}
	r := reading{At: now}
	if w := d.Weather; !w.Unavailable {
		if t, err := strconv.Atoi(w.Temperature); err == nil {
			r.Temp, r.HasTemp = t, true
		}
		r.Rain, _ = strconv.Atoi(strings.TrimSuffix(w.Rain, "%"))
		r.Cond, r.Sunrise, r.Sunset = w.Condition, w.Sunrise, w.Sunset
		hi, err1 := strconv.Atoi(w.High)
		lo, err2 := strconv.Atoi(w.Low)
		if err1 == nil && err2 == nil {
			r.High, r.Low, r.HasHL = hi, lo, true
		}
		if t := w.Tomorrow; t != nil {
			hi, err1 := strconv.Atoi(t.High)
			lo, err2 := strconv.Atoi(t.Low)
			if err1 == nil && err2 == nil {
				r.Tmr = &tomorrow{hi, lo, t.Rain, t.Condition}
			}
		}
	}
	if d.Air.PollenKnown {
		r.Pollen = d.Air.PollenLevel
	}
	for _, g := range d.Steam {
		if title, ok := fitTitle(g.Name, g.Tags); ok {
			r.Games = append(r.Games, title)
		}
	}
	for _, sh := range d.Streaming {
		if title, ok := fitTitle(sh.Name, nil); ok {
			r.Shows = append(r.Shows, title)
		}
	}
	for _, c := range d.Market.Cards {
		if c.Key == "sp500" && !c.Unavailable {
			if p, err := strconv.ParseFloat(strings.TrimSuffix(c.Percent, "%"), 64); err == nil {
				r.SP, r.HasSP = p, true
			}
		}
	}
	for _, c := range d.Signal.Conditions {
		if c.Label == "VIX" && c.Known {
			r.VIX, _ = strconv.ParseFloat(c.Value, 64)
		}
	}
	for _, a := range d.Ahead {
		if strings.HasPrefix(a.When, "TODAY") || strings.HasPrefix(a.When, "TOMORROW") {
			r.Front = a.Label + " " + strings.ToLower(strings.Fields(a.When)[0])
			break
		}
	}
	r.Swarm = d.HNPulse.Level == "hot"
	r.Major = d.Briefs.News.Read.Level == "major"
	for _, s := range d.Systems.Rows {
		if s.State == "down" {
			r.Down = s.Label
			break
		}
	}
	// A frost advisory is an alert too, and the frog only hides from the
	// kind that does damage.
	for _, a := range d.Alerts {
		if a.Severity == "Severe" || a.Severity == "Extreme" {
			r.Alert = a.Event
			break
		}
	}
	return r, nil
}

type almanac struct {
	last atomic.Pointer[reading]
	// The dev build can set a day by hand, and it holds until it's cleared.
	pinned atomic.Bool
}

func (a *almanac) poll(ctx context.Context) {
	client := &http.Client{Timeout: 8 * time.Second}
	for {
		if !a.pinned.Load() {
			if err := a.fetch(ctx, client); err != nil {
				slog.Info(fmt.Sprintf("the real day: %v", err))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(dayEvery):
		}
	}
}

func (a *almanac) fetch(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dashState, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("dash answered %d", res.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(nil, res.Body, 4<<20)).Decode(&raw); err != nil {
		return err
	}
	r, err := readDash(raw, time.Now())
	if err != nil {
		return err
	}
	a.last.Store(&r)
	return nil
}

func (a *almanac) reading(now time.Time) *reading {
	r := a.last.Load()
	if r == nil || (!a.pinned.Load() && now.Sub(r.At) > dayStale) {
		return nil
	}
	return r
}

// dayView is what the page draws and says. Every flag is a thing in the
// scene, and lines is the list under the frog saying why.
type dayView struct {
	Known     bool        `json:"known"`
	Night     bool        `json:"night"`
	Cold      bool        `json:"cold"`
	Rain      bool        `json:"rain"`
	Water     string      `json:"water"`
	Wind      int         `json:"wind"`
	Swarm     bool        `json:"swarm"`
	Dragonfly bool        `json:"dragonfly"`
	Heron     bool        `json:"heron"`
	Shelter   bool        `json:"shelter"`
	Moon      bool        `json:"moon"`
	Fireflies bool        `json:"fireflies"`
	Spawn     bool        `json:"spawn"`
	Mood      string      `json:"mood"`
	Hobby     *hobby      `json:"hobby"`
	Lines     [][2]string `json:"lines"`
}

// clockOn reads dash's "6:54am" as that time on now's day where the pond is.
func clockOn(s string, now time.Time) (time.Time, bool) {
	t, err := time.Parse("3:04pm", strings.ToLower(strings.TrimSpace(s)))
	if err != nil {
		return time.Time{}, false
	}
	n := now.In(pondZone)
	return time.Date(n.Year(), n.Month(), n.Day(), t.Hour(), t.Minute(), 0, 0, pondZone), true
}

const coldBelow = 45

func dayOfReading(r *reading, now time.Time) dayView {
	v := dayView{Mood: moodPlain, Lines: [][2]string{}}
	if r == nil {
		return v
	}
	v.Known = true
	rise, ok1 := clockOn(r.Sunrise, now)
	set, ok2 := clockOn(r.Sunset, now)
	if ok1 && ok2 {
		v.Night = now.Before(rise) || !now.Before(set)
	}
	cond := strings.ToLower(r.Cond)
	v.Rain = r.Rain >= 60 || strings.Contains(cond, "rain") || strings.Contains(cond, "shower") ||
		strings.Contains(cond, "drizzle") || strings.Contains(cond, "thunder")
	v.Cold = r.HasTemp && r.Temp < coldBelow

	if r.HasTemp {
		line := fmt.Sprintf("%d°, %s.", r.Temp, strings.ToLower(r.Cond))
		switch {
		case v.Cold && v.Night:
			line += " Cold enough that it's dozing in the mud, the way frogs do."
		case v.Cold:
			line += " Cold, so it's been dozing in the mud and came up for you."
		case v.Rain:
			line += " Rain, so the whole pond is singing."
		case v.Night:
			line += " Fireflies are out."
		}
		v.Lines = append(v.Lines, [2]string{"Weather", line})
	}
	if r.HasSP {
		switch {
		case r.SP >= 0.5:
			v.Water = "clear"
			v.Lines = append(v.Lines, [2]string{"Water", "Clear, with mayflies."})
		case r.SP <= -0.5:
			v.Water = "murky"
			v.Lines = append(v.Lines, [2]string{"Water", "Murky, and the beetles like it."})
		}
	}
	switch {
	case r.VIX >= 28:
		v.Wind = 2
		v.Lines = append(v.Lines, [2]string{"Wind", fmt.Sprintf("Gusty. The VIX is at %.0f.", r.VIX)})
	case r.VIX >= 22:
		v.Wind = 1
		v.Lines = append(v.Lines, [2]string{"Wind", fmt.Sprintf("A breeze across the water. The VIX is at %.0f.", r.VIX)})
	}
	if r.Front != "" {
		v.Lines = append(v.Lines, [2]string{"Sky", "A front is due, " + r.Front + "."})
	}
	if r.Swarm {
		v.Swarm = true
		v.Lines = append(v.Lines, [2]string{"Gnats", "A swarm, since something is blowing up on Hacker News."})
	}
	if r.Major {
		v.Dragonfly = true
		v.Lines = append(v.Lines, [2]string{"Dragonfly", "One came by, which happens on a big news day."})
	}
	if r.Down != "" {
		v.Heron = true
		v.Lines = append(v.Lines, [2]string{"Heron", "Its shadow is on the water while " + r.Down + " is down."})
	}
	if r.Alert != "" {
		v.Shelter = true
		v.Lines = append(v.Lines, [2]string{"Shelter", "Under the pad until the " + strings.ToLower(r.Alert) + " passes."})
	}
	if l := forecastLine(r.Tmr, dayOf(now)); l != "" {
		v.Lines = append(v.Lines, [2]string{"Forecast", l})
	}
	v.Mood = moodOf(r, v)
	return v
}

// season adds what the calendar brings, which needs nothing from dash.
func season(v *dayView, now time.Time) {
	n := now.In(pondZone)
	m, d := n.Month(), n.Day()
	if fullMoon(now) {
		v.Moon = true
		v.Lines = append(v.Lines, [2]string{"Moon", "Full tonight, and it lays a silver path across the water."})
	}
	switch {
	case near(now, time.March, 20):
		v.Lines = append(v.Lines, [2]string{"Calendar", "The spring equinox, as much day as night."})
	case near(now, time.June, 21):
		v.Lines = append(v.Lines, [2]string{"Calendar", "Midsummer, the longest day of the year."})
	case near(now, time.September, 22):
		v.Lines = append(v.Lines, [2]string{"Calendar", "The autumn equinox, and harvest supper on the bank."})
	case near(now, time.December, 21):
		v.Lines = append(v.Lines, [2]string{"Calendar", "Midwinter, the longest night of the year."})
	}
	if (m == time.June && d >= 5) || (m == time.July && d <= 10) {
		v.Fireflies = true
		v.Lines = append(v.Lines, [2]string{"Fireflies", "Firefly season, so after dark there are hundreds of them."})
	}
	if spawnSeason(now) {
		v.Spawn = true
		v.Lines = append(v.Lines, [2]string{"Shallows", "Frogspawn season, and the shallows are full of it."})
	}
}
