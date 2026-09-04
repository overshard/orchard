package main

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Conditions where Isaac actually is, beyond the temperature. Three upstreams,
// all free and keyless, all guarded.

const (
	// The National Weather Service asks for a contact in the User-Agent and is
	// otherwise open. This is the one panel that matters at three in the
	// morning, so it polls faster than the rest of the weather.
	alertsURL   = "https://api.weather.gov/alerts/active?point=%.2f,%.2f"
	alertsEvery = 10 * time.Minute

	airURL   = "https://air-quality-api.open-meteo.com/v1/air-quality"
	airEvery = 30 * time.Minute

	// pollen.com's own front end calls this, and it refuses a request without
	// a Referer pointing back at the matching page. Unofficial, so it is the
	// most likely thing here to break, and the panel drops the row rather than
	// the section when it does.
	pollenURL   = "https://www.pollen.com/api/forecast/current/pollen/"
	pollenZip   = "27055"
	pollenEvery = 6 * time.Hour
)

// Alert is one active NWS warning.
type Alert struct {
	Event     string `json:"event"`
	Severity  string `json:"severity"`
	Urgency   string `json:"urgency"`
	Area      string `json:"area"`
	Office    string `json:"office"`
	Headline  string `json:"headline"`
	Starts    string `json:"starts"`
	Until     string `json:"until"`
	UntilUnix int64  `json:"until_unix"`
}

type alertPayload struct {
	Features []struct {
		Properties struct {
			Event     string `json:"event"`
			Severity  string `json:"severity"`
			Urgency   string `json:"urgency"`
			Headline  string `json:"headline"`
			AreaDesc  string `json:"areaDesc"`
			Onset     string `json:"onset"`
			Effective string `json:"effective"`
			Ends      string `json:"ends"`
			Expires   string `json:"expires"`
		} `json:"properties"`
	} `json:"features"`
}

// The NWS writes every headline off one template, "<event> issued <time> until
// <time> by <office>", so beside a row that already carries the event and the
// end time it is the same sentence a third time. The office is the only part
// of it that is not already on the row.
func alertOffice(headline string) string {
	i := strings.LastIndex(headline, " by ")
	if i < 0 {
		return ""
	}
	return strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(headline[i+4:]), "NWS "))
}

// Empty for the generated template, which every watch and warning gets, and the
// text itself for the rare one where a forecaster wrote something.
func alertHeadline(event, headline string) string {
	h := strings.TrimSpace(headline)
	if h == "" || strings.HasPrefix(strings.ToUpper(h), strings.ToUpper(strings.TrimSpace(event))) {
		return ""
	}
	if i := strings.LastIndex(h, " by "); i > 0 {
		h = strings.TrimSpace(h[:i])
	}
	return h
}

