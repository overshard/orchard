package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// The read path.
//
// Two sources of truth, used deliberately. `rollups` answers "how many, over
// time" for any range, because it is hourly counters kept forever. `records`
// answers "which ones, and how slow", and only inside rawRetention, because
// that is how long the raw lines live. Every query below says which one it is
// reading and why, since asking the wrong table is the one way to get an answer
// that is quietly wrong rather than obviously missing.

// LabelCount is one row of a breakdown.
type LabelCount struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// GraphPoint is one bucket of the volume chart. Errors is carried alongside so
// the chart can show the error band without a second query.
type GraphPoint struct {
	Label  string `json:"label"`
	Count  int64  `json:"count"`
	Errors int64  `json:"errors"`
}

// Latency is the request duration distribution over a window. Percentiles come
// from raw rows, so they are exact rather than interpolated, and they are only
// available inside rawRetention.
type Latency struct {
	Count int64
	Mean  float64
	P50   float64
	P95   float64
	P99   float64
	Max   float64
}

// RecordRow is one raw line, for the search results and the error feed.
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

// SourceRow is one line of the sources table.
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

// PathRow is one row of the slowest-paths table.
type PathRow struct {
	Path   string
	Count  int64
	P95    float64
	Max    float64
	Mean   float64
	Errors int64
}

// filter is the shared WHERE clause behind every query here. Zero values mean
// "no constraint", so one struct covers the overview, a single source's page
// and a search.
type filter struct {
	StartMS int64
	EndMS   int64
	Source  string
	Level   string
	// Component, Method and Status narrow the search page. Status is a class
	// (2, 3, 4, 5) rather than a code, because that is what anybody actually
	// filters by.
	Component   string
	StatusClass int
	// Q matches the message or the path. A substring rather than a token
	// search: these are short strings and the set is already bounded by time,
	// so the index that would make this fast is not worth carrying.
	Q string
}

// where builds the clause and its arguments. Every value is a placeholder;
// nothing here interpolates a string into SQL.
//
// The start bound is floored when the column is `hour`, and that is not a
// nicety. `rollups.hour` holds an hour-floored timestamp, so `hour >= start`
// against an unaligned start silently drops the bucket that *contains* the
// start, whole: every rollup-backed number loses everything from the start to
// the top of the next hour, while the raw panels beside them use exact `ts`
// bounds and do not. resolveWindow already aligns the windows it hands out, so
// in production this is a no-op; it lives here as well because the mistake is
// invisible, and every caller that builds a filter by hand (sources(), and
// every test) would otherwise have to remember it independently. Belt and
// braces on a bug with no symptom.
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

// queryFailed centralises what a broken read does: report it and hand the page
// a zero value. A dashboard panel that is empty because a query failed is worse
// than one that is empty because there is no data, so it is logged either way,
// but a single bad panel does not cost the whole page.
func queryFailed(what string, err error) {
	if err == nil || err == sql.ErrNoRows {
		return
	}
	slog.Error("query failed", slog.String("component", "query"),
		slog.String("query", what), slog.Any("err", err))
}

// ---------------------------------------------------------------- totals

// Totals is the metric tile row.
type Totals struct {
	Records   int64
	Errors    int64
	Warnings  int64
	Requests  int64
	Server5xx int64
	Client4xx int64
	Sources   int64
	// ErrorRate is 5xx over requests, as a percentage. Deliberately not
	// "error records over all records", which mixes a scheduler complaining
	// with a visitor seeing a 500.
	ErrorRate float64
	// DirectHits are request records with no cf_ray, which means something
	// reached the origin without crossing the tunnel. The hardening pass
	// instrumented this and nothing has ever read it until now.
	DirectHits int64
}

