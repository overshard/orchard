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

// One kind of error rather than one occurrence of one.
//
// The twenty most recent rows are usually the same failure twenty times, which
// spends the whole answer saying so and never says how long it has been going
// on. Grouping by what the error is turns that into one line with a count and a
// span, and a group of one is still the single record it was.
//
// Details is the attrs bag, which is where the reason lives. A slog call is
// `slog.Error("shipping a batch failed", "err", err)`, so the message names the
// operation and the error itself is an attribute. Without this the reader is
// told something failed and never what went wrong, which is what the dashboard
// has always shown and this endpoint did not.
type apiError struct {
	Message   string `json:"message"`
	Source    string `json:"source"`
	Level     string `json:"level"`
	Count     int    `json:"count"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Component string `json:"component,omitempty"`
	Status    int    `json:"status,omitempty"`
	Path      string `json:"path,omitempty"`
	Paths     int    `json:"distinct_paths,omitempty"`
	Details   string `json:"details,omitempty"`
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

	// Narrowing is what makes a second look useful. A summary that says search
	// is erroring and cannot then be asked only about search leaves the reader
	// with the same twenty lines again.
	source := truncate(r.URL.Query().Get("source"), 64)
	contains := truncate(r.URL.Query().Get("contains"), 200)

	out := map[string]any{
		"window_hours": int(window.Hours()),
		"sources":      s.apiSources(ctx, since),
		"errors": s.apiRecentErrors(ctx, since,
			clamp(r.URL.Query().Get("errors"), 20, 200), source, contains),
		"busiest_paths": s.apiBusiestPaths(ctx, since, 15),
	}
	if source != "" {
		out["filtered_to_source"] = source
	}
	if contains != "" {
		out["filtered_to_contains"] = contains
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

// How much of the attrs bag is worth carrying. It is capped at 8KB on the way
// in, and twenty of those would be most of a small model's window spent on one
// tool result, so the reason is kept and a stack trace pasted into an attribute
// is not.
const maxDetailLen = 500

// The example row for a group is the newest one, picked with a window function
// rather than MAX(ts) so its path, status and attrs come from that same record.
// Mixing the newest timestamp with an arbitrary row's details is how a group
// ends up describing an error that never happened.
func (s *site) apiRecentErrors(ctx context.Context, since int64, limit int, source, contains string) []apiError {
	rows, err := s.db.QueryContext(ctx, `
		WITH errs AS (
		    SELECT ts, source, level, msg, component, path, status, attrs
		    FROM records
		    -- Numbered rather than positional, since a bare ? beside a ?1
		    -- takes the number one greater than the largest already assigned,
		    -- which for the first ? in the statement is 1, so the two collide.
		    WHERE ts >= ?1
		      AND (level = 'ERROR' OR status >= 500)
		      AND component != 'healthz'
		      AND (?2 = '' OR source = ?2)
		      AND (?3 = '' OR msg LIKE '%' || ?3 || '%' OR attrs LIKE '%' || ?3 || '%' OR path LIKE '%' || ?3 || '%')
		), ranked AS (
		    SELECT *, ROW_NUMBER() OVER (PARTITION BY source, msg, status ORDER BY ts DESC) AS rn
		    FROM errs
		)
		SELECT r.msg, r.source, r.level, g.n, g.first_ts, g.last_ts,
		       r.component, r.status, r.path, g.paths, r.attrs
		FROM ranked r
		JOIN (
		    SELECT source, msg, status,
		           COUNT(*) AS n, MIN(ts) AS first_ts, MAX(ts) AS last_ts,
		           COUNT(DISTINCT path) AS paths
		    FROM errs GROUP BY source, msg, status
		) g ON g.source = r.source AND g.msg = r.msg AND g.status = r.status
		WHERE r.rn = 1
		ORDER BY g.last_ts DESC LIMIT ?4`,
		since, source, contains, limit)
	if err != nil {
		queryFailed("api errors", err)
		return []apiError{}
	}
	defer rows.Close()
	out := []apiError{}
	for rows.Next() {
		var e apiError
		var first, last int64
		var attrs string
		if err := rows.Scan(&e.Message, &e.Source, &e.Level, &e.Count, &first, &last,
			&e.Component, &e.Status, &e.Path, &e.Paths, &attrs); err != nil {
			queryFailed("api errors scan", err)
			break
		}
		e.FirstSeen = time.UnixMilli(first).UTC().Format(time.RFC3339)
		e.LastSeen = time.UnixMilli(last).UTC().Format(time.RFC3339)
		if attrs != "" && attrs != "{}" {
			e.Details = truncate(attrs, maxDetailLen)
		}
		// One path is the path, several means the group spans them and naming
		// the newest one would read as the only one.
		if e.Paths > 1 {
			e.Path = ""
		} else {
			e.Paths = 0
		}
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
