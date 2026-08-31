package main

import (
	"context"
	"fmt"
	"time"
)

// open-meteo needs no key and no account, which is the reason it is here rather
// than any of the services that would put a credential in this repo for a panel
// showing the temperature.
const weatherURL = "https://api.open-meteo.com/v1/forecast"

type Weather struct {
	Place       string `json:"place"`
	Temperature string `json:"temperature"`
	Feels       string `json:"feels"`
	High        string `json:"high"`
	Low         string `json:"low"`
	Rain        string `json:"rain"`
	Condition   string `json:"condition"`
	Wind        string `json:"wind"`
	Sunrise     string `json:"sunrise"`
	Sunset      string `json:"sunset"`
	UV          string `json:"uv"`
	UVState     string `json:"uv_state"`
	DayPercent  int    `json:"day_percent"`
	Unavailable bool   `json:"unavailable"`
}

type weatherPayload struct {
	Current struct {
		Temperature float64 `json:"temperature_2m"`
		Apparent    float64 `json:"apparent_temperature"`
		Wind        float64 `json:"wind_speed_10m"`
		Code        int     `json:"weather_code"`
	} `json:"current"`
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
			"&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max,sunrise,sunset,uv_index_max"+
			"&temperature_unit=fahrenheit&wind_speed_unit=mph&precipitation_unit=inch"+
			"&timezone=America%%2FNew_York&forecast_days=1",
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
		w.UV = fmt.Sprintf("%.0f", payload.Daily.UVMax[0])
		w.UVState = uvBand(payload.Daily.UVMax[0])
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
	return w, nil
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
