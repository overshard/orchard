package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Two tables answer different questions. rollups answers "how many, over time"
// for any range; records answers "which ones, and how slow", and only inside
// rawRetention. Reading the wrong one gives a quietly wrong answer.

type LabelCount struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// GraphPoint is one bucket of the volume chart; Errors rides along so the error
// band needs no second query.
type GraphPoint struct {
	Label  string `json:"label"`
	Count  int64  `json:"count"`
	Errors int64  `json:"errors"`
}

// Latency is the request duration distribution. Percentiles come from raw rows,
// so they are exact and only available inside rawRetention.
type Latency struct {
	Count int64
	Mean  float64
	P50   float64
	P95   float64
	P99   float64
	Max   float64
}

type RecordRow struct {
	ID         int64
	Source     string
	TS         int64
	Time       string
	Level      string
	Msg        string
	Component  string
	Method     string
	Path       string
	Host       string
	Status     int64
	DurationMS float64
	IP         string
	CFRay      string
	Attrs      string
}

type SourceRow struct {
	Source    string
	Records   int64
	Errors    int64
	Requests  int64
	Server5xx int64
	Client4xx int64
	ErrorRate float64
	P95       float64
	LastSeen  string
	IsLive    bool
}

type PathRow struct {
	Path   string
	Count  int64
	P95    float64
	Max    float64
	Mean   float64
	Errors int64
}

// filter is the shared WHERE clause behind every query here; a zero field means
// no constraint.
type filter struct {
	StartMS int64
	EndMS   int64
	Source  string
	Level   string
	// StatusClass is a class (2, 3, 4, 5), not a code.
	Component   string
	StatusClass int
	// Q is a substring match on the message or the path.
	Q string
}

// where builds the clause and its arguments, all as placeholders. The start is
// floored for the hour column: rollups.hour is hour-floored, so an unaligned
// start would silently drop the bucket containing it, whole.
func (f filter) where(tsColumn string) (string, []any) {
	start := f.StartMS
	if tsColumn == "hour" {
		start = hourFloor(start)
	}
	clauses := []string{tsColumn + " >= ?", tsColumn + " < ?"}
	args := []any{start, f.EndMS}

	if f.Source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, f.Source)
	}
	if f.Level != "" {
		clauses = append(clauses, "level = ?")
		args = append(args, strings.ToUpper(f.Level))
	}
	if f.Component != "" {
		clauses = append(clauses, "component = ?")
		args = append(args, f.Component)
	}
	if f.StatusClass >= 1 && f.StatusClass <= 5 {
		lo := int64(f.StatusClass) * 100
		clauses = append(clauses, "status >= ? AND status < ?")
		args = append(args, lo, lo+100)
	}
	if f.Q != "" {
		// ESCAPE is what stops a search for "100%" from matching everything.
		clauses = append(clauses, "(msg LIKE ? ESCAPE '\\' OR path LIKE ? ESCAPE '\\')")
		like := "%" + escapeLike(f.Q) + "%"
		args = append(args, like, like)
	}
	return strings.Join(clauses, " AND "), args
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// queryFailed logs a broken read so one dead panel does not cost the page. A
// nil error and sql.ErrNoRows are both no-ops.
func queryFailed(what string, err error) {
	if err == nil || err == sql.ErrNoRows {
		return
	}
	slog.Error("query failed", slog.String("component", "query"),
		slog.String("query", what), slog.Any("err", err))
}

// Totals is the metric tile row.
type Totals struct {
	Records   int64
	Errors    int64
	Warnings  int64
	Requests  int64
	Server5xx int64
	Client4xx int64
	Sources   int64
	// ErrorRate is 5xx over requests, not error records over all records,
	// which would mix a subsystem complaining with a visitor seeing a 500.
	ErrorRate float64
	// DirectHits are request records with no cf_ray: something reached the
	// origin without crossing the tunnel.
	DirectHits int64
}

