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

var sparkURL = "https://query1.finance.yahoo.com/v7/finance/spark?range=1d&interval=1m&symbols="

var pushedPrice = regexp.MustCompile(`\$([0-9,]+(?:\.[0-9]+)?)`)

type Quote struct {
	Price  float64
	Change float64
	First  time.Time
}

// fetchQuote reads Yahoo's spark, the same keyless endpoint dash uses. Before
// an IPO's opening cross Yahoo shows the offer price with no minute bars, so
// traded is false until the first bar with a close shows up.
func fetchQuote(ctx context.Context, c *http.Client, symbol string) (q Quote, traded bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sparkURL+url.QueryEscape(symbol), nil)
	if err != nil {
		return q, false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; cron.bythewood.me)")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return q, false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return q, false, fmt.Errorf("yahoo: http %d", resp.StatusCode)
	}
	var r struct {
		Spark struct {
			Result []struct {
				Response []struct {
					Meta struct {
						Price  float64 `json:"regularMarketPrice"`
						Change float64 `json:"regularMarketChangePercent"`
					} `json:"meta"`
					Timestamp  []int64 `json:"timestamp"`
					Indicators struct {
						Quote []struct {
							Close []*float64 `json:"close"`
						} `json:"quote"`
					} `json:"indicators"`
				} `json:"response"`
			} `json:"result"`
		} `json:"spark"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return q, false, fmt.Errorf("yahoo: %w", err)
	}
	if len(r.Spark.Result) == 0 || len(r.Spark.Result[0].Response) == 0 {
		return q, false, fmt.Errorf("yahoo has no quote for %s", symbol)
	}
	resp0 := r.Spark.Result[0].Response[0]
	q.Price, q.Change = resp0.Meta.Price, resp0.Meta.Change
	if len(resp0.Indicators.Quote) == 0 {
		return q, false, nil
	}
	closes := resp0.Indicators.Quote[0].Close
	for i, ts := range resp0.Timestamp {
		if i < len(closes) && closes[i] != nil {
			q.First = time.Unix(ts, 0)
			return q, q.Price > 0, nil
		}
	}
	return q, false, nil
}

func (s *Scheduler) quote(ctx context.Context, j Job) (title, body string, send bool, err error) {
	symbol := strings.ToUpper(strings.TrimSpace(j.Message))
	q, traded, err := fetchQuote(ctx, s.http, symbol)
	if err != nil || !traded {
		return "", "", false, err
	}
	last, ok := s.lastPushRun(j.ID)
	if !ok {
		return symbol + " is trading", fmt.Sprintf("%s $%.2f, first trade at %s, %+.2f%% on the day", symbol, q.Price, q.First.In(s.loc).Format("3:04pm"), q.Change), true, nil
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
	body += fmt.Sprintf(", %+.2f%% on the day", q.Change)
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
