package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// The one read another site makes of this one. dash.bythewood.me shows a health
// strip and needs a number per source, so this hands over counts and nothing
// else: no messages, no paths, no IP addresses, no request identifiers. dash is
// public and unauthenticated, so what leaves here has to be safe to publish
// without anything downstream having to remember that.
//
// Unauthenticated for the same reason /ingest is, and with the same fence:
// Caddy refuses this path on the public hostname, so it is reachable only by
// container name on the orchard-edge bridge.

const (
	// aggregateWindow is what "recent" means on the dash strip.
	aggregateWindow = 24 * time.Hour

	// baselineWindow sits immediately before it and is what "normal" means.
	baselineWindow = 7 * 24 * time.Hour

	// Past this a response was being held open rather than worked on.
	heldOpenMS = 60_000
)

type aggregateSource struct {
	Source    string `json:"source"`
	Records   int64  `json:"records"`
	Errors    int64  `json:"errors"`
	Requests  int64  `json:"requests"`
	Server5xx int64  `json:"server_5xx"`
	Up        bool   `json:"up"`
	UpKnown   bool   `json:"up_known"`

	// Requests a day over the preceding week, so a caller can say whether
	// today is busy for this site rather than busy in the abstract. A site
	// with no history yet reports zero and the caller shows no comparison.
	BaselineDaily float64 `json:"baseline_daily"`

	// The slow end of how long this site takes to answer a real request. A
	// duration is not a path or a message, so it clears the same fence
	// everything else here does.
	P95MS float64 `json:"p95_ms"`
}

func (s *site) aggregate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	since := time.Now().Add(-aggregateWindow).Truncate(time.Hour).UnixMilli()

	// healthz is excluded the same way the source table excludes it: a probe
	// every thirty seconds would otherwise be most of what a quiet site logs.
	rows, err := s.db.QueryContext(ctx, `
		SELECT source,
		       SUM(count),
		       SUM(CASE WHEN level = 'ERROR' THEN count ELSE 0 END),
		       SUM(CASE WHEN status > 0 THEN count ELSE 0 END),
		       SUM(CASE WHEN status >= 500 THEN count ELSE 0 END)
		FROM rollups
		WHERE hour >= ? AND component != 'healthz'
		GROUP BY source ORDER BY source`, since)
	if err != nil {
		queryFailed("aggregate", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()

	out := []aggregateSource{}
	for rows.Next() {
		var a aggregateSource
		if err := rows.Scan(&a.Source, &a.Records, &a.Errors, &a.Requests, &a.Server5xx); err != nil {
			queryFailed("aggregate scan", err)
			break
		}
		out = append(out, a)
	}
	queryFailed("aggregate rows", rows.Err())

	for i := range out {
		out[i].Up, out[i].UpKnown = lifecycleState(ctx, s.db, out[i].Source)
	}
	baselines(ctx, s.db, out)
	responseTimes(ctx, s.db, out)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"window_hours": int(aggregateWindow / time.Hour),
		"sources":      out,
	})
}

// lifecycleState reads what the watchdog last wrote for a source. It parses the
// same "state|millis" value writeLifecycle produces and only needs the state
// half.
func lifecycleState(ctx context.Context, db *sql.DB, source string) (up, known bool) {
	var value string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key = ?`, lifecycleKey(source)).Scan(&value)
	if err != nil {
		return false, false
	}
	state, _, _ := strings.Cut(value, "|")
	return state == "up", true
}

// baselines fills in the per-day request average over the week before the
// window, which is the comparison that makes a traffic number mean anything on
// a site this size. The window itself is excluded so today is measured against
// normal rather than against itself.
func baselines(ctx context.Context, db *sql.DB, out []aggregateSource) {
	now := time.Now()
	windowStart := now.Add(-aggregateWindow).Truncate(time.Hour).UnixMilli()
	baselineStart := now.Add(-aggregateWindow - baselineWindow).Truncate(time.Hour).UnixMilli()

	// MIN(hour) as well as the sum, because this database is younger than the
	// baseline window and dividing two days of traffic by seven would report
	// every site as busier than normal, every day, forever.
	rows, err := db.QueryContext(ctx, `
		SELECT source,
		       SUM(CASE WHEN status > 0 THEN count ELSE 0 END),
		       MIN(hour)
		FROM rollups
		WHERE hour >= ? AND hour < ? AND component != 'healthz'
		GROUP BY source`, baselineStart, windowStart)
	if err != nil {
		queryFailed("aggregate baseline", err)
		return
	}
	defer rows.Close()

	type span struct {
		requests int64
		earliest int64
	}
	totals := map[string]span{}
	for rows.Next() {
		var source string
		var sp span
		if err := rows.Scan(&source, &sp.requests, &sp.earliest); err != nil {
			queryFailed("aggregate baseline scan", err)
			break
		}
		totals[source] = sp
	}
	queryFailed("aggregate baseline rows", rows.Err())

	for i := range out {
		sp, ok := totals[out[i].Source]
		if !ok || sp.earliest <= 0 {
			continue
		}

		days := float64(windowStart-sp.earliest) / float64(24*time.Hour/time.Millisecond)
		// Under a day of history is not a baseline, and calling it one would
		// divide by a fraction and report a wild number.
		if days < 1 {
			continue
		}
		out[i].BaselineDaily = float64(sp.requests) / days
	}
}

// responseTimes fills in the 95th percentile request duration per source. It
// reads raw rows rather than the rollups because a percentile needs the
// distribution and the rollups only carry a sum, a count and a max.
//
// The mean is what the rollups could answer cheaply and it is the wrong number
// here: dash holds an events stream open for as long as a tab is on it, so one
// six hour visit dragged its mean to seventy seconds while it was answering
// pages in one. Both of the components excluded below are requests whose
// duration measures something other than work: healthz is the container
// talking to itself, and stream is a connection deliberately left open.
//
// The ceiling is there for the streams that cannot be named. Caddy proxies
// every one of them and writes its own access log, so it has no handler to tag
// them in, and nothing this machine serves takes a minute to answer.
func responseTimes(ctx context.Context, db *sql.DB, out []aggregateSource) {
	since := time.Now().Add(-aggregateWindow).UnixMilli()

	rows, err := db.QueryContext(ctx, `
		WITH d AS (
		  SELECT source, duration_ms,
		         CUME_DIST() OVER (PARTITION BY source ORDER BY duration_ms) cd
		  FROM records
		  WHERE ts >= ? AND duration_ms > 0 AND duration_ms <= ?
		    AND component NOT IN ('healthz', 'stream')
		)
		SELECT source, MIN(CASE WHEN cd >= 0.95 THEN duration_ms END)
		FROM d GROUP BY source`, since, heldOpenMS)
	if err != nil {
		queryFailed("aggregate p95", err)
		return
	}
	defer rows.Close()

	p95 := map[string]float64{}
	for rows.Next() {
		var source string
		var v sql.NullFloat64
		if err := rows.Scan(&source, &v); err != nil {
			queryFailed("aggregate p95 scan", err)
			break
		}
		if v.Valid {
			p95[source] = v.Float64
		}
	}
	queryFailed("aggregate p95 rows", rows.Err())

	for i := range out {
		out[i].P95MS = p95[out[i].Source]
	}
}