// totals reads rollups, so it is correct over any range. DirectHits is the one
// exception and reads raw rows.
func totals(ctx context.Context, db *sql.DB, f filter) Totals {
	var t Totals

	// Health checks are excluded so the tiles describe the same population as
	// the raw panels beside them. They stay visible under component 'healthz'.
	w, args := f.where("hour")
	err := db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(count), 0),
		  COALESCE(SUM(CASE WHEN level = 'ERROR' THEN count ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN level = 'WARN'  THEN count ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN status > 0                    THEN count ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN status >= 500                 THEN count ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN status >= 400 AND status < 500 THEN count ELSE 0 END), 0),
		  COUNT(DISTINCT source)
		FROM rollups WHERE `+w+` AND component != 'healthz'`, args...).
		Scan(&t.Records, &t.Errors, &t.Warnings, &t.Requests, &t.Server5xx, &t.Client4xx, &t.Sources)
	queryFailed("totals", err)

	if t.Requests > 0 {
		t.ErrorRate = float64(t.Server5xx) * 100 / float64(t.Requests)
	}

	// cf_ray cannot be a rollup dimension, being unique per request, so this
	// reads raw rows and only means anything inside rawRetention. Loopback is
	// excluded: a self-probe never crossed the tunnel and never carries a
	// CF-Ray. A record with no IP at all is still counted.
	rw, rargs := f.where("ts")
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM records
		WHERE `+rw+` AND msg = 'request' AND cf_ray = ''
		  AND ip NOT IN ('127.0.0.1', '::1')`, rargs...).Scan(&t.DirectHits)
	queryFailed("direct hits", err)

	return t
}

// volumeGraph buckets the record count over time, from rollups. The bucket
// width follows the range: hourly up to three days, six-hourly up to two weeks,
// daily beyond.
func volumeGraph(ctx context.Context, db *sql.DB, f filter) []GraphPoint {
	span := f.EndMS - f.StartMS
	const hourMS = int64(60 * 60 * 1000)

	bucket := hourMS
	layout := "Jan 2 15:04"
	switch {
	// Past about ten months the first and last labels collide without a year.
	case span > 300*24*hourMS:
		bucket = 24 * hourMS
		layout = "Jan 2 2006"
	case span > 14*24*hourMS:
		bucket = 24 * hourMS
		layout = "Jan 2"
	case span > 3*24*hourMS:
		bucket = 6 * hourMS
		layout = "Jan 2 15h"
	}

	w, args := f.where("hour")
	args = append([]any{bucket, bucket}, args...)

	rows, err := db.QueryContext(ctx, `
		SELECT hour / ? * ? AS bucket,
		       SUM(count),
		       SUM(CASE WHEN level = 'ERROR' OR status >= 500 THEN count ELSE 0 END)
		FROM rollups WHERE `+w+`
		GROUP BY bucket ORDER BY bucket`, args...)
	if err != nil {
		queryFailed("volume graph", err)
		return nil
	}
	defer rows.Close()

	// Empty buckets return no row, and skipping them would draw a quiet hour
	// as a straight line between its neighbours, so the series is filled.
	seen := make(map[int64]GraphPoint)
	for rows.Next() {
		var at, count, errs int64
		if err := rows.Scan(&at, &count, &errs); err != nil {
			queryFailed("volume graph scan", err)
			return nil
		}
		seen[at] = GraphPoint{Count: count, Errors: errs}
	}
	queryFailed("volume graph rows", rows.Err())

	start := f.StartMS / bucket * bucket
	out := make([]GraphPoint, 0, span/bucket+1)
	for at := start; at < f.EndMS; at += bucket {
		p := seen[at]
		p.Label = time.UnixMilli(at).UTC().Format(layout)
		out = append(out, p)
	}
	return out
}

