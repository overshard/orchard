// The read only JSON view of this site, for chat.bythewood.me's tools.
//
// This is not /aggregate. That one is public, feeds dash, and returns counts
// and nothing else because it is published to the internet. This one is behind
// the same session as the dashboard and carries the messages and paths with it,
// which is the whole reason for asking.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type apiSource struct {
	Source    string `json:"source"`
	Records   int    `json:"records"`
	Errors    int    `json:"errors"`
	Requests  int    `json:"requests"`
	Server5xx int    `json:"server_5xx"`
}

type apiError struct {
	When    string `json:"when"`
	Source  string `json:"source"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Status  int    `json:"status,omitempty"`
}

type apiPath struct {
	Path   string  `json:"path"`
	Hits   int     `json:"hits"`
	P95MS  float64 `json:"p95_ms"`
	Errors int     `json:"errors"`
}

func (s *site) apiSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	window := 24 * time.Hour
	if h, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && h > 0 && h <= 24*30 {
		window = time.Duration(h) * time.Hour
	}
	// Floored, because rollups.hour is an hour floored timestamp and a
	// now-relative start otherwise drops the bucket containing it whole.
	since := time.Now().Add(-window).Truncate(time.Hour).UnixMilli()

	out := map[string]any{
		"window_hours": int(window.Hours()),
		"sources":      s.apiSources(ctx, since),
		"recent_errors": s.apiRecentErrors(ctx, since,
			clamp(r.URL.Query().Get("errors"), 20, 200)),
		"busiest_paths": s.apiBusiestPaths(ctx, since, 15),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *site) apiSources(ctx context.Context, since int64) []apiSource {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source,
		       SUM(count),
		       SUM(CASE WHEN level = 'ERROR' THEN count ELSE 0 END),
		       SUM(CASE WHEN status > 0 THEN count ELSE 0 END),
		       SUM(CASE WHEN status >= 500 THEN count ELSE 0 END)
		FROM rollups
		WHERE hour >= ? AND component != 'healthz'
		GROUP BY source ORDER BY SUM(count) DESC`, since)
	if err != nil {
		queryFailed("api sources", err)
		return []apiSource{}
	}
	defer rows.Close()
	out := []apiSource{}
	for rows.Next() {
		var a apiSource
		if err := rows.Scan(&a.Source, &a.Records, &a.Errors, &a.Requests, &a.Server5xx); err != nil {
			queryFailed("api sources scan", err)
			break
		}
		out = append(out, a)
	}
	return out
}

func (s *site) apiRecentErrors(ctx context.Context, since int64, limit int) []apiError {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts, source, level, msg, path, status
		FROM records
		WHERE ts >= ? AND (level = 'ERROR' OR status >= 500) AND component != 'healthz'
		ORDER BY ts DESC LIMIT ?`, since, limit)
	if err != nil {
		queryFailed("api errors", err)
		return []apiError{}
	}
	defer rows.Close()
	out := []apiError{}
	for rows.Next() {
		var e apiError
		var ts int64
		if err := rows.Scan(&ts, &e.Source, &e.Level, &e.Message, &e.Path, &e.Status); err != nil {
			queryFailed("api errors scan", err)
			break
		}
		e.When = time.UnixMilli(ts).UTC().Format(time.RFC3339)
		out = append(out, e)
	}
	return out
}

// CUME_DIST and not PERCENT_RANK: PERCENT_RANK gives exactly 1.0 to the largest
// row of every partition, so `<= 0.95` never selects the slowest sample and a
// path with few samples reports well under the percentile it claims.
func (s *site) apiBusiestPaths(ctx context.Context, since int64, limit int) []apiPath {
	rows, err := s.db.QueryContext(ctx, `
		WITH windowed AS (
		    SELECT path, duration_ms, status,
		           CUME_DIST() OVER (PARTITION BY path ORDER BY duration_ms) AS cd
		    FROM records
		    WHERE ts >= ? AND path != '' AND component != 'healthz'
		)
		SELECT path,
		       COUNT(*),
		       COALESCE(MIN(CASE WHEN cd >= 0.95 THEN duration_ms END), 0),
		       SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END)
		FROM windowed
		GROUP BY path ORDER BY COUNT(*) DESC LIMIT ?`, since, limit)
	if err != nil {
		queryFailed("api paths", err)
		return []apiPath{}
	}
	defer rows.Close()
	out := []apiPath{}
	for rows.Next() {
		var p apiPath
		if err := rows.Scan(&p.Path, &p.Hits, &p.P95MS, &p.Errors); err != nil {
			queryFailed("api paths scan", err)
			break
		}
		out = append(out, p)
	}
	return out
}

func clamp(raw string, def, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
