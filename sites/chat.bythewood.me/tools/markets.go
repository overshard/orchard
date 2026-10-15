package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// alias lets the model say "S&P 500" or "gold" instead of knowing ticker syntax.
// Keys are lower case with hyphens read as spaces.
var alias = map[string]string{
	"s&p 500": "^GSPC", "s&p": "^GSPC", "sp500": "^GSPC", "spx": "^GSPC", "spy": "SPY",
	"nasdaq": "^IXIC", "nasdaq 100": "^NDX", "dow": "^DJI", "dow jones": "^DJI",
	"russell 2000": "^RUT", "vix": "^VIX", "gold": "GC=F", "silver": "SI=F",
	"oil": "CL=F", "crude": "CL=F", "natural gas": "NG=F", "10 year": "^TNX",
	"10 year treasury": "^TNX", "10 year yield": "^TNX", "10 year treasury yield": "^TNX",
	"us10y": "^TNX", "tnx": "^TNX", "treasury": "^TNX",
	"bitcoin": "BTC-USD", "btc": "BTC-USD", "ethereum": "ETH-USD", "eth": "ETH-USD",
	"solana": "SOL-USD", "dogecoin": "DOGE-USD",
	// ES on its own is also Eversource's ticker, and asked "ES, NQ, YM" for the
	// morning's futures it quoted a utility.
	"es": "ES=F", "/es": "ES=F", "s&p futures": "ES=F", "s&p 500 futures": "ES=F", "spx futures": "ES=F",
	"nq": "NQ=F", "/nq": "NQ=F", "nasdaq futures": "NQ=F", "nasdaq 100 futures": "NQ=F",
	"ym": "YM=F", "/ym": "YM=F", "dow futures": "YM=F", "dow jones futures": "YM=F",
	"rty": "RTY=F", "/rty": "RTY=F", "russell futures": "RTY=F", "russell 2000 futures": "RTY=F",
}

// futuresOf is the contract that trades while an index is shut.
var futuresOf = map[string]string{"^GSPC": "ES=F", "^NDX": "NQ=F", "^IXIC": "NQ=F", "^DJI": "YM=F", "^RUT": "RTY=F"}

// closedIndex is a quote whose last print was a close and not a trade, which is
// how CNBC dates a quote with no time on it.
func closedIndex(q Quote) bool { return strings.HasPrefix(q.AsOf, "the close") }

// Asked for "futures" the model means the four index contracts.
var indexFutures = []string{"ES=F", "NQ=F", "YM=F", "RTY=F"}

var futuresWord = map[string]bool{
	"futures": true, "index futures": true, "stock futures": true, "equity futures": true,
	"us futures": true, "stock market futures": true,
}

// cnbcSym maps the tickers people write to the ones CNBC's quote cache uses.
// CNBC is the primary here instead of Yahoo because Yahoo's spark and chart
// endpoints both rate limit a home address hard, and this one doesn't.
var cnbcSym = map[string]string{
	"^GSPC": ".SPX", "^IXIC": ".IXIC", "^DJI": ".DJI", "^NDX": ".NDX", "^RUT": ".RUT",
	"^VIX": ".VIX", "GC=F": "@GC.1", "SI=F": "@SI.1", "CL=F": "@CL.1", "NG=F": "@NG.1",
	"^TNX": "US10Y", "ES=F": "@SP.1", "NQ=F": "@ND.1", "YM=F": "@DJ.1", "RTY=F": "@TFS.1",
}

var coinIDs = map[string]string{
	"BTC-USD": "bitcoin", "ETH-USD": "ethereum", "SOL-USD": "solana",
	"DOGE-USD": "dogecoin", "XRP-USD": "ripple",
}

type Quote struct {
	Symbol    string   `json:"symbol"`
	Name      string   `json:"name,omitempty"`
	Price     float64  `json:"price"`
	Change    string   `json:"change,omitempty"`
	ChangePct string   `json:"change_pct,omitempty"`
	PrevClose string   `json:"prev_close,omitempty"`
	AsOf      string   `json:"as_of,omitempty"`
	Extended  *Session `json:"extended_hours,omitempty"`
	Futures   *Quote   `json:"futures_now,omitempty"`
	Over      *Span    `json:"over_period,omitempty"`
	Currency  string   `json:"currency,omitempty"`
	Source    string   `json:"source"`
	Err       string   `json:"error,omitempty"`
}

// Session is a stock's pre-market or after hours print, which is the only
// thing that moved when the regular quote is still last night's close.
type Session struct {
	Kind      string `json:"session"`
	Last      string `json:"price"`
	Change    string `json:"change,omitempty"`
	ChangePct string `json:"change_pct,omitempty"`
	AsOf      string `json:"as_of,omitempty"`
}

// Span is the change over a week, a month or a year, measured from the first
// bar of the span, since a question about the week is not answered by a day.
type Span struct {
	Period  string  `json:"period"`
	Since   string  `json:"since"`
	From    float64 `json:"from"`
	Change  float64 `json:"change"`
	Percent float64 `json:"percent"`
}

// periodRange is the chart range each period reads.
var periodRange = map[string]string{"week": "1w", "month": "1m", "year": "1y"}

func nyOrUTC() *time.Location {
	if ny, err := time.LoadLocation("America/New_York"); err == nil {
		return ny
	}
	return time.UTC
}