// breakdown is the shape every "top N by X" panel shares, over rollups.
func breakdown(ctx context.Context, db *sql.DB, f filter, column string, limit int) []LabelCount {
	// column is interpolated into the SQL, so it is allowlisted here rather
	// than trusted from a caller.
	switch column {
	case "source", "level", "component":
	default:
		queryFailed("breakdown", fmt.Errorf("refusing to group by %q", column))
		return nil
	}

	w, args := f.where("hour")
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT `+column+`, SUM(count) AS n FROM rollups
		WHERE `+w+` AND `+column+` != ''
		GROUP BY `+column+` ORDER BY n DESC LIMIT ?`, args...)
	if err != nil {
		queryFailed("breakdown "+column, err)
		return nil
	}
	defer rows.Close()
	return scanLabelCounts(rows, "breakdown "+column)
}

// statusClasses is the 2xx/3xx/4xx/5xx split, from rollups.
func statusClasses(ctx context.Context, db *sql.DB, f filter) []LabelCount {
	w, args := f.where("hour")
	rows, err := db.QueryContext(ctx, `
		SELECT CAST(status / 100 AS INTEGER) || 'xx' AS class, SUM(count) AS n
		FROM rollups WHERE `+w+` AND status > 0
		GROUP BY class ORDER BY class`, args...)
	if err != nil {
		queryFailed("status classes", err)
		return nil
	}
	defer rows.Close()
	return scanLabelCounts(rows, "status classes")
}

// topPaths reads raw rows: path is unbounded enough that keying rollups on it
// would make that table grow like the raw one.
func topPaths(ctx context.Context, db *sql.DB, f filter, limit int) []LabelCount {
	w, args := f.where("ts")
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT path, COUNT(*) AS n FROM records
		WHERE `+w+` AND path != ''
		GROUP BY path ORDER BY n DESC LIMIT ?`, args...)
	if err != nil {
		queryFailed("top paths", err)
		return nil
	}
	defer rows.Close()
	return scanLabelCounts(rows, "top paths")
}

func scanLabelCounts(rows *sql.Rows, what string) []LabelCount {
	var out []LabelCount
	for rows.Next() {
		var lc LabelCount
		if err := rows.Scan(&lc.Label, &lc.Count); err != nil {
			queryFailed(what+" scan", err)
			return out
		}
		out = append(out, lc)
	}
	queryFailed(what+" rows", rows.Err())
	return out
}

// latency computes the whole distribution in one CUME_DIST pass, so all three
// percentiles cost one sort. Not PERCENT_RANK: it gives the largest row 1.0, so
// a threshold test can never select the slowest sample.
func latency(ctx context.Context, db *sql.DB, f filter) Latency {
	var l Latency
	w, args := f.where("ts")

	var p50, p95, p99 sql.NullFloat64
	err := db.QueryRowContext(ctx, `
		WITH ranked AS (
		  SELECT duration_ms,
		         CUME_DIST() OVER (ORDER BY duration_ms) AS cd
		  FROM records WHERE `+w+` AND duration_ms > 0
		)
		SELECT COUNT(*),
		       COALESCE(AVG(duration_ms), 0),
		       COALESCE(MAX(duration_ms), 0),
		       MIN(CASE WHEN cd >= 0.50 THEN duration_ms END),
		       MIN(CASE WHEN cd >= 0.95 THEN duration_ms END),
		       MIN(CASE WHEN cd >= 0.99 THEN duration_ms END)
		FROM ranked`, args...).
		Scan(&l.Count, &l.Mean, &l.Max, &p50, &p95, &p99)
	queryFailed("latency", err)

	l.P50 = p50.Float64
	l.P95 = p95.Float64
	l.P99 = p99.Float64
	return l
}

