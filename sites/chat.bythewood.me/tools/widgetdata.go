package tools

import (
	"context"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The readings behind a widget. These are called from the page instead of from
// a turn, so a chart can change range without the model being involved and a
// reopened conversation draws today's numbers.

// Range pairs the span with its interval, since Yahoo wants both and a year at
// five minute bars is 20,000 points nobody can see and a day at daily bars is one.
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

	// Intraday is the one range whose baseline is yesterday's close, drawn as
	// the dotted rule, instead of the first bar.
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

	// A gap in the series is a bar the exchange never printed, so it's dropped
	// instead of zeroed, since a zero would draw a spike to the floor.
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

	// The last bar is the price when the quote field is empty, as it is on an
	// index and on anything out of hours.
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

	Pollen     int    `json:"pollen"`
	PollenBand string `json:"pollen_band,omitempty"`
	PollenTop  string `json:"pollen_top,omitempty"`
	HasPollen  bool   `json:"has_pollen"`
}

// Forecast is the daily outlook plus the two readings that are not in it. The air
// call and the pollen call are separate hosts and either can be missing without
// the panel being wrong, so each failure only costs its own tile.
func Forecast(ctx context.Context, d *Deps, lat, lon float64, place string, days int) (Report, error) {
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
	rep.pollen(ctx, d, lat, lon)
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

// Forsyth County's sampler sits downtown in Winston-Salem, and its count is
// only worth showing for somewhere within a drive of it. open-meteo returns null
// pollen for every US location.
const (
	pollenURL      = "https://www.co.forsyth.nc.us/eap/pollen.aspx"
	pollenLat      = 36.0985
	pollenLon      = -80.2420
	pollenMaxMiles = 50
)

func (rep *Report) pollen(ctx context.Context, d *Deps, lat, lon float64) {
	if milesBetween(lat, lon, pollenLat, pollenLon) > pollenMaxMiles {
		return
	}
	raw, err := getWith(ctx, d, pollenURL, "text/html", map[string]string{"User-Agent": "chat.bythewood.me"})
	if err != nil {
		return
	}
	counts, err := parsePollen(string(raw), time.Now().In(newYork()))
	if err != nil {
		return
	}
	names := make([]string, 0, len(counts))
	for name, n := range counts {
		rep.Pollen = max(rep.Pollen, n)
		if n > 0 {
			names = append(names, strings.ToLower(name))
		}
	}
	sort.Strings(names)
	rep.HasPollen, rep.PollenBand, rep.PollenTop = true, pollenBand(rep.Pollen), strings.Join(names, ", ")
}

var (
	pollenCell    = regexp.MustCompile(`(?s)<t[hd][^>]*>(.*?)</t[hd]>`)
	pollenCaption = regexp.MustCompile(`(?s)<caption>(.*?)</caption>`)
	pollenDate    = regexp.MustCompile(`\d{2}/\d{2}/\d{4}`)
	pollenTag     = regexp.MustCompile(`<[^>]+>`)
)

var pollenTypes = []string{"Trees", "Grasses", "Weeds"}

// pollenTable is the table with this id, cut out of the page.
func pollenTable(page, id string) string {
	i := strings.Index(page, `id="`+id+`"`)
	if i < 0 {
		return ""
	}
	page = page[i:]
	if j := strings.Index(page, "</table>"); j >= 0 {
		page = page[:j]
	}
	return page
}

func pollenText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(pollenTag.ReplaceAllString(s, " "))), " ")
}

func pollenCells(table string) []string {
	var cells []string
	for _, m := range pollenCell.FindAllStringSubmatch(table, -1) {
		cells = append(cells, pollenText(m[1]))
	}
	return cells
}

// parsePollen reads the Pollen Rating Scale for each type on the given day. The
// count is used once it's posted for that day, and the forecast until then.
// dash reads the same page the same way.
func parsePollen(page string, day time.Time) (map[string]int, error) {
	want := day.Format("01/02/2006")

	if m := pollenCaption.FindStringSubmatch(pollenTable(page, "Graph")); m != nil {
		posted, err := time.Parse("Monday, January 2, 2006", pollenText(m[1]))
		if err == nil && posted.Format("01/02/2006") == want {
			if counts, ok := pollenRow(pollenCells(pollenTable(page, "Statistics")), 1, 0); ok {
				return counts, nil
			}
		}
	}

	cells := pollenCells(pollenTable(page, "Forecast"))
	col := -1
	for i, c := range cells {
		if pollenDate.FindString(c) == want {
			// Less one for the empty corner cell.
			col = i - 1
			break
		}
	}
	if col < 0 {
		return nil, fmt.Errorf("pollen: no reading for %s", want)
	}
	if counts, ok := pollenRow(cells, 0, col); ok {
		return counts, nil
	}
	return nil, fmt.Errorf("pollen: forecast table has no %s column", want)
}

// pollenRow finds each type's row label and reads the PRS that sits skip cells
// and then col columns after it.
func pollenRow(cells []string, skip, col int) (map[string]int, bool) {
	counts := map[string]int{}
	for i, c := range cells {
		for _, name := range pollenTypes {
			if c != name || i+1+skip+col >= len(cells) {
				continue
			}
			n, err := strconv.Atoi(cells[i+1+skip+col])
			if err != nil {
				return nil, false
			}
			counts[name] = n
		}
	}
	return counts, len(counts) == len(pollenTypes)
}

// pollenBand is Forsyth County's own Pollen Rating Scale.
func pollenBand(prs int) string {
	switch {
	case prs <= 0:
		return "absent"
	case prs <= 25:
		return "low"
	case prs <= 50:
		return "moderate"
	case prs <= 75:
		return "high"
	default:
		return "very high"
	}
}

// milesBetween is a straight line, which is all a fifty mile radius needs.
func milesBetween(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 3958.8
	rad := math.Pi / 180
	dlat, dlon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dlon/2)*math.Sin(dlon/2)
	return 2 * r * math.Asin(math.Sqrt(a))
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
