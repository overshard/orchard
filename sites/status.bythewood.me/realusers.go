package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// analyticsBridge is reached by container name over orchard-edge, and Caddy
// refuses /bridge on analytics' public hostname.
const analyticsBridge = "http://orchard-analytics:8000/bridge/vitals"

// RealUsers is analytics' last week of real visitors for one host.
type RealUsers struct {
	Dashboard string `json:"dashboard"`
	Samples   int    `json:"samples"`
	Metrics   []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
		Class string `json:"class"`
		N     int    `json:"n"`
	} `json:"metrics"`
	Errors24h int64 `json:"errors_24h"`
	Errors7d  int64 `json:"errors_7d"`
	TopErrors []struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
		Source  string `json:"source"`
		Count   int64  `json:"count"`
	} `json:"top_errors"`
}

// realUsersReader caches each answer for a few minutes, so a busy property
// page doesn't ask analytics to sort a week of rows on every load.
type realUsersReader struct {
	url    string
	client *http.Client
	mu     sync.Mutex
	cache  map[string]cachedRealUsers
}

type cachedRealUsers struct {
	at time.Time
	ru *RealUsers
}

func newRealUsersReader() *realUsersReader {
	return &realUsersReader{url: analyticsBridge, client: &http.Client{Timeout: 2 * time.Second}, cache: map[string]cachedRealUsers{}}
}

// For returns nil when analytics doesn't answer or doesn't track the host, and
// the page shows that instead of failing.
func (rr *realUsersReader) For(ctx context.Context, pageURL string) *RealUsers {
	u, err := url.Parse(pageURL)
	if err != nil || u.Host == "" {
		return nil
	}
	host := u.Hostname()
	rr.mu.Lock()
	if c, ok := rr.cache[host]; ok && time.Since(c.at) < 5*time.Minute {
		rr.mu.Unlock()
		return c.ru
	}
	rr.mu.Unlock()

	ru := rr.fetch(ctx, host)
	rr.mu.Lock()
	rr.cache[host] = cachedRealUsers{time.Now(), ru}
	rr.mu.Unlock()
	return ru
}

func (rr *realUsersReader) fetch(ctx context.Context, host string) *RealUsers {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rr.url+"?host="+url.QueryEscape(host), nil)
	if err != nil {
		return nil
	}
	resp, err := rr.client.Do(req)
	if err != nil {
		slog.Info("real users", slog.String("host", host), slog.Any("err", err))
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var ru RealUsers
	if err := json.NewDecoder(resp.Body).Decode(&ru); err != nil {
		return nil
	}
	return &ru
}