// slowestPaths ranks by p95, not mean, so an endpoint that is fast a thousand
// times and terrible twice still surfaces. CUME_DIST for the reason in latency.
func slowestPaths(ctx context.Context, db *sql.DB, f filter, limit int) []PathRow {
	w, args := f.where("ts")
	args = append(args, minPathSamples, limit)

	rows, err := db.QueryContext(ctx, `
		WITH ranked AS (
		  SELECT path, duration_ms, status,
		         CUME_DIST() OVER (PARTITION BY path ORDER BY duration_ms) AS cd
		  FROM records
		  WHERE `+w+` AND path != '' AND duration_ms > 0
		)
		SELECT path,
		       COUNT(*),
		       MIN(CASE WHEN cd >= 0.95 THEN duration_ms END),
		       MAX(duration_ms),
		       AVG(duration_ms),
		       SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END)
		FROM ranked
		GROUP BY path
		HAVING COUNT(*) >= ?
		ORDER BY 3 DESC
		LIMIT ?`, args...)
	if err != nil {
		queryFailed("slowest paths", err)
		return nil
	}
	defer rows.Close()

	var out []PathRow
	for rows.Next() {
		var p PathRow
		var p95 sql.NullFloat64
		if err := rows.Scan(&p.Path, &p.Count, &p95, &p.Max, &p.Mean, &p.Errors); err != nil {
			queryFailed("slowest paths scan", err)
			return out
		}
		// Every CUME_DIST partition has a row reaching 1.0, so a NULL here
		// would mean an empty group, which HAVING already excludes.
		p.P95 = p95.Float64
		if !p95.Valid {
			p.P95 = p.Max
		}
		out = append(out, p)
	}
	queryFailed("slowest paths rows", rows.Err())
	return out
}

// minPathSamples keeps a path hit once from topping a latency ranking on one
// cold start.
const minPathSamples = 5

// sources is one line per site for the overview. Counts come from rollups, so
// they are right over any range; p95 and last-seen come from raw rows and are
// blank for a source quiet longer than the retention window.
func sources(ctx context.Context, db *sql.DB, f filter) []SourceRow {
	w, args := f.where("hour")
	rows, err := db.QueryContext(ctx, `
		SELECT source,
		       SUM(count),
		       SUM(CASE WHEN level = 'ERROR' THEN count ELSE 0 END),
		       SUM(CASE WHEN status > 0 THEN count ELSE 0 END),
		       SUM(CASE WHEN status >= 500 THEN count ELSE 0 END),
		       SUM(CASE WHEN status >= 400 AND status < 500 THEN count ELSE 0 END)
		FROM rollups WHERE `+w+` AND component != 'healthz'
		GROUP BY source ORDER BY 2 DESC`, args...)
	if err != nil {
		queryFailed("sources", err)
		return nil
	}
	defer rows.Close()

	var out []SourceRow
	for rows.Next() {
		var s SourceRow
		if err := rows.Scan(&s.Source, &s.Records, &s.Errors, &s.Requests, &s.Server5xx, &s.Client4xx); err != nil {
			queryFailed("sources scan", err)
			return out
		}
		if s.Requests > 0 {
			s.ErrorRate = float64(s.Server5xx) * 100 / float64(s.Requests)
		}
		out = append(out, s)
	}
	queryFailed("sources rows", rows.Err())

	// A second pass rather than a join: the two tables cover different
	// windows, and a join would drop any source whose raw rows have aged out.
	liveCutoff := time.Now().Add(-15 * time.Minute).UnixMilli()
	for i := range out {
		sf := f
		sf.Source = out[i].Source
		sw, sargs := sf.where("ts")

		var last sql.NullInt64
		err := db.QueryRowContext(ctx,
			`SELECT MAX(ts) FROM records WHERE `+sw, sargs...).Scan(&last)
		queryFailed("source last seen", err)
		if last.Valid {
			out[i].LastSeen = time.UnixMilli(last.Int64).UTC().Format("2006-01-02 15:04")
			out[i].IsLive = last.Int64 >= liveCutoff
		}
		out[i].P95 = latency(ctx, db, sf).P95
	}
	return out
}

