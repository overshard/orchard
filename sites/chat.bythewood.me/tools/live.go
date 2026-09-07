package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------- weather

var stateNames = map[string]string{
	"nc": "North Carolina", "sc": "South Carolina", "va": "Virginia", "tn": "Tennessee",
	"ga": "Georgia", "ny": "New York", "ca": "California", "tx": "Texas", "fl": "Florida",
	"wv": "West Virginia", "ky": "Kentucky", "oh": "Ohio", "pa": "Pennsylvania", "md": "Maryland",
}

var trailingState = regexp.MustCompile(`^(.*?)[,\s]+([A-Za-z]{2})$`)

// geocode tries the obvious rewrites before giving up. Open-Meteo wants a clean
// place name and a model passes whatever the user typed, so "Yadkin Valley NC"
// misses where "Yadkin Valley, North Carolina" hits. That one missing comma was
// a real wrong answer during the bakeoff.
func geocode(ctx context.Context, d *Deps, place string) (g geo, err error) {
	cands := []string{place}
	if m := trailingState.FindStringSubmatch(place); m != nil {
		if full, ok := stateNames[strings.ToLower(m[2])]; ok {
			cands = append(cands, m[1]+", "+full, strings.TrimSpace(m[1]))
		}
	}
	if i := strings.Index(place, ","); i > 0 {
		cands = append(cands, strings.TrimSpace(place[:i]))
	}
	seen := map[string]bool{}
	for _, c := range cands {
		c = strings.TrimSpace(c)
		if c == "" || seen[strings.ToLower(c)] {
			continue
		}
		seen[strings.ToLower(c)] = true
		var out struct {
			Results []struct {
				Latitude  float64  `json:"latitude"`
				Longitude float64  `json:"longitude"`
				Name      string   `json:"name"`
				Admin1    string   `json:"admin1"`
				Country   string   `json:"country_code"`
				Postcodes []string `json:"postcodes"`
			} `json:"results"`
		}
		u := "https://geocoding-api.open-meteo.com/v1/search?count=1&language=en&name=" + url.QueryEscape(c)
		if e := getJSON(ctx, d, u, &out); e != nil {
			err = e
			continue
		}
		if len(out.Results) > 0 {
			r := out.Results[0]
			g = geo{Lat: r.Latitude, Lon: r.Longitude,
				Name:    strings.TrimSuffix(r.Name+", "+r.Admin1, ", "),
				Country: strings.ToUpper(r.Country)}
			if len(r.Postcodes) > 0 {
				g.Zip = r.Postcodes[0]
			}
			return g, nil
		}
	}
	return geo{}, fmt.Errorf("could not find a place called %q", place)
}

// geo is what one lookup settles. The postcode is carried because pollen.com is
// keyed on a US zip and this is the only call that ever knows one, and the
// country because that is what says whether asking for pollen is worth a
// request at all.
type geo struct {
	Lat, Lon float64
	Name     string
	Zip      string
	Country  string
}