// areaDesc is a semicolon separated list of every county an alert covers, which
// runs to nine names on a watch and will not fit on a phone. The home county
// leads when it is in the list, since the row is read to find out whether the
// thing is overhead, and the rest becomes a count.
func alertArea(desc string) string {
	seen := make(map[string]bool)
	names := make([]string, 0, 8)
	for _, part := range strings.Split(desc, ";") {
		n := strings.TrimSpace(part)
		if i := strings.LastIndex(n, ","); i > 0 {
			n = strings.TrimSpace(n[:i])
		}
		n = strings.ToUpper(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	if len(names) == 0 {
		return ""
	}

	lead := names[0]
	for _, n := range names {
		if n == homeCounty {
			lead = n
			break
		}
	}
	if len(names) == 1 {
		return lead
	}
	return fmt.Sprintf("%s +%d", lead, len(names)-1)
}

func fetchAlerts(ctx context.Context, g *Guard) ([]Alert, error) {
	url := fmt.Sprintf(alertsURL, weatherLat, weatherLon)

	var payload alertPayload
	if err := getJSONHeaders(ctx, g, "nws", url, map[string]string{
		// The NWS asks that automated clients identify themselves and say how
		// to be contacted about a misbehaving one.
		"User-Agent": "dash.bythewood.me (isaac@bythewood.me)",
		"Accept":     "application/geo+json",
	}, &payload); err != nil {
		return nil, err
	}

	alerts := make([]Alert, 0, len(payload.Features))
	now := time.Now()
	for _, f := range payload.Features {
		p := f.Properties
		a := Alert{
			Event:    strings.ToUpper(p.Event),
			Severity: strings.ToLower(p.Severity),
			Urgency:  strings.ToLower(p.Urgency),
			Area:     alertArea(p.AreaDesc),
			Office:   alertOffice(p.Headline),
			Headline: alertHeadline(p.Event, p.Headline),
		}

		ends := p.Ends
		if ends == "" {
			ends = p.Expires
		}
		if t, err := time.Parse(time.RFC3339, ends); err == nil {
			a.Until = strings.ToUpper(t.In(easternTime()).Format("Mon 3:04pm"))
			a.UntilUnix = t.Unix()
		}

		// A watch for this evening and one already overhead are different rows,
		// and the start time is the only thing that says which.
		onset := p.Onset
		if onset == "" {
			onset = p.Effective
		}
		if t, err := time.Parse(time.RFC3339, onset); err == nil && t.After(now) {
			a.Starts = strings.ToUpper(t.In(easternTime()).Format("Mon 3:04pm"))
		}

		alerts = append(alerts, a)
	}

	// Worst first, so a tornado warning is never below a frost advisory.
	rank := map[string]int{"extreme": 0, "severe": 1, "moderate": 2, "minor": 3}
	sort.SliceStable(alerts, func(i, j int) bool {
		ri, ok := rank[alerts[i].Severity]
		if !ok {
			ri = 4
		}
		rj, ok := rank[alerts[j].Severity]
		if !ok {
			rj = 4
		}
		return ri < rj
	})
	return alerts, nil
}

// Air is the air quality panel, and pollen when the unofficial source answers.
type Air struct {
	AQI      int    `json:"aqi"`
	AQIState string `json:"aqi_state"`
	PM25     string `json:"pm25"`
	Known    bool   `json:"known"`

	Pollen      string `json:"pollen"`
	PollenState string `json:"pollen_state"`
	PollenTop   string `json:"pollen_top"`
	PollenKnown bool   `json:"pollen_known"`

	// Each reading placed on its own scale, so UV, air quality and pollen read
	// as three of the same instrument rather than as three unrelated numbers
	// that happen to sit beside each other. Level is the severity step the
	// colour comes from.
	AQIFill     float64 `json:"aqi_fill"`
	AQILevel    int     `json:"aqi_level"`
	PollenFill  float64 `json:"pollen_fill"`
	PollenLevel int     `json:"pollen_level"`
}

// gauge places a reading on a scale that ends where the scale's own top band
// begins, and buckets it into four steps for colour.
func gauge(v, top float64, edges ...float64) (float64, int) {
	fill := math.Round(math.Min(100, math.Max(0, v/top*100))*10) / 10

	level := 0
	for _, e := range edges {
		if v >= e {
			level++
		}
	}
	return fill, level
}

type airPayload struct {
	Current struct {
		USAQI float64 `json:"us_aqi"`
		PM25  float64 `json:"pm2_5"`
	} `json:"current"`
}

func fetchAir(ctx context.Context, g *Guard) (Air, error) {
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%.2f", weatherLat))
	q.Set("longitude", fmt.Sprintf("%.2f", weatherLon))
	q.Set("current", "us_aqi,pm2_5")
	q.Set("timezone", "America/New_York")

	var payload airPayload
	if err := getJSON(ctx, g, "openmeteo", airURL+"?"+q.Encode(), &payload); err != nil {
		return Air{}, err
	}

	aqi := int(payload.Current.USAQI)
	air := Air{
		AQI:      aqi,
		AQIState: aqiBand(aqi),
		PM25:     fmt.Sprintf("%.1f", payload.Current.PM25),
		Known:    true,
	}
	// Scaled to 200 rather than to the 500 the index runs to, since anything
	// over 200 here would be smoke from a fire two states away and the bar has
	// to say something on an ordinary day.
	air.AQIFill, air.AQILevel = gauge(float64(aqi), 200, 50, 100, 150)
	return air, nil
}

// aqiBand is the EPA's own scale, which is what the number means and what every
// other reading of it will say.
func aqiBand(aqi int) string {
	switch {
	case aqi <= 50:
		return "GOOD"
	case aqi <= 100:
		return "MODERATE"
	case aqi <= 150:
		return "SENSITIVE GROUPS"
	case aqi <= 200:
		return "UNHEALTHY"
	case aqi <= 300:
		return "VERY UNHEALTHY"
	default:
		return "HAZARDOUS"
	}
}

type pollenPayload struct {
	Location struct {
		Periods []struct {
			Type     string  `json:"Type"`
			Index    float64 `json:"Index"`
			Triggers []struct {
				Name string `json:"Name"`
			} `json:"Triggers"`
		} `json:"periods"`
	} `json:"Location"`
}

// fetchPollen returns today's index. open-meteo carries pollen for Europe only,
// so there is no keyless first party source for North America and this is the
// one the pollen.com front end uses.
func fetchPollen(ctx context.Context, g *Guard) (index float64, top string, err error) {
	var payload pollenPayload
	if err := getJSONHeaders(ctx, g, "pollen", pollenURL+pollenZip, map[string]string{
		"Referer": "https://www.pollen.com/forecast/current/pollen/" + pollenZip,
	}, &payload); err != nil {
		return 0, "", err
	}

	for _, p := range payload.Location.Periods {
		if !strings.EqualFold(p.Type, "Today") {
			continue
		}
		// Two, because a third wraps the cell onto a second line and the panel
		// is a readout rather than a list of what is in the air.
		names := make([]string, 0, 2)
		for _, t := range p.Triggers {
			if len(names) == 2 {
				break
			}
			names = append(names, strings.ToUpper(t.Name))
		}
		return p.Index, strings.Join(names, " · "), nil
	}
	return 0, "", fmt.Errorf("pollen: no reading for today")
}

// pollenBand is pollen.com's own 0 to 12 scale.
func pollenBand(index float64) string {
	switch {
	case index < 2.5:
		return "LOW"
	case index < 4.9:
		return "LOW-MED"
	case index < 7.3:
		return "MEDIUM"
	case index < 9.7:
		return "MED-HIGH"
	default:
		return "HIGH"
	}
}