// recentRecords backs the search results. The limit is applied in SQL, because
// the one way to make this page slow is to render every row into a template.
func recentRecords(ctx context.Context, db *sql.DB, f filter, limit, offset int) []RecordRow {
	w, args := f.where("ts")
	args = append(args, limit, offset)

	rows, err := db.QueryContext(ctx, `
		SELECT id, source, ts, level, msg, component, method, path, host,
		       status, duration_ms, ip, cf_ray, attrs
		FROM records WHERE `+w+`
		ORDER BY ts DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		queryFailed("recent records", err)
		return nil
	}
	defer rows.Close()

	var out []RecordRow
	for rows.Next() {
		var r RecordRow
		if err := rows.Scan(&r.ID, &r.Source, &r.TS, &r.Level, &r.Msg, &r.Component,
			&r.Method, &r.Path, &r.Host, &r.Status, &r.DurationMS, &r.IP, &r.CFRay, &r.Attrs); err != nil {
			queryFailed("recent records scan", err)
			return out
		}
		r.Time = time.UnixMilli(r.TS).UTC().Format("2006-01-02 15:04:05")
		out = append(out, r)
	}
	queryFailed("recent records rows", rows.Err())
	return out
}

// errorFeed ORs level and status, so a 500 logged at INFO and an ERROR with no
// status both land on the same list.
func errorFeed(ctx context.Context, db *sql.DB, f filter, limit int) []RecordRow {
	w, args := f.where("ts")
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, `
		SELECT id, source, ts, level, msg, component, method, path, host,
		       status, duration_ms, ip, cf_ray, attrs
		FROM records WHERE `+w+` AND (level = 'ERROR' OR level = 'WARN' OR status >= 500)
		ORDER BY ts DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		queryFailed("error feed", err)
		return nil
	}
	defer rows.Close()

	var out []RecordRow
	for rows.Next() {
		var r RecordRow
		if err := rows.Scan(&r.ID, &r.Source, &r.TS, &r.Level, &r.Msg, &r.Component,
			&r.Method, &r.Path, &r.Host, &r.Status, &r.DurationMS, &r.IP, &r.CFRay, &r.Attrs); err != nil {
			queryFailed("error feed scan", err)
			return out
		}
		r.Time = time.UnixMilli(r.TS).UTC().Format("2006-01-02 15:04:05")
		out = append(out, r)
	}
	queryFailed("error feed rows", rows.Err())
	return out
}

// SiteStats is the only data on this site visible without a session: counts and
// a start date, nothing about what any record says.
type SiteStats struct {
	Records   int64
	Sources   int64
	FirstSeen string
	RawDays   int
}

func siteStats(ctx context.Context, db *sql.DB) SiteStats {
	s := SiteStats{RawDays: int(rawRetention / (24 * time.Hour))}

	var first sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(count), 0), COUNT(DISTINCT source), MIN(hour) FROM rollups`).
		Scan(&s.Records, &s.Sources, &first)
	queryFailed("site stats", err)
	if first.Valid {
		s.FirstSeen = time.UnixMilli(first.Int64).UTC().Format("Jan 2006")
	}
	return s
}

// sourceExists reads rollups, so a source whose raw lines have aged out still
// resolves.
func sourceExists(ctx context.Context, db *sql.DB, source string) bool {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM rollups WHERE source = ? LIMIT 1`, source).Scan(&n)
	if err == sql.ErrNoRows {
		return false
	}
	queryFailed("source exists", err)
	// Anything but a clean "no such row" resolves to "exists", so a broken
	// database shows an empty dashboard rather than a 404 on a real source.
	return true
}

// knownSources reads rollups, so a source that stopped reporting long ago is
// still offered in the filter.
func knownSources(ctx context.Context, db *sql.DB) []string {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT source FROM rollups ORDER BY source`)
	if err != nil {
		queryFailed("known sources", err)
		return nil
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			queryFailed("known sources scan", err)
			return out
		}
		out = append(out, s)
	}
	queryFailed("known sources rows", rows.Err())
	return out
}
