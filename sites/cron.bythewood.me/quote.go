package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A quote job stays quiet until its ticker trades, pushes the first trade it
// sees, then pushes at most every quoteEvery with the change since its last
// push. Run it every minute so the first trade lands fast.
const quoteEvery = 5 * time.Minute

var nasdaqQuote = "https://api.nasdaq.com/api/quote/%s/info?assetclass=stocks"

var pushedPrice = regexp.MustCompile(`\$([0-9,]+(?:\.[0-9]+)?)`)

type Quote struct {
	Price  float64
	Change string
	At     string
}

// fetchQuote reads the listing exchange's own quote. Traded is false while
// Nasdaq still shows N/A, which is how an IPO looks before its opening cross.
func fetchQuote(ctx context.Context, c *http.Client, symbol string) (q Quote, traded bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(nasdaqQuote, url.PathEscape(symbol)), nil)
	if err != nil {
		return q, false, err
	}
	// api.nasdaq.com hangs on requests that don't look like a browser.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return q, false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return q, false, fmt.Errorf("nasdaq: http %d", resp.StatusCode)
	}
	var r struct {
		Data *struct {
			PrimaryData struct {
				LastSalePrice      string `json:"lastSalePrice"`
				PercentageChange   string `json:"percentageChange"`
				LastTradeTimestamp string `json:"lastTradeTimestamp"`
			} `json:"primaryData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return q, false, fmt.Errorf("nasdaq: %w", err)
	}
	if r.Data == nil {
		return q, false, fmt.Errorf("nasdaq has no quote for %s", symbol)
	}
	p := r.Data.PrimaryData
	price, err := strconv.ParseFloat(strings.NewReplacer("$", "", ",", "").Replace(p.LastSalePrice), 64)
	if err != nil || price <= 0 {
		return q, false, nil
	}
	return Quote{Price: price, Change: p.PercentageChange, At: p.LastTradeTimestamp}, true, nil
}

func (s *Scheduler) quote(ctx context.Context, j Job) (title, body string, send bool, err error) {
	symbol := strings.ToUpper(strings.TrimSpace(j.Message))
	q, traded, err := fetchQuote(ctx, s.http, symbol)
	if err != nil || !traded {
		return "", "", false, err
	}
	last, ok := s.lastPushRun(j.ID)
	if !ok {
		return symbol + " is trading", fmt.Sprintf("%s first trade $%.2f, %s", symbol, q.Price, q.At), true, nil
	}
	// A few seconds of slack so a run that fires early in its minute still counts.
	if s.now().Sub(last.At) < quoteEvery-15*time.Second {
		return "", "", false, nil
	}
	body = fmt.Sprintf("%s $%.2f", symbol, q.Price)
	if m := pushedPrice.FindStringSubmatch(last.Body); m != nil {
		if prev, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64); err == nil && prev > 0 {
			body += fmt.Sprintf(", %+.2f%% since %s", (q.Price-prev)/prev*100, last.At.In(s.loc).Format("3:04"))
		}
	}
	if q.Change != "" {
		body += ", " + q.Change + " on the day"
	}
	return symbol, body, true, nil
}

func (s *Scheduler) lastPushRun(job int64) (Run, bool) {
	runs, err := s.store.Runs(job, 50)
	if err != nil {
		return Run{}, false
	}
	for _, r := range runs {
		if r.Pushed {
			return r, true
		}
	}
	return Run{}, false
}
