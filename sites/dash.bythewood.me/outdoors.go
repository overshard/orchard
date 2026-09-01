package main

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// Is the weekend worth going out in. Isaac camps and hikes, and described his
// optimal day as a cool dry one with firm ground and no wind, so that is what
// the scoring is built around rather than around a generic idea of nice
// weather. A hot still day scores badly here and would score well anywhere else.
const (
	outdoorsURL   = "https://api.open-meteo.com/v1/forecast"
	outdoorsEvery = 3 * time.Hour

	// Three days back is enough to know whether the ground has dried out, and
	// it is what the same request returns for free.
	groundDays = 3

	// Friday through Sunday, which is the shape of a trip rather than the
	// shape of a weekend.
	outlookDays = 3
)

type OutlookDay struct {
	Day     string `json:"day"`
	Date    string `json:"date"`
	Verdict string `json:"verdict"`
	Score   int    `json:"score"`

	High string `json:"high"`
	Low  string `json:"low"`

	// The four things being graded, each with the word and the nought to three
	// it scored. The word alone said what the weather was doing and left which
	// of the four ruined the day to be worked out by comparing four adjectives.
	Factors []Factor `json:"factors"`
}

type Factor struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Score int    `json:"score"`

	// The reading the word came from. FIRM and MUDDY are a judgement about
	// three days of rain and the judgement is the useful part, but the number
	// behind it is what says whether a call was close.
	Detail string `json:"detail"`
}

type Outlook struct {
	Days []OutlookDay `json:"days"`
}

type outdoorsPayload struct {
	Daily struct {
		Time         []string  `json:"time"`
		Code         []int     `json:"weather_code"`
		Max          []float64 `json:"temperature_2m_max"`
		Min          []float64 `json:"temperature_2m_min"`
		Precip       []float64 `json:"precipitation_sum"`
		PrecipChance []int     `json:"precipitation_probability_max"`
		Gusts        []float64 `json:"wind_gusts_10m_max"`
	} `json:"daily"`
}

func fetchOutlook(ctx context.Context, g *Guard, now time.Time) (Outlook, error) {
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%.2f", weatherLat))
	q.Set("longitude", fmt.Sprintf("%.2f", weatherLon))
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,precipitation_probability_max,wind_gusts_10m_max")
	q.Set("past_days", fmt.Sprint(groundDays))
	q.Set("forecast_days", "10")
	q.Set("temperature_unit", "fahrenheit")
	q.Set("wind_speed_unit", "mph")
	q.Set("precipitation_unit", "inch")
	q.Set("timezone", "America/New_York")

	var payload outdoorsPayload
	if err := getJSON(ctx, g, "openmeteo", outdoorsURL+"?"+q.Encode(), &payload); err != nil {
		return Outlook{}, err
	}

	d := payload.Daily
	if len(d.Time) == 0 {
		return Outlook{}, fmt.Errorf("open-meteo: no daily series")
	}

	today := now.In(easternTime()).Format("2006-01-02")
	var out Outlook

	for i, day := range d.Time {
		if len(out.Days) == outlookDays || day < today {
			continue
		}
		parsed, err := time.ParseInLocation("2006-01-02", day, easternTime())
		if err != nil {
			continue
		}
		// Friday counts, because a weekend trip leaves on one.
		switch parsed.Weekday() {
		case time.Friday, time.Saturday, time.Sunday:
		default:
			continue
		}

		// Rain over the days before this one, which is what decides whether the
		// ground is firm or a swamp. The window walks back through the past
		// days the same request returned.
		var before float64
		for j := i - groundDays; j < i; j++ {
			if j >= 0 && j < len(d.Precip) {
				before += d.Precip[j]
			}
		}

		out.Days = append(out.Days, scoreDay(parsed, nth(d.Max, i), nth(d.Min, i), nth(d.Precip, i), chance(d.PrecipChance, i), nth(d.Gusts, i), before))
	}

	if len(out.Days) == 0 {
		return Outlook{}, fmt.Errorf("open-meteo: no weekend in the forecast window")
	}
	return out, nil
}

func nth(xs []float64, i int) float64 {
	if i < len(xs) {
		return xs[i]
	}
	return 0
}

func chance(xs []int, i int) int {
	if i < len(xs) {
		return xs[i]
	}
	return 0
}

// scoreDay grades the four things Isaac named. Each contributes nought to three
// and the total decides the verdict, so one washout ruins a day on its own
// while two merely mediocre readings only make it middling.
func scoreDay(day time.Time, high, low, precip float64, pop int, gust, groundRain float64) OutlookDay {
	o := OutlookDay{
		Day:  day.Format("Mon"),
		Date: day.Format("2 Jan"),
		High: fmt.Sprintf("%.0f", high),
		Low:  fmt.Sprintf("%.0f", low),
	}

	tempScore, tempWord := gradeTemp(high)
	rainScore, rainWord := gradeRain(precip, pop)
	groundScore, groundWord := gradeGround(groundRain)
	windScore, windWord := gradeWind(gust)

	o.Factors = []Factor{
		{"TEMP", tempWord, tempScore, fmt.Sprintf("%.0f°", high)},
		{"RAIN", rainWord, rainScore, fmt.Sprintf("%d%%", pop)},
		{"GRND", groundWord, groundScore, fmt.Sprintf("%.1f\"", groundRain)},
		{"WIND", windWord, windScore, fmt.Sprintf("%.0fmph", gust)},
	}
	o.Score = tempScore + rainScore + groundScore + windScore

	switch {
	case o.Score <= 1:
		o.Verdict = "OPTIMAL"
	case o.Score <= 3:
		o.Verdict = "GOOD"
	case o.Score <= 6:
		o.Verdict = "MARGINAL"
	default:
		o.Verdict = "POOR"
	}
	return o
}

// A cool day is the good one here. Isaac's stated optimum is a fall day, so the
// band that scores zero is 55 to 75 and anything in the nineties is penalised
// as hard as anything near freezing.
func gradeTemp(high float64) (int, string) {
	switch {
	case high >= 55 && high <= 75:
		return 0, "COOL"
	case high > 75 && high <= 85:
		return 1, "WARM"
	case high >= 45 && high < 55:
		return 1, "CHILLY"
	case high > 85 && high <= 95:
		return 2, "HOT"
	case high >= 32 && high < 45:
		return 2, "COLD"
	case high > 95:
		return 3, "BAKING"
	default:
		return 3, "FREEZING"
	}
}

func gradeRain(precip float64, pop int) (int, string) {
	switch {
	case precip < 0.02 && pop < 25:
		return 0, "DRY"
	case precip < 0.1 && pop < 50:
		return 1, "MAYBE"
	case precip < 0.3:
		return 2, "WET"
	default:
		return 3, "POURING"
	}
}

// The one nobody forecasts, and the one that decides whether a trail is a trail
// or a bog. Measured as rain over the three days before, not on the day.
func gradeGround(before float64) (int, string) {
	switch {
	case before < 0.1:
		return 0, "FIRM"
	case before < 0.5:
		return 1, "DAMP"
	case before < 1.0:
		return 2, "MUDDY"
	default:
		return 3, "SOAKED"
	}
}

// Gusts rather than the average, since an average of ten with gusts to thirty
// is the day the tent goes flat.
func gradeWind(gust float64) (int, string) {
	switch {
	case gust < 15:
		return 0, "CALM"
	case gust < 25:
		return 1, "BREEZY"
	case gust < 35:
		return 2, "WINDY"
	default:
		return 3, "GALE"
	}
}