// totals reads rollups, so it is correct over any range including ones older
// than the raw retention. DirectHits is the exception and is noted below.
func totals(ctx context.Context, db *sql.DB, f filter) Totals {
	var t Totals

	// Health checks are excluded from every number here, which is what makes
	// the tiles describe the same population as the raw panels beside them.
	// They are the process probing itself over loopback every thirty seconds,
	// roughly 480 an hour across the fleet, and counting them made the p50 read
	// 0.042ms against a real 0.537ms and put /healthz at the top of the busiest
	// paths. They are still counted, under component 'healthz', and still
	// visible in the components breakdown, where "the probe is running" is
	// genuine information.
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

	// cf_ray is not a rollup dimension: adding it would multiply the key
	// space by one column that is unique per request, which is the one thing
	// a rollup must never key on. So this reads raw rows and is therefore
	// only meaningful inside rawRetention, which is the window anybody would
	// investigate an unexpected direct hit in anyway.
	//
	// Loopback is excluded, and that exclusion is the difference between this
	// tile meaning something and meaning nothing. Every container health check
	// is the binary probing itself over 127.0.0.1, which carries no CF-Ray
	// because it never left the container, let alone crossed the tunnel. At
	// one every thirty seconds per site that is roughly 480 an hour, which
	// buried the real signal within an hour of going live and is exactly how
	// this was found. A loopback request cannot be the thing this tile is
	// looking for, by construction.
	//
	// Only loopback. A request record carrying no IP at all is odd rather than
	// exempt, and this counts it: the tile's job is to surface something
	// unexpected, so it fails toward showing a strange record rather than
	// hiding one.
	rw, rargs := f.where("ts")
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM records
		WHERE `+rw+` AND msg = 'request' AND cf_ray = ''
		  AND ip NOT IN ('127.0.0.1', '::1')`, rargs...).Scan(&t.DirectHits)
	queryFailed("direct hits", err)

	return t
}

// ---------------------------------------------------------------- graph

// volumeGraph buckets the record count over time, from rollups.
//
// The bucket width follows the range rather than being fixed: hourly up to
// three days, six-hourly up to two weeks, daily beyond. A year of hourly
// buckets is 8,760 points, which is more pixels than a chart has and more
// numbers than a page should carry.
func volumeGraph(ctx context.Context, db *sql.DB, f filter) []GraphPoint {
	span := f.EndMS - f.StartMS
	const hourMS = int64(60 * 60 * 1000)

	bucket := hourMS
	layout := "Jan 2 15:04"
	switch {
	// Past about ten months the first and last labels are the same day of the
	// same month in different years, and read identically without one.
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

	// Buckets with no records return no row, and a chart that simply skips
	// them draws a quiet hour as a straight line between its neighbours. The
	// gap is the information, so the series is filled.
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

// ---------------------------------------------------------------- breakdowns

// breakdown is the shape every "top N by X" panel shares, over rollups.
func breakdown(ctx context.Context, db *sql.DB, f filter, column string, limit int) []LabelCount {
	// column is chosen from a fixed set by the caller and never from a
	// request. Named explicitly so a future caller cannot pass a query
	// parameter through by accident.
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

// topPaths reads raw rows, because path is not a rollup dimension: a path is
// close enough to unbounded that keying rollups on it would make the rollup
// table grow like the raw one, which is the thing it exists to avoid.
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

// ---------------------------------------------------------------- latency

// latency computes the duration distribution in a single pass.
//
// It used to run four queries: one aggregate plus one
// `ORDER BY duration_ms LIMIT 1 OFFSET ?` per percentile. There is no index on
// duration_ms, so each of those was a full sort of the window, and sources()
// calls this once per source on top of the global call. Measured on 200,000
// records over 30 days: 3.1 seconds for this function alone, inside a ~4.8
// second page render that is invisible today and arrives gradually as the
// database fills.
//
// One CUME_DIST pass is one sort for all three percentiles. CUME_DIST rather
// than PERCENT_RANK because they disagree at exactly the point that matters:
// PERCENT_RANK gives the largest row of a set the value 1.0, so `pr <= 0.95`
// can never select the slowest sample and a five-sample set reports its 4th
// value as "p95". CUME_DIST is the fraction of rows at or below the current
// one, so the smallest value whose CUME_DIST reaches p is the nearest-rank
// pth percentile, which is what these numbers claim to be.
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

// slowestPaths ranks by p95 rather than by mean, because a mean hides the
// endpoint that is fast a thousand times and terrible twice, which is the one
// worth finding.
//
// CUME_DIST, not PERCENT_RANK, for the reason spelled out in latency:
// PERCENT_RANK gives the slowest sample of every path the value 1.0, so
// `pr <= 0.95` could never select it and a path at the five-sample floor
// reported its 4th value, the 80th percentile, under a column headed p95.
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
		// Under CUME_DIST every partition has a row reaching 1.0, so this is
		// defensive rather than expected: a NULL would mean the group was
		// empty, which HAVING already excludes.
		p.P95 = p95.Float64
		if !p95.Valid {
			p.P95 = p.Max
		}
		out = append(out, p)
	}
	queryFailed("slowest paths rows", rows.Err())
	return out
}

// minPathSamples keeps a path that was hit once from topping a latency ranking
// on the strength of a single cold start.
const minPathSamples = 5

// ---------------------------------------------------------------- sources

// sources is the table on the overview: one line per site, its volume, its
// error rate and whether anything has arrived from it recently.
//
// Counts come from rollups so the numbers are right over any range; p95 and
// last-seen come from raw rows, so both are blank for a source that has been
// quiet longer than the retention window. That is the correct answer rather
// than a missing one.
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

	// Last seen and p95 per source, from raw rows. A second pass rather than
	// a join, because the two tables cover different windows and a join would
	// silently drop any source whose raw rows have aged out. Health checks
	// have no raw row at all now, so a source that did nothing but answer its
	// probe shows a real record count and a blank last-seen, which is the
	// honest reading of "it is alive and served nobody".
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

// ---------------------------------------------------------------- raw lines

// recentRecords is the search results and the error feed, both of which are the
// same query with a different filter.
//
// The limit is applied in SQL and the caller decides it, because the one way to
// make this page slow is to render fifty thousand rows into a template.
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

// errorFeed is the newest problems, wherever they came from. Level and status
// are OR'd rather than filtered separately because a 500 logged at INFO and an
// ERROR with no status are both things worth seeing on the same list.
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

// ---------------------------------------------------------------- site stats

// SiteStats is what the public home page shows, and the only numbers on this
// site that are visible without a session. Counts and a start date, nothing
// about what any of it says.
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

// sourceExists reports whether anything has ever been shipped under this name.
// It reads rollups rather than records, so a source that stopped reporting a
// year ago still resolves: its history is real even though its raw lines are
// long gone.
func sourceExists(ctx context.Context, db *sql.DB, source string) bool {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM rollups WHERE source = ? LIMIT 1`, source).Scan(&n)
	if err == sql.ErrNoRows {
		return false
	}
	queryFailed("source exists", err)
	// Anything other than a clean "no such row" resolves to "exists", so a
	// broken database shows an empty dashboard rather than telling the
	// operator a real source was never real.
	return true
}

// knownSources is the filter dropdown on the search page. It reads rollups so
// a source that stopped reporting last year is still offered, which is what
// somebody searching history wants.
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
