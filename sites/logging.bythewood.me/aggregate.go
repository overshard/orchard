package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// dash reads /aggregate for its health strip and dash is public, so this hands
// over counts per source and never a message, path, IP address or request id.
// It's unauthenticated like /ingest, and Caddy refuses it on the public hostname.

const (
	// aggregateWindow is what "recent" means on the dash strip.
	aggregateWindow = 24 * time.Hour

	// baselineWindow sits immediately before it and is what "normal" means.
	baselineWindow = 7 * 24 * time.Hour

	// Past this a response was being held open, not worked on.
	heldOpenMS = 60_000

	// dash asks every minute, and a week's baseline or a day's p95 over raw
	// rows barely moves in five.
	slowAggregateTTL = 5 * time.Minute
)

// slowAggregates holds the baselines and p95s between calls, since they're
// most of the cost of /aggregate and the counts and up flags beside them are
// what has to be fresh.
type slowAggregates struct {
	mu       sync.Mutex
	at       time.Time
	baseline map[string]float64
	p95      map[string]float64
}

func (c *slowAggregates) fill(ctx context.Context, db *sql.DB, out []aggregateSource) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseline == nil || time.Since(c.at) > slowAggregateTTL {
		// A caller hanging up mid query would otherwise leave five minutes of
		// blanks behind it.
		ctx = context.WithoutCancel(ctx)
		c.baseline, c.p95, c.at = baselines(ctx, db), responseTimes(ctx, db), time.Now()
	}
	for i := range out {
		out[i].BaselineDaily = c.baseline[out[i].Source]
		out[i].P95MS = c.p95[out[i].Source]
	}
}

type aggregateSource struct {
	Source    string `json:"source"`
	Records   int64  `json:"records"`
	Errors    int64  `json:"errors"`
	Requests  int64  `json:"requests"`
	Server5xx int64  `json:"server_5xx"`
	Up        bool   `json:"up"`
	UpKnown   bool   `json:"up_known"`

	// Requests a day over the preceding week, so a caller can say whether
	// today is busy for this site. A site with no history yet reports zero.
	BaselineDaily float64 `json:"baseline_daily"`

	// A duration isn't a path or a message, so it's as safe to publish as a count.
	P95MS float64 `json:"p95_ms"`
}

func (s *site) aggregate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	since := time.Now().Add(-aggregateWindow).Truncate(time.Hour).UnixMilli()

	// healthz is excluded like the source table excludes it, since a probe every
	// thirty seconds would otherwise be most of what a quiet site logs.
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
	s.slow.fill(ctx, s.db, out)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"window_hours": int(aggregateWindow / time.Hour),
		"sources":      out,
	})
}

// lifecycleState reads the "state|millis" the watchdog last wrote for a source
// and keeps the state half.
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
// window. The window itself is excluded so today is measured against normal and
// not against itself.
func baselines(ctx context.Context, db *sql.DB) map[string]float64 {
	now := time.Now()
	windowStart := now.Add(-aggregateWindow).Truncate(time.Hour).UnixMilli()
	baselineStart := now.Add(-aggregateWindow - baselineWindow).Truncate(time.Hour).UnixMilli()

	// MIN(hour) too, since the database can be younger than the baseline window
	// and dividing two days of traffic by seven would make every site look busy.
	rows, err := db.QueryContext(ctx, `
		SELECT source,
		       SUM(CASE WHEN status > 0 THEN count ELSE 0 END),
		       MIN(hour)
		FROM rollups
		WHERE hour >= ? AND hour < ? AND component != 'healthz'
		GROUP BY source`, baselineStart, windowStart)
	if err != nil {
		queryFailed("aggregate baseline", err)
		return nil
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

	daily := map[string]float64{}
	for source, sp := range totals {
		if sp.earliest <= 0 {
			continue
		}
		days := float64(windowStart-sp.earliest) / float64(24*time.Hour/time.Millisecond)
		// Under a day of history is not a baseline, and calling it one would
		// divide by a fraction and report a wild number.
		if days < 1 {
			continue
		}
		daily[source] = float64(sp.requests) / days
	}
	return daily
}

// responseTimes reads raw rows since a percentile needs the distribution and the
// rollups only carry a sum, count and max. healthz and stream aren't work, and
// heldOpenMS drops the streams Caddy proxies, since it has no handler to tag them.
func responseTimes(ctx context.Context, db *sql.DB) map[string]float64 {
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
		return nil
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
	return p95
}
