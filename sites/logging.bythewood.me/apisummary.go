// The read only JSON view of this site for chat.bythewood.me's tools. It sits
// behind the dashboard's session and carries messages and paths, while the public
// /aggregate hands dash counts and nothing else.
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

// One kind of error, grouped so the same failure twenty times is one line with a
// count and a span. Details is the attrs bag, since slog puts the error itself in
// an attribute and the message only names the operation.
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

// What the edge turned away, which errors can't answer since a refused scanner
// probe is an ERROR nowhere. The scanner block carries log_skip, so blocked
// requests are absent and this is what got past it.
type apiRefused struct {
	Client4xx  int          `json:"client_4xx"`
	Server5xx  int          `json:"server_5xx"`
	Requests   int          `json:"requests"`
	DirectHits int          `json:"direct_hits"`
	TopPaths   []apiBadPath `json:"top_4xx_paths"`
}

type apiBadPath struct {
	Host    string `json:"host,omitempty"`
	Path    string `json:"path"`
	Status  int    `json:"status"`
	Hits    int    `json:"hits"`
	Clients int    `json:"distinct_clients"`
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

	// Narrowing lets a second look ask only about the source or the text the
	// first one flagged, instead of reading the same twenty lines again.
	source := truncate(r.URL.Query().Get("source"), 64)
	contains := truncate(r.URL.Query().Get("contains"), 200)

	out := map[string]any{
		"window_hours": int(window.Hours()),
		"sources":      s.apiSources(ctx, since),
		"errors": s.apiRecentErrors(ctx, since,
			clamp(r.URL.Query().Get("errors"), 20, 200), source, contains),
		"busiest_paths": s.apiBusiestPaths(ctx, since, 15),
		"refused":       s.apiRefusedSummary(ctx, since, 15),
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

// Attrs are capped at 8KB on the way in, and twenty of those would spend most of
// a small model's window on one tool result, so the reason is kept and a stack
// trace pasted into an attribute isn't.
const maxDetailLen = 500

// The example row for a group is the newest one, picked with a window function
// instead of MAX(ts) so its path, status and attrs all come from that one record.
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

// CUME_DIST, since PERCENT_RANK gives exactly 1.0 to the largest row of every
// partition, so `<= 0.95` never selects the slowest sample and a path with few
// samples reports well under the percentile it claims.
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

// privateIP matches this machine talking to itself, over loopback or the docker
// bridge. The dashboard tile only excludes loopback, but here dash polling off
// the bridge all day would bury the one request that skipped Cloudflare.
const privateIP = `(ip = '' OR ip = '127.0.0.1' OR ip = '::1'
	OR ip LIKE '10.%' OR ip LIKE '127.%' OR ip LIKE '192.168.%' OR ip LIKE '169.254.%'
	OR ip GLOB '172.1[6-9].*' OR ip GLOB '172.2[0-9].*' OR ip GLOB '172.3[01].*'
	OR ip LIKE 'fc%' OR ip LIKE 'fd%' OR ip LIKE 'fe80:%')`

func (s *site) apiRefusedSummary(ctx context.Context, since int64, limit int) apiRefused {
	var out apiRefused
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       SUM(CASE WHEN status >= 400 AND status < 500 THEN 1 ELSE 0 END),
		       SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END),
		       SUM(CASE WHEN cf_ray = '' AND NOT `+privateIP+` THEN 1 ELSE 0 END)
		FROM records
		WHERE ts >= ? AND status > 0 AND component != 'healthz'`, since).
		Scan(&out.Requests, &out.Client4xx, &out.Server5xx, &out.DirectHits)
	if err != nil {
		queryFailed("api refused", err)
		return apiRefused{TopPaths: []apiBadPath{}}
	}

	out.TopPaths = []apiBadPath{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT host, path, status, COUNT(*), COUNT(DISTINCT ip)
		FROM records
		WHERE ts >= ? AND status >= 400 AND status < 500 AND path != ''
		      AND component != 'healthz'
		GROUP BY host, path, status
		ORDER BY COUNT(*) DESC LIMIT ?`, since, limit)
	if err != nil {
		queryFailed("api refused paths", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p apiBadPath
		if err := rows.Scan(&p.Host, &p.Path, &p.Status, &p.Hits, &p.Clients); err != nil {
			queryFailed("api refused paths scan", err)
			break
		}
		out.TopPaths = append(out.TopPaths, p)
	}
	return out
}
