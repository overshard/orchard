package skills

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Shared with dash. Coordinates are rounded to two places, which is all
// open-meteo's grid resolves anyway.
const (
	weatherURL   = "https://api.open-meteo.com/v1/forecast"
	weatherLat   = 36.13
	weatherLon   = -80.62
	weatherPlace = "Yadkin Valley, NC"
)

// Weather answers for one place, which is the constraint that shapes its card:
// a question naming anywhere else has to fall through rather than quietly
// answer for here.
type Weather struct{}

func (Weather) Card() Card {
	return Card{
		Name: "weather",
		Does: "reports the current conditions and the coming week's forecast for the user's own location, and only that location.",
		Fires: []string{
			"what is the weather this weekend",
			"is it going to rain tomorrow",
			"how cold is it",
			"what's the forecast",
			"do i need a jacket today",
		},
		NotFor: []string{
			"what is the weather in tokyo",
			"why does it rain",
			"what was the hottest day on record",
			"how do hurricanes form",
			"what is the climate like in arizona",
		},
		Keywords: []string{"weather", "forecast", "temperature", "rain", "snow",
			"how hot", "how cold", "how warm", "need a jacket", "need an umbrella"},
	}
}

type weatherPayload struct {
	Current struct {
		Temperature float64 `json:"temperature_2m"`
		Apparent    float64 `json:"apparent_temperature"`
		Code        int     `json:"weather_code"`
		Wind        float64 `json:"wind_speed_10m"`
	} `json:"current"`
	Daily struct {
		Time         []string  `json:"time"`
		Max          []float64 `json:"temperature_2m_max"`
		Min          []float64 `json:"temperature_2m_min"`
		Code         []int     `json:"weather_code"`
		PrecipChance []int     `json:"precipitation_probability_max"`
	} `json:"daily"`
}

func (Weather) Run(ctx context.Context, question string, d Deps) (*Result, error) {
	start := d.now()

	// Naming a different place is the one thing this cannot answer, and the
	// router is not the only guard against it, because a skill that trusts the
	// router to be right has no way of declining.
	if elsewhere(question) {
		return nil, nil
	}

	url := fmt.Sprintf("%s?latitude=%.2f&longitude=%.2f"+
		"&current=temperature_2m,apparent_temperature,weather_code,wind_speed_10m"+
		"&daily=temperature_2m_max,temperature_2m_min,weather_code,precipitation_probability_max"+
		"&temperature_unit=fahrenheit&wind_speed_unit=mph&precipitation_unit=inch"+
		"&timezone=America%%2FNew_York&forecast_days=7",
		weatherURL, weatherLat, weatherLon)

	var p weatherPayload
	if err := getJSON(ctx, d, url, &p); err != nil {
		return nil, err
	}
	if len(p.Daily.Time) == 0 {
		return nil, nil
	}

	days := 3
	if containsAny(strings.ToLower(question), "weekend", "week", "next few") {
		days = len(p.Daily.Time)
	}
	if days > len(p.Daily.Time) {
		days = len(p.Daily.Time)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%.0f°F in %s**, feels like %.0f, %s, wind %.0f mph.\n\n",
		p.Current.Temperature, weatherPlace, p.Current.Apparent,
		strings.ToLower(describeWeather(p.Current.Code)), p.Current.Wind)
	for i := 0; i < days; i++ {
		day := dayLabel(p.Daily.Time[i], i)
		rain := ""
		if i < len(p.Daily.PrecipChance) && p.Daily.PrecipChance[i] > 10 {
			rain = fmt.Sprintf(", **%d%%** chance of rain", p.Daily.PrecipChance[i])
		}
		cond := ""
		if i < len(p.Daily.Code) {
			cond = ", " + strings.ToLower(describeWeather(p.Daily.Code[i]))
		}
		fmt.Fprintf(&b, "- **%s** high **%.0f°** low **%.0f°**%s%s\n",
			day, p.Daily.Max[i], p.Daily.Min[i], cond, rain)
	}
	fmt.Fprintf(&b, "\nFrom open-meteo, read %s.", d.now().Format("3:04 PM on 2 January"))

	return &Result{
		Skill: "weather", Shape: "factual", Text: b.String(),
		Sources: []Source{{URL: "https://open-meteo.com/", Title: "open-meteo", Site: "open-meteo.com"}},
		Elapsed: d.now().Sub(start).Round(10 * time.Millisecond).String(),
	}, nil
}

// elsewhere spots a question about a place other than home. "in" and "at" are
// the giveaways, and the exceptions are the phrasings where they do not name a
// place at all.
func elsewhere(q string) bool {
	l := strings.ToLower(q)
	if containsAny(l, " in the morning", " in the afternoon", " in the evening",
		" at night", " in a bit", " in an hour", " in the next") {
		return false
	}
	return containsAny(l, " in ", " at ", " for ")
}

func dayLabel(iso string, i int) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	switch i {
	case 0:
		return "Today"
	case 1:
		return "Tomorrow"
	}
	return t.Format("Monday")
}

// describeWeather turns a WMO code into words. Same table as dash.
func describeWeather(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code <= 2:
		return "Partly cloudy"
	case code == 3:
		return "Overcast"
	case code <= 48:
		return "Fog"
	case code <= 57:
		return "Drizzle"
	case code <= 67:
		return "Rain"
	case code <= 77:
		return "Snow"
	case code <= 82:
		return "Showers"
	case code <= 86:
		return "Snow showers"
	default:
		return "Thunderstorms"
	}
}