// asOf says when a CNBC quote was taken, in words. A quote from before today is
// the last close, which a model reading it at eight in the morning reports as
// how the futures are doing.
func asOf(lastTime, when string, now time.Time) string {
	ny := nyOrUTC()
	now = now.In(ny)
	if t, err := time.ParseInLocation("2006-01-02", lastTime, ny); err == nil {
		if t.Format("2006-01-02") == now.Format("2006-01-02") {
			return "the close today"
		}
		return "the close on " + t.Format("Monday, 2 January")
	}
	if t, err := time.Parse("2006-01-02T15:04:05.000-0700", lastTime); err == nil {
		t = t.In(ny)
		if t.Format("2006-01-02") == now.Format("2006-01-02") {
			return t.Format("3:04 PM MST") + " today"
		}
		return t.Format("3:04 PM MST on Monday, 2 January")
	}
	return when
}

var Markets = Tool{
	Name: "markets",
	Description: "Current price and daily change for stocks, indexes, futures, treasury yields, commodities and crypto. " +
		"Plain names work: \"S&P 500, gold, bitcoin\" as well as \"AAPL\". \"futures\" gives the S&P, Nasdaq, Dow and " +
		"Russell index futures, which are what moves before the open and after the close, since an index quote " +
		"outside market hours is the last close. An index that is shut carries its futures in futures_now, and that " +
		"is how it is trading now. Each quote says when it was taken in as_of, so say which day a " +
		"close is from. Pass period week, month or year for a question about more than today.",
	Schema: obj(map[string]any{
		"symbols": str("comma separated symbols or plain names, up to eight"),
		"period":  str("day (default), week, month or year, for the change over that span"),
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
			key := strings.ReplaceAll(strings.ToLower(s), "-", " ")
			syms := []string{strings.ToUpper(s)}
			if v, ok := alias[key]; ok {
				syms = []string{v}
			} else if futuresWord[key] {
				syms = indexFutures
			}
			for _, sym := range syms {
				if !seen[sym] && len(want) < 8 {
					seen[sym] = true
					want = append(want, sym)
				}
			}
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
				// An index always brings its future along and keeps it while the
				// index is shut, or a morning question about the S&P gets last
				// night's close as how the futures are doing.
				if f, ok := futuresOf[s]; ok && !seen[f] {
					ids = append(ids, cnbcSym[f])
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
					Symbol       string `json:"symbol"`
					ShortName    string `json:"shortName"`
					Last         string `json:"last"`
					Change       string `json:"change"`
					ChangePct    string `json:"change_pct"`
					PrevClose    string `json:"previous_day_closing"`
					LastTime     string `json:"last_time"`
					LastTimedate string `json:"last_timedate"`
					CurrencyCode string `json:"currencyCode"`
					Extended     *struct {
						Type         string `json:"type"`
						Last         string `json:"last"`
						Change       string `json:"change"`
						ChangePct    string `json:"change_pct"`
						LastTime     string `json:"last_time"`
						LastTimedate string `json:"last_timedate"`
					} `json:"ExtendedMktQuote"`
				}
				var rows []cq
				// One symbol comes back as an object instead of an array.
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
					// A yield comes back as "5.241%".
					p, _ := strconv.ParseFloat(strings.NewReplacer(",", "", "%", "").Replace(r.Last), 64)
					if p == 0 {
						continue
					}
					// After the close CNBC hands back the close as the previous
					// close too, which reads as a day that did not move.
					if r.PrevClose == r.Last {
						r.PrevClose = ""
					}
					q := Quote{Symbol: sym, Name: r.ShortName, Price: p, Change: r.Change,
						ChangePct: r.ChangePct, PrevClose: r.PrevClose, AsOf: asOf(r.LastTime, r.LastTimedate, d.Now()),
						Currency: r.CurrencyCode, Source: "cnbc"}
					if x := r.Extended; x != nil && x.Last != "" {
						kind := "after hours"
						if x.Type == "PRE_MKT" {
							kind = "pre-market"
						}
						q.Extended = &Session{Kind: kind, Last: x.Last, Change: x.Change, ChangePct: x.ChangePct,
							AsOf: asOf(x.LastTime, x.LastTimedate, d.Now())}
					}
					got[sym] = q
				}
				for _, s := range rest {
					if q, ok := got[s]; ok {
						if f, ok := got[futuresOf[s]]; ok && closedIndex(q) {
							q.Futures = &f
						}
						out = append(out, q)
					} else {
						out = append(out, Quote{Symbol: s, Source: "cnbc",
							Err: "no quote for that symbol, try web_search"})
					}
				}
			}
		}
		period := strings.ToLower(argStr(a, "period"))
		if key, ok := periodRange[period]; ok {
			spans := 0
			for i := range out {
				if spans >= 4 || out[i].Err != "" || out[i].Price == 0 {
					continue
				}
				spans++
				ser, err := Ticker(ctx, d, out[i].Symbol, key)
				if err != nil || len(ser.Points) == 0 {
					continue
				}
				out[i].Over = &Span{Period: period, From: ser.Previous, Change: ser.Change,
					Percent: ser.Percent, Since: time.Unix(ser.Points[0].T, 0).In(nyOrUTC()).Format("Monday, 2 January 2006")}
			}
		}
		// A chart each for the first few that actually resolved, since charting a
		// symbol whose quote came back empty draws an axis with nothing on it.
		charts := 0
		for _, q := range out {
			if charts >= 3 {
				break
			}
			if q.Err != "" || q.Price == 0 {
				continue
			}
			d.Widgets.Add(Widget{Kind: "ticker", Symbol: q.Symbol})
			charts++
		}
		return map[string]any{"quotes": out}, nil
	},
}