var Weather = Tool{
	Name:        "weather",
	Description: "Daily forecast for a place, up to 14 days out, in Fahrenheit and mph.",
	Schema: obj(map[string]any{
		"location": str("a town, city or region, like \"Boone, NC\""),
		"days":     integer("how many days ahead, 1 to 14, default 7"),
	}, "location"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		g, err := geocode(ctx, d, argStr(a, "location"))
		if err != nil {
			return nil, err
		}
		lat, lon, name := g.Lat, g.Lon, g.Name
		d.Widgets.Add(Widget{Kind: "weather", Place: name, Lat: lat, Lon: lon,
			Zip: g.Zip, Country: g.Country})
		days := int(argNum(a, "days", 7))
		if days < 1 || days > 14 {
			days = 7
		}
		var w struct {
			Daily struct {
				Time    []string  `json:"time"`
				Max     []float64 `json:"temperature_2m_max"`
				Min     []float64 `json:"temperature_2m_min"`
				Precip  []float64 `json:"precipitation_probability_max"`
				Wind    []float64 `json:"wind_speed_10m_max"`
				Sunrise []string  `json:"sunrise"`
				Sunset  []string  `json:"sunset"`
			} `json:"daily"`
		}
		u := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f"+
			"&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max,wind_speed_10m_max,sunrise,sunset"+
			"&temperature_unit=fahrenheit&wind_speed_unit=mph&timezone=auto&forecast_days=%d", lat, lon, days)
		if err := getJSON(ctx, d, u, &w); err != nil {
			return nil, err
		}
		type day struct {
			Date      string  `json:"date"`
			Weekday   string  `json:"weekday"`
			HighF     float64 `json:"high_f"`
			LowF      float64 `json:"low_f"`
			PrecipPct float64 `json:"precip_chance_pct"`
			WindMPH   float64 `json:"wind_mph"`
			Sunset    string  `json:"sunset,omitempty"`
		}
		out := make([]day, 0, len(w.Daily.Time))
		for i := range w.Daily.Time {
			wd := ""
			if t, e := time.Parse("2006-01-02", w.Daily.Time[i]); e == nil {
				wd = t.Format("Monday")
			}
			dd := day{Date: w.Daily.Time[i], Weekday: wd, HighF: w.Daily.Max[i],
				LowF: w.Daily.Min[i], PrecipPct: w.Daily.Precip[i], WindMPH: w.Daily.Wind[i]}
			if i < len(w.Daily.Sunset) {
				if t, e := time.Parse("2006-01-02T15:04", w.Daily.Sunset[i]); e == nil {
					dd.Sunset = t.Format("3:04 PM")
				}
			}
			out = append(out, dd)
		}
		return map[string]any{"place": name, "units": "fahrenheit, mph", "days": out}, nil
	},
}

// ---------------------------------------------------------------- markets

// alias lets the model say "S&P 500" or "gold" instead of knowing ticker syntax.
var alias = map[string]string{
	"s&p 500": "^GSPC", "s&p": "^GSPC", "sp500": "^GSPC", "spx": "^GSPC", "spy": "SPY",
	"nasdaq": "^IXIC", "nasdaq 100": "^NDX", "dow": "^DJI", "dow jones": "^DJI",
	"russell 2000": "^RUT", "vix": "^VIX", "gold": "GC=F", "silver": "SI=F",
	"oil": "CL=F", "crude": "CL=F", "natural gas": "NG=F", "10 year": "^TNX",
	"bitcoin": "BTC-USD", "btc": "BTC-USD", "ethereum": "ETH-USD", "eth": "ETH-USD",
	"solana": "SOL-USD", "dogecoin": "DOGE-USD",
}

// cnbcSym maps the tickers people write to the ones CNBC's quote cache uses.
// CNBC is the primary here rather than Yahoo because Yahoo's spark and chart
// endpoints both rate limit a home address hard, and this one does not.
var cnbcSym = map[string]string{
	"^GSPC": ".SPX", "^IXIC": ".IXIC", "^DJI": ".DJI", "^NDX": ".NDX", "^RUT": ".RUT",
	"^VIX": ".VIX", "GC=F": "@GC.1", "SI=F": "@SI.1", "CL=F": "@CL.1", "NG=F": "@NG.1",
	"^TNX": "US10Y",
}

var coinIDs = map[string]string{
	"BTC-USD": "bitcoin", "ETH-USD": "ethereum", "SOL-USD": "solana",
	"DOGE-USD": "dogecoin", "XRP-USD": "ripple",
}

type Quote struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name,omitempty"`
	Price     float64 `json:"price"`
	Change    string  `json:"change,omitempty"`
	ChangePct string  `json:"change_pct,omitempty"`
	PrevClose string  `json:"prev_close,omitempty"`
	AsOf      string  `json:"as_of,omitempty"`
	Currency  string  `json:"currency,omitempty"`
	Source    string  `json:"source"`
	Err       string  `json:"error,omitempty"`
}

