package tools

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

// The readings behind a widget. These are called from the page rather than from
// a turn, so a chart can change range without the model being involved and a
// reopened conversation draws today's numbers.

// Range is one of the four spans the chart offers. Yahoo wants a range and an
// interval together and the pair has to be chosen rather than derived: a year
// at five minute bars is 20,000 points nobody can see and a day at daily bars
// is one.
type Range struct {
	Key      string
	Label    string
	Range    string
	Interval string
}

var Ranges = []Range{
	{"1d", "1D", "1d", "5m"},
	{"1w", "1W", "5d", "30m"},
	{"1m", "1M", "1mo", "1d"},
	{"1y", "1Y", "1y", "1d"},
}

func RangeByKey(k string) Range {
	for _, r := range Ranges {
		if r.Key == k {
			return r
		}
	}
	return Ranges[0]
}

type Point struct {
	T int64   `json:"t"`
	C float64 `json:"c"`
}

type Series struct {
	Symbol   string  `json:"symbol"`
	Name     string  `json:"name,omitempty"`
	Currency string  `json:"currency,omitempty"`
	Range    string  `json:"range"`
	Price    float64 `json:"price"`
	Previous float64 `json:"previous"`
	Change   float64 `json:"change"`
	Percent  float64 `json:"percent"`
	Points   []Point `json:"points"`

	// Intraday is the one range whose baseline means yesterday's close rather
	// than the first bar drawn, which is what the dotted rule on the chart is.
	Intraday bool `json:"intraday"`
}

// Ticker reads one symbol over one range. Symbols arrive already in Yahoo's
// form, indexes as ^GSPC and futures as GC=F and crypto as BTC-USD, because the
// markets tool resolved them before the widget was ever recorded.
func Ticker(ctx context.Context, d *Deps, symbol, rangeKey string) (Series, error) {
	r := RangeByKey(rangeKey)
	var out chartPayload
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=%s",
		url.PathEscape(symbol), r.Range, r.Interval)
	if err := getJSON(ctx, d, u, &out); err != nil {
		return Series{}, err
	}
	return buildSeries(out, r, symbol)
}

