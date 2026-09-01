package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// open-meteo needs no key and no account, which is the reason it is here rather
// than any of the services that would put a credential in this repo for a panel
// showing the temperature.
const weatherURL = "https://api.open-meteo.com/v1/forecast"

type Weather struct {
	Place       string  `json:"place"`
	Temperature string  `json:"temperature"`
	Feels       string  `json:"feels"`
	High        string  `json:"high"`
	Low         string  `json:"low"`
	Rain        string  `json:"rain"`
	Condition   string  `json:"condition"`
	Wind        string  `json:"wind"`
	Sunrise     string  `json:"sunrise"`
	Sunset      string  `json:"sunset"`
	UV          string  `json:"uv"`
	UVState     string  `json:"uv_state"`
	UVFill      float64 `json:"uv_fill"`
	UVLevel     int     `json:"uv_level"`
	DayPercent  int     `json:"day_percent"`
	Unavailable bool    `json:"unavailable"`

	// The next several hours, which is the half of a forecast anyone actually
	// acts on. It comes out of the same request the rest of this panel already
	// makes, so it costs nothing.
	Hours []Hour `json:"hours"`
}

// Hour is one column of the strip. Rain is a percentage rather than a depth
// because a chance is what decides whether to take a coat.
type Hour struct {
	Label string `json:"label"`
	Temp  string `json:"temp"`
	Rain  int    `json:"rain"`
	Warm  int    `json:"warm"`
}

type weatherPayload struct {
	Current struct {
		Temperature float64 `json:"temperature_2m"`
		Apparent    float64 `json:"apparent_temperature"`
		Wind        float64 `json:"wind_speed_10m"`
		Code        int     `json:"weather_code"`
	} `json:"current"`
	Hourly struct {
		Time         []string  `json:"time"`
		Temperature  []float64 `json:"temperature_2m"`
		PrecipChance []int     `json:"precipitation_probability"`
	} `json:"hourly"`
	Daily struct {
		Max          []float64 `json:"temperature_2m_max"`
		Min          []float64 `json:"temperature_2m_min"`
		PrecipChance []int     `json:"precipitation_probability_max"`
		Sunrise      []string  `json:"sunrise"`
		Sunset       []string  `json:"sunset"`
		UVMax        []float64 `json:"uv_index_max"`
	} `json:"daily"`
}

func fetchWeather(ctx context.Context, g *Guard) (Weather, error) {
	url := fmt.Sprintf(
		"%s?latitude=%.2f&longitude=%.2f"+
			"&current=temperature_2m,apparent_temperature,weather_code,wind_speed_10m"+
			"&hourly=temperature_2m,precipitation_probability"+
			"&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max,sunrise,sunset,uv_index_max"+
			"&temperature_unit=fahrenheit&wind_speed_unit=mph&precipitation_unit=inch"+
			// Two days, because the strip runs past midnight for most of the
			// evening and one day of hours would run out at 11pm.
			"&timezone=America%%2FNew_York&forecast_days=2",
		weatherURL, weatherLat, weatherLon)

	var payload weatherPayload
	if err := getJSON(ctx, g, "openmeteo", url, &payload); err != nil {
		return Weather{Place: weatherPlace, Unavailable: true}, err
	}

	w := Weather{
		Place:       weatherPlace,
		Temperature: fmt.Sprintf("%.0f", payload.Current.Temperature),
		Feels:       fmt.Sprintf("%.0f", payload.Current.Apparent),
		Wind:        fmt.Sprintf("%.0f mph", payload.Current.Wind),
	}
	w.Condition = describeWeather(payload.Current.Code)

	if len(payload.Daily.Max) > 0 {
		w.High = fmt.Sprintf("%.0f", payload.Daily.Max[0])
	}
	if len(payload.Daily.Min) > 0 {
		w.Low = fmt.Sprintf("%.0f", payload.Daily.Min[0])
	}
	if len(payload.Daily.PrecipChance) > 0 {
		w.Rain = fmt.Sprintf("%d%%", payload.Daily.PrecipChance[0])
	}
	if len(payload.Daily.UVMax) > 0 {
		uv := payload.Daily.UVMax[0]
		w.UV = fmt.Sprintf("%.0f", uv)
		w.UVState = uvBand(uv)
		// Eleven is where the WHO scale stops naming steps and starts saying
		// extreme, so it is the top of the bar.
		w.UVFill, w.UVLevel = gauge(uv, 11, 3, 6, 8)
	}

	// open-meteo returns these as local wall clock with no offset, which is
	// what timezone=America/New_York asked for, so they are parsed in that
	// location rather than as UTC.
	if len(payload.Daily.Sunrise) > 0 && len(payload.Daily.Sunset) > 0 {
		rise, errRise := time.ParseInLocation("2006-01-02T15:04", payload.Daily.Sunrise[0], easternTime())
		set, errSet := time.ParseInLocation("2006-01-02T15:04", payload.Daily.Sunset[0], easternTime())
		if errRise == nil && errSet == nil {
			w.Sunrise = rise.Format("3:04pm")
			w.Sunset = set.Format("3:04pm")
			w.DayPercent = dayProgress(time.Now(), rise, set)
		}
	}
	w.Hours = buildHours(payload, time.Now())
	return w, nil
}

