package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Some questions have a right answer sitting behind an API, and searching the
// web for them is strictly worse: slower, and it produces a paraphrase of a
// page that was itself reading the number. Weather and markets are the two
// [[dash]] already proves work keylessly, so they are the two done here, and
// the sources and their gotchas are shared with it rather than rediscovered.
//
// A skill only claims a question it is sure about. Anything ambiguous falls
// through to the ordinary pipeline, because a skill that guesses wrong is worse
// than a web search that takes ten seconds.

const (
	// Shared with dash. Coordinates are rounded to two places, which is all
	// open-meteo's grid resolves anyway.
	weatherURL   = "https://api.open-meteo.com/v1/forecast"
	weatherLat   = 36.13
	weatherLon   = -80.62
	weatherPlace = "Yadkin Valley, NC"

	// v7/finance/quote is dead: it answers 401 to anything without a cookie and
	// a crumb. spark still works unauthenticated and carries the previous close,
	// which is what a change is measured against.
	sparkURL = "https://query1.finance.yahoo.com/v7/finance/spark"
)

// TrySkill answers a question from a live source when one clearly applies.
func (e *Engine) TrySkill(ctx context.Context, question string, pr Progress) (*Answer, bool) {
	switch {
	case looksLikeWeather(question):
		pr.send("skill", "reading the forecast")
		if a, ok := e.weatherAnswer(ctx, question); ok {
			return a, true
		}
	case looksLikeMarket(question):
		if syms := marketSymbols(question); len(syms) > 0 {
			pr.send("skill", "reading the market")
			if a, ok := e.marketAnswer(ctx, question, syms); ok {
				return a, true
			}
		}
	}
	return nil, false
}

func looksLikeWeather(q string) bool {
	l := strings.ToLower(q)
	if !containsAny(l, "weather", "forecast", "temperature", "how hot", "how cold",
		"going to rain", "will it rain", "is it raining", "how warm") {
		return false
	}
	// Somewhere else is a question about a place this cannot answer for.
	return !containsAny(l, " in ", " at ", " for ")
}

func looksLikeMarket(q string) bool {
	l := strings.ToLower(q)
	return containsAny(l, "s&p", "sp500", "s and p", "dow", "nasdaq", "russell",
		"vix", "bitcoin", "btc", "ethereum", "gold price", "oil price", "stock market",
		"market doing", "markets doing")
}

// marketSymbols maps the words a person uses to Yahoo's tickers.
func marketSymbols(q string) []string {
	l := strings.ToLower(q)
	pairs := []struct {
		words  []string
		symbol string
		name   string
	}{
		{[]string{"s&p", "sp500", "s and p", "s & p"}, "^GSPC", "S&P 500"},
		{[]string{"dow"}, "^DJI", "Dow Jones"},
		{[]string{"nasdaq"}, "^IXIC", "Nasdaq"},
		{[]string{"russell"}, "^RUT", "Russell 2000"},
		{[]string{"vix", "volatility"}, "^VIX", "VIX"},
		{[]string{"bitcoin", "btc"}, "BTC-USD", "Bitcoin"},
		{[]string{"ethereum", "eth"}, "ETH-USD", "Ethereum"},
		{[]string{"gold"}, "GC=F", "Gold"},
		{[]string{"oil", "crude"}, "CL=F", "Crude oil"},
	}
	var out []string
	for _, p := range pairs {
		if containsAny(l, p.words...) {
			out = append(out, p.symbol)
		}
	}
	// "how is the stock market doing" means the indexes.
	if len(out) == 0 && containsAny(l, "stock market", "market doing", "markets doing") {
		out = []string{"^GSPC", "^DJI", "^IXIC"}
	}
	return out
}

func symbolName(sym string) string {
	return map[string]string{
		"^GSPC": "S&P 500", "^DJI": "Dow Jones", "^IXIC": "Nasdaq",
		"^RUT": "Russell 2000", "^VIX": "VIX", "BTC-USD": "Bitcoin",
		"ETH-USD": "Ethereum", "GC=F": "Gold", "CL=F": "Crude oil",
	}[sym]
}

// ---- weather ----

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

