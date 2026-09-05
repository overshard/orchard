package skills

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// v7/finance/quote is dead: it answers 401 to anything without a cookie and a
// crumb. spark still works unauthenticated and carries the previous close,
// which is what a change is measured against.
const sparkURL = "https://query1.finance.yahoo.com/v7/finance/spark"

// Markets quotes the indexes, the two big cryptocurrencies and the two
// commodities dash already tracks. It is a price lookup and nothing else, so
// anything asking why a price moved belongs on the web.
type Markets struct{}

func (Markets) Card() Card {
	return Card{
		Name: "markets",
		Does: "quotes the current price and the move since the previous close for major indexes, bitcoin, ethereum, gold and crude oil.",
		Fires: []string{
			"what is the S&P 500 right now",
			"how is the market doing",
			"bitcoin price",
			"is the dow up or down today",
			"what is gold at",
		},
		NotFor: []string{
			"why did the market drop",
			"should i buy bitcoin",
			"what is the S&P 500",
			"how has the nasdaq performed this year",
			"what is the price of a tesla",
		},
		Keywords: []string{"s&p", "sp500", "dow", "nasdaq", "vix", "bitcoin", "btc",
			"ethereum", "gold price", "oil price", "stock market", "market doing"},
	}
}

// symbols maps the words a person uses to Yahoo's tickers.
func marketSymbols(q string) []string {
	l := strings.ToLower(q)
	pairs := []struct {
		words  []string
		symbol string
	}{
		{[]string{"s&p", "sp500", "s and p", "s & p"}, "^GSPC"},
		{[]string{"dow"}, "^DJI"},
		{[]string{"nasdaq"}, "^IXIC"},
		{[]string{"russell"}, "^RUT"},
		{[]string{"vix", "volatility"}, "^VIX"},
		{[]string{"bitcoin", "btc"}, "BTC-USD"},
		{[]string{"ethereum", "eth"}, "ETH-USD"},
		{[]string{"gold"}, "GC=F"},
		{[]string{"oil", "crude"}, "CL=F"},
	}
	var out []string
	for _, p := range pairs {
		if containsAny(l, p.words...) {
			out = append(out, p.symbol)
		}
	}
	// "how is the stock market doing" means the indexes.
	if len(out) == 0 && containsAny(l, "stock market", "market doing", "markets doing", "markets today") {
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

func (Markets) Run(ctx context.Context, question string, d Deps) (*Result, error) {
	start := d.now()

	// No recognised instrument means the router matched on the subject rather
	// than on something quotable, so decline instead of quoting the indexes at
	// someone who asked about a share price.
	syms := marketSymbols(question)
	if len(syms) == 0 {
		return nil, nil
	}

	url := fmt.Sprintf("%s?symbols=%s&range=1d&interval=5m", sparkURL, strings.Join(syms, ","))
	var p sparkPayload
	if err := getJSON(ctx, d, url, &p); err != nil {
		return nil, err
	}
	if len(p.Spark.Result) == 0 {
		return nil, nil
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
		return nil, nil
	}

	stamp := "an unknown time"
	if newest > 0 {
		stamp = time.Unix(newest, 0).In(d.now().Location()).Format("3:04 PM MST on 2 January 2006")
	}
	text := "Latest prices, measured against the previous close.\n\n" + b.String() +
		fmt.Sprintf("\nFrom Yahoo Finance, quoted at %s.", stamp)

	return &Result{
		Skill: "markets", Shape: "factual", Text: text,
		Sources: []Source{{URL: "https://finance.yahoo.com/", Title: "Yahoo Finance", Site: "finance.yahoo.com"}},
		Elapsed: d.now().Sub(start).Round(10 * time.Millisecond).String(),
	}, nil
}