var Markets = Tool{
	Name: "markets",
	Description: "Current price and daily change for stocks, indexes, commodities and crypto. " +
		"Plain names work: \"S&P 500, gold, bitcoin\" as well as \"AAPL\".",
	Schema: obj(map[string]any{
		"symbols": str("comma separated symbols or plain names, up to eight"),
	}, "symbols"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		raw := argStr(a, "symbols")
		if raw == "" {
			return nil, fmt.Errorf("symbols is required")
		}
		var want []string
		seen := map[string]bool{}
		for _, s := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '|' || r == '\n' }) {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			sym := s
			if v, ok := alias[strings.ToLower(s)]; ok {
				sym = v
			} else {
				sym = strings.ToUpper(s)
			}
			if !seen[sym] {
				seen[sym] = true
				want = append(want, sym)
			}
			if len(want) >= 8 {
				break
			}
		}
		// A chart each for the first few. Eight symbols is a legitimate ask and
		// eight charts is a wall, and the answer under them still names all of
		// them. Symbols are already in Yahoo's form here, indexes and futures
		// and crypto pairs alike, which is what the chart endpoint wants.
		for i, sym := range want {
			if i >= 3 {
				break
			}
			d.Widgets.Add(Widget{Kind: "ticker", Symbol: sym})
		}

		out := make([]Quote, 0, len(want))
		var coins, rest []string
		for _, s := range want {
			if _, ok := coinIDs[s]; ok {
				coins = append(coins, s)
			} else {
				rest = append(rest, s)
			}
		}
		if len(coins) > 0 {
			ids := make([]string, 0, len(coins))
			for _, c := range coins {
				ids = append(ids, coinIDs[c])
			}
			var cg map[string]struct {
				USD    float64 `json:"usd"`
				Change float64 `json:"usd_24h_change"`
			}
			u := "https://api.coingecko.com/api/v3/simple/price?vs_currencies=usd&include_24hr_change=true&ids=" + strings.Join(ids, ",")
			if err := getJSON(ctx, d, u, &cg); err != nil {
				for _, c := range coins {
					out = append(out, Quote{Symbol: c, Source: "coingecko", Err: err.Error()})
				}
			} else {
				for _, c := range coins {
					v := cg[coinIDs[c]]
					out = append(out, Quote{Symbol: c, Price: v.USD, Currency: "USD",
						ChangePct: fmt.Sprintf("%+.2f%%", v.Change), Source: "coingecko"})
				}
			}
		}
		if len(rest) > 0 {
			ids := make([]string, 0, len(rest))
			for _, s := range rest {
				if v, ok := cnbcSym[s]; ok {
					ids = append(ids, v)
				} else {
					ids = append(ids, s)
				}
			}
			var cn struct {
				R struct {
					Q json.RawMessage `json:"FormattedQuote"`
				} `json:"FormattedQuoteResult"`
			}
			u := "https://quote.cnbc.com/quote-html-webservice/restQuote/symbolType/symbol?symbols=" +
				url.QueryEscape(strings.Join(ids, "|")) +
				"&requestMethod=itv&noform=1&partnerId=2&fund=1&exthrs=1&output=json&events=1"
			if err := getJSON(ctx, d, u, &cn); err != nil {
				for _, s := range rest {
					out = append(out, Quote{Symbol: s, Source: "cnbc", Err: err.Error()})
				}
			} else {
				type cq struct {
					Symbol, ShortName, Last, Change, ChangePct, PreviousDayClosing, LastTimedate, CurrencyCode string
				}
				var rows []cq
				// One symbol comes back as an object rather than an array.
				if err := json.Unmarshal(cn.R.Q, &rows); err != nil {
					var one cq
					if json.Unmarshal(cn.R.Q, &one) == nil {
						rows = []cq{one}
					}
				}
				back := map[string]string{}
				for k, v := range cnbcSym {
					back[v] = k
				}
				got := map[string]Quote{}
				for _, r := range rows {
					sym := r.Symbol
					if b, ok := back[sym]; ok {
						sym = b
					}
					p, _ := strconv.ParseFloat(strings.ReplaceAll(r.Last, ",", ""), 64)
					if p == 0 {
						continue
					}
					got[sym] = Quote{Symbol: sym, Name: r.ShortName, Price: p, Change: r.Change,
						ChangePct: r.ChangePct, PrevClose: r.PreviousDayClosing, AsOf: r.LastTimedate,
						Currency: r.CurrencyCode, Source: "cnbc"}
				}
				for _, s := range rest {
					if q, ok := got[s]; ok {
						out = append(out, q)
					} else {
						out = append(out, Quote{Symbol: s, Source: "cnbc",
							Err: "no quote for that symbol, try web_search"})
					}
				}
			}
		}
		return map[string]any{"quotes": out}, nil
	},
}
