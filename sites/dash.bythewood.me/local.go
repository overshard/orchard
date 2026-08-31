package main

import (
	"context"
	"fmt"
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
	Event    string `json:"event"`
	Severity string `json:"severity"`
	Urgency  string `json:"urgency"`
	Headline string `json:"headline"`
	Until    string `json:"until"`
}

type alertPayload struct {
	Features []struct {
		Properties struct {
			Event    string `json:"event"`
			Severity string `json:"severity"`
			Urgency  string `json:"urgency"`
			Headline string `json:"headline"`
			Ends     string `json:"ends"`
			Expires  string `json:"expires"`
		} `json:"properties"`
	} `json:"features"`
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
	for _, f := range payload.Features {
		p := f.Properties
		a := Alert{
			Event:    strings.ToUpper(p.Event),
			Severity: strings.ToLower(p.Severity),
			Urgency:  strings.ToLower(p.Urgency),
			Headline: p.Headline,
		}
		ends := p.Ends
		if ends == "" {
			ends = p.Expires
		}
		if t, err := time.Parse(time.RFC3339, ends); err == nil {
			a.Until = t.In(easternTime()).Format("Mon 3:04pm")
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
	return Air{
		AQI:      aqi,
		AQIState: aqiBand(aqi),
		PM25:     fmt.Sprintf("%.1f", payload.Current.PM25),
		Known:    true,
	}, nil
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
		names := make([]string, 0, 3)
		for _, t := range p.Triggers {
			if len(names) == 3 {
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