// chartPayload is Yahoo's chart response, only the fields the panel draws.
type chartPayload struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Currency  string  `json:"currency"`
				Symbol    string  `json:"symbol"`
				Price     float64 `json:"regularMarketPrice"`
				PrevClose float64 `json:"chartPreviousClose"`
				ShortName string  `json:"shortName"`
				LongName  string  `json:"longName"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
	} `json:"chart"`
}

// buildSeries is the arithmetic, split from the fetch so the part that decides
// what a percentage means can be tested without a network.
func buildSeries(out chartPayload, r Range, symbol string) (Series, error) {
	if len(out.Chart.Result) == 0 {
		return Series{}, fmt.Errorf("no chart for %q", symbol)
	}
	res := out.Chart.Result[0]

	s := Series{
		Symbol: strings.ToUpper(symbol), Currency: res.Meta.Currency,
		Range: r.Key, Price: res.Meta.Price, Previous: res.Meta.PrevClose,
		Intraday: r.Key == "1d",
	}
	s.Name = res.Meta.LongName
	if s.Name == "" {
		s.Name = res.Meta.ShortName
	}

	// A gap in the series is a bar the exchange never printed, so it is dropped
	// rather than zeroed. A zero would draw a spike to the floor of the chart.
	if len(res.Indicators.Quote) > 0 {
		cl := res.Indicators.Quote[0].Close
		for i := range cl {
			if cl[i] == nil || i >= len(res.Timestamp) {
				continue
			}
			s.Points = append(s.Points, Point{T: res.Timestamp[i], C: round4(*cl[i])})
		}
	}
	if len(s.Points) == 0 {
		return Series{}, fmt.Errorf("no prices for %q over %s", symbol, r.Label)
	}

	// The last bar is the price when the quote field is empty, which is what
	// happens on an index and on anything out of hours.
	if s.Price == 0 {
		s.Price = s.Points[len(s.Points)-1].C
	}
	// Over a longer span the change is measured from the first bar on the
	// chart, since "up 18% this year" means against a year ago and not against
	// yesterday. Only the intraday chart is measured from the previous close.
	base := s.Previous
	if !s.Intraday || base == 0 {
		base = s.Points[0].C
		if !s.Intraday {
			s.Previous = base
		}
	}
	if base > 0 {
		s.Change = round4(s.Price - base)
		s.Percent = round2((s.Price - base) / base * 100)
	}
	return s, nil
}

// ---------------------------------------------------------------- weather

type Day struct {
	Date      string  `json:"date"`
	Weekday   string  `json:"weekday"`
	HighF     float64 `json:"high_f"`
	LowF      float64 `json:"low_f"`
	FeelsHigh float64 `json:"feels_high_f"`
	FeelsLow  float64 `json:"feels_low_f"`
	PrecipPct float64 `json:"precip_pct"`
	UV        float64 `json:"uv"`
	Code      int     `json:"code"`
	Summary   string  `json:"summary"`
}

type Report struct {
	Place string `json:"place"`
	Days  []Day  `json:"days"`

	NowF     float64 `json:"now_f"`
	FeelsF   float64 `json:"feels_f"`
	Humidity float64 `json:"humidity"`
	WindMPH  float64 `json:"wind_mph"`
	Code     int     `json:"code"`
	Summary  string  `json:"summary"`

	AQI     float64 `json:"aqi"`
	AQIBand string  `json:"aqi_band,omitempty"`
	PM25    float64 `json:"pm25"`
	HasAir  bool    `json:"has_air"`

	Pollen     float64 `json:"pollen"`
	PollenBand string  `json:"pollen_band,omitempty"`
	PollenTop  string  `json:"pollen_top,omitempty"`
	HasPollen  bool    `json:"has_pollen"`
}

// Forecast is the daily outlook plus the two readings that are not in it. The air
// call and the pollen call are separate hosts and either can be missing without
// the panel being wrong, so each failure only costs its own tile.
func Forecast(ctx context.Context, d *Deps, lat, lon float64, place, zip, country string, days int) (Report, error) {
	if days < 1 || days > 14 {
		days = 7
	}
	var w struct {
		Current struct {
			Temp     float64 `json:"temperature_2m"`
			Feels    float64 `json:"apparent_temperature"`
			Humidity float64 `json:"relative_humidity_2m"`
			Wind     float64 `json:"wind_speed_10m"`
			Code     int     `json:"weather_code"`
		} `json:"current"`
		Daily struct {
			Time     []string  `json:"time"`
			Max      []float64 `json:"temperature_2m_max"`
			Min      []float64 `json:"temperature_2m_min"`
			FeelsMax []float64 `json:"apparent_temperature_max"`
			FeelsMin []float64 `json:"apparent_temperature_min"`
			Precip   []float64 `json:"precipitation_probability_max"`
			UV       []float64 `json:"uv_index_max"`
			Code     []int     `json:"weather_code"`
		} `json:"daily"`
	}
	u := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f"+
		"&current=temperature_2m,apparent_temperature,relative_humidity_2m,wind_speed_10m,weather_code"+
		"&daily=temperature_2m_max,temperature_2m_min,apparent_temperature_max,apparent_temperature_min,"+
		"precipitation_probability_max,uv_index_max,weather_code"+
		"&temperature_unit=fahrenheit&wind_speed_unit=mph&timezone=auto&forecast_days=%d", lat, lon, days)
	if err := getJSON(ctx, d, u, &w); err != nil {
		return Report{}, err
	}

	rep := Report{Place: place, NowF: round1(w.Current.Temp), FeelsF: round1(w.Current.Feels),
		Humidity: round1(w.Current.Humidity), WindMPH: round1(w.Current.Wind),
		Code: w.Current.Code, Summary: wmo(w.Current.Code)}

	for i := range w.Daily.Time {
		dd := Day{Date: w.Daily.Time[i]}
		if t, e := time.Parse("2006-01-02", w.Daily.Time[i]); e == nil {
			dd.Weekday = t.Format("Mon")
		}
		at := func(s []float64) float64 {
			if i < len(s) {
				return round1(s[i])
			}
			return 0
		}
		dd.HighF, dd.LowF = at(w.Daily.Max), at(w.Daily.Min)
		dd.FeelsHigh, dd.FeelsLow = at(w.Daily.FeelsMax), at(w.Daily.FeelsMin)
		dd.PrecipPct, dd.UV = at(w.Daily.Precip), at(w.Daily.UV)
		if i < len(w.Daily.Code) {
			dd.Code = w.Daily.Code[i]
			dd.Summary = wmo(dd.Code)
		}
		rep.Days = append(rep.Days, dd)
	}

	rep.airQuality(ctx, d, lat, lon)
	rep.pollen(ctx, d, zip, country)
	return rep, nil
}

func (rep *Report) airQuality(ctx context.Context, d *Deps, lat, lon float64) {
	var air struct {
		Current struct {
			AQI  *float64 `json:"us_aqi"`
			PM25 *float64 `json:"pm2_5"`
		} `json:"current"`
	}
	u := fmt.Sprintf("https://air-quality-api.open-meteo.com/v1/air-quality?latitude=%f&longitude=%f"+
		"&current=us_aqi,pm2_5&timezone=auto", lat, lon)
	if err := getJSON(ctx, d, u, &air); err != nil || air.Current.AQI == nil {
		return
	}
	rep.AQI, rep.HasAir = math.Round(*air.Current.AQI), true
	rep.AQIBand = aqiBand(rep.AQI)
	if air.Current.PM25 != nil {
		rep.PM25 = round1(*air.Current.PM25)
	}
}

// pollen is pollen.com, which is the source dash settled on for the same
// reason: open-meteo carries pollen for Europe only and returns null for every
// US location. It is keyed on a zip, which the geocode already knew, and it
// refuses a request that arrives without a Referer.
func (rep *Report) pollen(ctx context.Context, d *Deps, zip, country string) {
	if zip == "" || (country != "" && country != "US") {
		return
	}
	var p struct {
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
	u := "https://www.pollen.com/api/forecast/current/pollen/" + url.PathEscape(zip)
	if err := getJSONHeaders(ctx, d, u, map[string]string{
		"Referer": "https://www.pollen.com/forecast/current/pollen/" + zip,
	}, &p); err != nil {
		return
	}
	for _, per := range p.Location.Periods {
		if !strings.EqualFold(per.Type, "Today") {
			continue
		}
		rep.Pollen, rep.HasPollen = round1(per.Index), true
		rep.PollenBand = pollenBand(per.Index)
		names := make([]string, 0, len(per.Triggers))
		for _, t := range per.Triggers {
			names = append(names, t.Name)
		}
		sort.Strings(names)
		rep.PollenTop = strings.Join(names, ", ")
		return
	}
}

// pollenBand is pollen.com's own 0 to 12 scale, the same bands dash reads it on.
func pollenBand(i float64) string {
	switch {
	case i < 2.4:
		return "low"
	case i < 4.8:
		return "low-medium"
	case i < 7.2:
		return "medium"
	case i < 9.7:
		return "medium-high"
	default:
		return "high"
	}
}

// aqiBand is the EPA's own naming for the US AQI breakpoints.
func aqiBand(v float64) string {
	switch {
	case v <= 50:
		return "good"
	case v <= 100:
		return "moderate"
	case v <= 150:
		return "unhealthy for some"
	case v <= 200:
		return "unhealthy"
	case v <= 300:
		return "very unhealthy"
	default:
		return "hazardous"
	}
}

// wmo names the weather code open-meteo answers with. The list is theirs and
// the wording is shortened to what fits a tile.
func wmo(c int) string {
	switch c {
	case 0:
		return "clear"
	case 1:
		return "mostly clear"
	case 2:
		return "partly cloudy"
	case 3:
		return "overcast"
	case 45, 48:
		return "fog"
	case 51, 53, 55:
		return "drizzle"
	case 56, 57:
		return "freezing drizzle"
	case 61, 63, 65:
		return "rain"
	case 66, 67:
		return "freezing rain"
	case 71, 73, 75:
		return "snow"
	case 77:
		return "snow grains"
	case 80, 81, 82:
		return "showers"
	case 85, 86:
		return "snow showers"
	case 95:
		return "thunderstorms"
	case 96, 99:
		return "thunderstorms, hail"
	}
	return ""
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