func (e *Engine) weatherAnswer(ctx context.Context, question string) (*Answer, bool) {
	start := time.Now()
	url := fmt.Sprintf("%s?latitude=%.2f&longitude=%.2f"+
		"&current=temperature_2m,apparent_temperature,weather_code,wind_speed_10m"+
		"&daily=temperature_2m_max,temperature_2m_min,weather_code,precipitation_probability_max"+
		"&temperature_unit=fahrenheit&wind_speed_unit=mph&precipitation_unit=inch"+
		"&timezone=America%%2FNew_York&forecast_days=7",
		weatherURL, weatherLat, weatherLon)

	var p weatherPayload
	if err := getJSON(ctx, e.client, url, &p); err != nil || len(p.Daily.Time) == 0 {
		return nil, false
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
	fmt.Fprintf(&b, "\nFrom open-meteo, read %s.", time.Now().Format("3:04 PM on 2 January"))

	text := b.String()
	return &Answer{
		Query: question, Standalone: question, Skill: "weather", Shape: ShapeFactual,
		Text: text, HTML: renderMarkdown(text), Support: 1,
		Sources: []Source{{N: 1, URL: "https://open-meteo.com/", Title: "open-meteo", Site: "open-meteo.com"}},
		Elapsed: time.Since(start).Round(10 * time.Millisecond).String(),
	}, true
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

// ---- markets ----

type sparkPayload struct {
	Spark struct {
		Result []struct {
			Symbol   string `json:"symbol"`
			Response []struct {
				Meta struct {
					Price     float64 `json:"regularMarketPrice"`
					PrevClose float64 `json:"chartPreviousClose"`
					Time      int64   `json:"regularMarketTime"`
					Currency  string  `json:"currency"`
				} `json:"meta"`
			} `json:"response"`
		} `json:"result"`
	} `json:"spark"`
}

func (e *Engine) marketAnswer(ctx context.Context, question string, symbols []string) (*Answer, bool) {
	start := time.Now()
	url := fmt.Sprintf("%s?symbols=%s&range=1d&interval=5m",
		sparkURL, strings.Join(symbols, ","))

	var p sparkPayload
	if err := getJSON(ctx, e.client, url, &p); err != nil || len(p.Spark.Result) == 0 {
		return nil, false
	}

	var b strings.Builder
	var newest int64
	for _, r := range p.Spark.Result {
		if len(r.Response) == 0 {
			continue
		}
		m := r.Response[0].Meta
		if m.Price == 0 || m.PrevClose == 0 {
			continue
		}
		pts := m.Price - m.PrevClose
		pct := pts / m.PrevClose * 100
		dir := "up"
		if pts < 0 {
			dir = "down"
		}
		if m.Time > newest {
			newest = m.Time
		}
		name := symbolName(r.Symbol)
		if name == "" {
			name = r.Symbol
		}
		fmt.Fprintf(&b, "- **%s** %s, %s **%s** (**%+.2f%%**) since the previous close of %s\n",
			name, formatNumber(round2(m.Price)), dir,
			formatNumber(round2(abs(pts))), pct, formatNumber(round2(m.PrevClose)))
	}
	if b.Len() == 0 {
		return nil, false
	}

	head := "Latest prices, measured against the previous close.\n\n"
	stamp := "unknown"
	if newest > 0 {
		stamp = time.Unix(newest, 0).In(localNow().Location()).Format("3:04 PM MST on 2 January 2006")
	}
	text := head + b.String() + fmt.Sprintf("\nFrom Yahoo Finance, quoted at %s.", stamp)

	return &Answer{
		Query: question, Standalone: question, Skill: "markets", Shape: ShapeFactual,
		Text: text, HTML: renderMarkdown(text), Support: 1,
		Sources: []Source{{N: 1, URL: "https://finance.yahoo.com/", Title: "Yahoo Finance", Site: "finance.yahoo.com"}},
		Elapsed: time.Since(start).Round(10 * time.Millisecond).String(),
	}, true
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// getJSON fetches and decodes. Yahoo needs a browser-like agent or it answers a
// block page instead of JSON, which is the same reason dash sends one.
func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}