// hoursShown is how far ahead the strip looks. Eight covers the rest of an
// evening or a working day, and more than that stops being a forecast anyone
// reads off a dashboard.
const hoursShown = 8

// buildHours takes the hourly series from the hour containing now. open-meteo
// returns whole days from midnight, so most of what comes back is already past.
func buildHours(p weatherPayload, now time.Time) []Hour {
	h := p.Hourly
	if len(h.Time) == 0 {
		return nil
	}

	start := -1
	cutoff := now.In(easternTime()).Truncate(time.Hour)
	for i, at := range h.Time {
		t, err := time.ParseInLocation("2006-01-02T15:04", at, easternTime())
		if err != nil || t.Before(cutoff) {
			continue
		}
		start = i
		break
	}
	if start < 0 {
		return nil
	}

	// The strip colours its temperatures against its own range, so a mild
	// evening still shows a gradient instead of eight identical cells.
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := start; i < len(h.Temperature) && i < start+hoursShown; i++ {
		lo, hi = math.Min(lo, h.Temperature[i]), math.Max(hi, h.Temperature[i])
	}

	out := make([]Hour, 0, hoursShown)
	for i := start; i < len(h.Time) && len(out) < hoursShown; i++ {
		t, err := time.ParseInLocation("2006-01-02T15:04", h.Time[i], easternTime())
		if err != nil || i >= len(h.Temperature) {
			continue
		}

		cell := Hour{
			Label: strings.ToUpper(t.Format("3PM")),
			Temp:  fmt.Sprintf("%.0f", h.Temperature[i]),
			Warm:  warmStep(h.Temperature[i], lo, hi),
		}
		if i < len(h.PrecipChance) {
			cell.Rain = h.PrecipChance[i]
		}
		out = append(out, cell)
	}
	return out
}

// warmStep buckets a temperature into four steps across the strip's own range,
// so the row reads as a gradient rather than as eight numbers.
func warmStep(v, lo, hi float64) int {
	if hi-lo < 1 {
		return 1
	}
	return int(math.Min(3, math.Max(0, (v-lo)/(hi-lo)*4)))
}

// dayProgress is how far through the daylight hours it is, clamped at both
// ends so the bar reads empty before dawn and full after dusk.
func dayProgress(now, rise, set time.Time) int {
	span := set.Sub(rise)
	if span <= 0 {
		return 0
	}
	switch pct := int(now.Sub(rise) * 100 / span); {
	case pct < 0:
		return 0
	case pct > 100:
		return 100
	default:
		return pct
	}
}

// uvBand is the WHO scale, which is what the number means anywhere else it is
// reported.
func uvBand(uv float64) string {
	switch {
	case uv < 3:
		return "LOW"
	case uv < 6:
		return "MODERATE"
	case uv < 8:
		return "HIGH"
	case uv < 11:
		return "VERY HIGH"
	default:
		return "EXTREME"
	}
}

// describeWeather maps a WMO 4677 present-weather code to words. The ranges are
// the ones open-meteo documents, collapsed to the distinctions worth making on
// a card this size. No glyph, because an emoji is the one texture that would
// give the whole page away as a web page.
func describeWeather(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code <= 2:
		return "Partly cloudy"
	case code == 3:
		return "Overcast"
	case code >= 45 && code <= 48:
		return "Fog"
	case code >= 51 && code <= 57:
		return "Drizzle"
	case code >= 61 && code <= 67:
		return "Rain"
	case code >= 71 && code <= 77:
		return "Snow"
	case code >= 80 && code <= 82:
		return "Showers"
	case code >= 85 && code <= 86:
		return "Snow showers"
	case code >= 95:
		return "Thunderstorms"
	default:
		return "Unknown"
	}
}

// weatherEvery is generous because the temperature does not move fast and this
// is the one upstream here with no interest in being polled harder.
const weatherEvery = 15 * time.Minute
