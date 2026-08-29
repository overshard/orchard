package main

// Dashboard aggregations. The hot fields are typed columns rather than JSON,
// so most of these are a COUNT(*) over one column. Time arithmetic is in unix
// milliseconds throughout, matching events.created_at.
//
// Every function here logs its database error and returns a zero value. The
// dashboard renders seventeen independent panels, and one failing aggregation
// should leave a blank panel rather than a 500 where the other sixteen would
// have been.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The events the collector emits on its own. Everything else a site sends is a
// custom event, which is how the dashboard discovers them without being told.
var builtInEvents = []string{"session_start", "page_view", "page_leave", "click", "scroll"}

// Page-leave timings outside this band measure nothing: under a second is a
// bounce or a double fire, over thirty minutes is a tab left open over
// lunch.
const (
	timeOnPageMinS = 1.0
	timeOnPageMaxS = 30.0 * 60.0
)

// LabelCount is one row of any "top N by something" breakdown.
type LabelCount struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// GraphPoint is one bucket of the time series. Label is preformatted because
// the bucket width varies and only the query knows which it produced.
type GraphPoint struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// EventCard is one metric tile. Value is a string because most tiles are
// counts, while two are a percentage and a duration with their units baked
// in.
type EventCard struct {
	Name          string `json:"name"`
	Value         string `json:"value"`
	PercentChange int64  `json:"percent_change"`
	HelpText      string `json:"help_text,omitempty"`
}

// CustomEventDescriptor is one row of the "which custom events exist, and
// which are pinned" list behind the dashboard's card picker.
type CustomEventDescriptor struct {
	Event  string `json:"event"`
	Active bool   `json:"active"`
}

// BotTraffic is the bot panel. Bots live in their own table so they cannot
// contaminate a human aggregation.
type BotTraffic struct {
	Total    int64        `json:"total"`
	TopBots  []LabelCount `json:"top_bots"`
	TopPages []LabelCount `json:"top_pages"`
}

// EventCounts is the five headline counts, gathered in one pass.
type EventCounts struct {
	SessionStart int64
	PageView     int64
	Click        int64
	Scroll       int64
	Total        int64
}

// pctChange is the period-over-period delta shown on each card. Zero previous
// returns zero rather than infinity, which would otherwise put "+100%" on
// every card of a property's first week.
func pctChange(current, previous float64) int64 {
	if previous == 0 {
		return 0
	}
	return int64(math.Round((current - previous) / previous * 100))
}

// filterClause implements the dashboard's "only this page" filter. It returns
// SQL to append and the argument to go with it, so callers stay parameterised
// rather than interpolating a URL a visitor supplied.
func filterClause(filterURL string) (string, []any) {
	if filterURL == "" {
		return "", nil
	}
	return " AND url = ?", []any{filterURL}
}

// placeholders renders "?,?,?" for an IN clause of n items.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func logQuery(what string, err error) {
	if err != nil && err != sql.ErrNoRows {
		slog.Info(fmt.Sprintf("query %s: %v", what, err))
	}
}

// totalLiveUsers counts distinct visitors seen in the last thirty minutes.
// Not bounded by the dashboard's date range on purpose: "live" means now.
func totalLiveUsers(ctx context.Context, db *sql.DB, propertyID uuid.UUID) int64 {
	cutoff := time.Now().Add(-30 * time.Minute).UnixMilli()
	var n int64
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM events
		 WHERE property_id = ? AND created_at >= ? AND user_id IS NOT NULL`,
		propertyID[:], cutoff).Scan(&n)
	logQuery("total_live_users", err)
	return n
}

func eventCounts(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS int64, filterURL string) EventCounts {
	extraSQL, extraArgs := filterClause(filterURL)
	query := `SELECT
	    SUM(CASE WHEN event = 'session_start' THEN 1 ELSE 0 END),
	    SUM(CASE WHEN event = 'page_view'     THEN 1 ELSE 0 END),
	    SUM(CASE WHEN event = 'click'         THEN 1 ELSE 0 END),
	    SUM(CASE WHEN event = 'scroll'        THEN 1 ELSE 0 END),
	    COUNT(*)
	  FROM events
	  WHERE property_id = ? AND created_at >= ? AND created_at <= ?` + extraSQL

	args := append([]any{propertyID[:], startMS, endMS}, extraArgs...)

	// SUM over no rows is NULL, not 0, so every column but the COUNT comes
	// back through a nullable.
	var ss, pv, cl, sc sql.NullInt64
	var total int64
	err := db.QueryRowContext(ctx, query, args...).Scan(&ss, &pv, &cl, &sc, &total)
	logQuery("event_counts", err)
	return EventCounts{
		SessionStart: ss.Int64,
		PageView:     pv.Int64,
		Click:        cl.Int64,
		Scroll:       sc.Int64,
		Total:        total,
	}
}

// engagedUsers is the share of visitors with more than ten events, as a
// percentage of session starts.
func engagedUsers(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS int64, filterURL string, sessionStarts int64) float64 {
	if sessionStarts == 0 {
		return 0
	}
	extraSQL, extraArgs := filterClause(filterURL)
	query := `SELECT COUNT(*) FROM (
	    SELECT user_id FROM events
	    WHERE property_id = ? AND created_at >= ? AND created_at <= ?
	          AND user_id IS NOT NULL` + extraSQL + `
	    GROUP BY user_id HAVING COUNT(*) >= 10
	  )`
	args := append([]any{propertyID[:], startMS, endMS}, extraArgs...)

	var engaged int64
	err := db.QueryRowContext(ctx, query, args...).Scan(&engaged)
	logQuery("engaged_users", err)
	return math.Round(float64(engaged)/float64(sessionStarts)*100*100) / 100
}

func avgTimeOnPage(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS int64, filterURL string) float64 {
	extraSQL, extraArgs := filterClause(filterURL)
	query := `SELECT AVG(time_on_page_ms / 1000.0) FROM events
	  WHERE property_id = ? AND created_at >= ? AND created_at <= ?
	        AND event = 'page_leave'
	        AND time_on_page_ms IS NOT NULL
	        AND time_on_page_ms / 1000.0 BETWEEN ? AND ?` + extraSQL
	args := append([]any{propertyID[:], startMS, endMS, timeOnPageMinS, timeOnPageMaxS}, extraArgs...)

	var avg sql.NullFloat64
	err := db.QueryRowContext(ctx, query, args...).Scan(&avg)
	logQuery("avg_time_on_page", err)
	return math.Round(avg.Float64*100) / 100
}

// standardEventCards builds the seven tiles every property gets, each with its
// change against the immediately preceding period of the same length.
func standardEventCards(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS, prevStartMS, prevEndMS int64, filterURL string) []EventCard {
	cur := eventCounts(ctx, db, propertyID, startMS, endMS, filterURL)
	prev := eventCounts(ctx, db, propertyID, prevStartMS, prevEndMS, filterURL)

	cards := []EventCard{
		{
			Name:          "Total session starts",
			Value:         fmt.Sprintf("%d", cur.SessionStart),
			PercentChange: pctChange(float64(cur.SessionStart), float64(prev.SessionStart)),
			HelpText:      "Unique users visiting your site for your selected date range.",
		},
		{
			Name:          "Total page views",
			Value:         fmt.Sprintf("%d", cur.PageView),
			PercentChange: pctChange(float64(cur.PageView), float64(prev.PageView)),
			HelpText:      "Total pages viewed for your selected date range.",
		},
		{
			Name:          "Total clicks",
			Value:         fmt.Sprintf("%d", cur.Click),
			PercentChange: pctChange(float64(cur.Click), float64(prev.Click)),
			HelpText:      "Total clicks users made on all your pages for your selected date range.",
		},
		{
			Name:          "Total scrolls",
			Value:         fmt.Sprintf("%d", cur.Scroll),
			PercentChange: pctChange(float64(cur.Scroll), float64(prev.Scroll)),
			HelpText:      "Total scrolls users made on all your pages for your selected date range.",
		},
		{
			Name:          "Total events",
			Value:         fmt.Sprintf("%d", cur.Total),
			PercentChange: pctChange(float64(cur.Total), float64(prev.Total)),
			HelpText:      "All events for your selected date range, including custom events.",
		},
	}

	engCur := engagedUsers(ctx, db, propertyID, startMS, endMS, filterURL, cur.SessionStart)
	engPrev := engagedUsers(ctx, db, propertyID, prevStartMS, prevEndMS, filterURL, prev.SessionStart)
	cards = append(cards, EventCard{
		Name:          "Total user engagement",
		Value:         trimFloat(engCur) + "%",
		PercentChange: pctChange(engCur, engPrev),
		HelpText:      "An engaged user is a user with more than 10 events collected for your selected date range.",
	})

	tCur := avgTimeOnPage(ctx, db, propertyID, startMS, endMS, filterURL)
	tPrev := avgTimeOnPage(ctx, db, propertyID, prevStartMS, prevEndMS, filterURL)
	cards = append(cards, EventCard{
		Name:          "Avg. time on page",
		Value:         trimFloat(tCur) + "s",
		PercentChange: pctChange(tCur, tPrev),
		HelpText:      "Average time a user spends on each page. Sessions over 30 minutes are excluded as idle.",
	})

	return cards
}

// trimFloat renders the shortest form that round-trips: no trailing zeros, no
// decimal point on a whole number. 'f' with precision -1 rather than %v, which
// is %g underneath and would flip to "1.2e+06" at the top of the range.
func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// customEventCards returns the pinned custom-event tiles and, separately,
// every custom event this property has ever recorded so the picker can list
// them.
func customEventCards(ctx context.Context, db *sql.DB, propertyID uuid.UUID, cards []CustomCard, startMS, endMS, prevStartMS, prevEndMS int64, filterURL string) ([]EventCard, []CustomEventDescriptor) {
	// Unbounded by the date range, so the picker still lists an event that
	// stopped firing last month.
	query := `SELECT DISTINCT event FROM events
	  WHERE property_id = ? AND event NOT IN (` + placeholders(len(builtInEvents)) + `)
	  ORDER BY event`
	args := []any{propertyID[:]}
	for _, b := range builtInEvents {
		args = append(args, b)
	}

	var names []string
	rows, err := db.QueryContext(ctx, query, args...)
	logQuery("custom_event_names", err)
	if err == nil {
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				names = append(names, n)
			}
		}
		rows.Close()
	}

	active := make(map[string]bool, len(cards))
	for _, c := range cards {
		if c.Value {
			active[c.Event] = true
		}
	}

	descriptors := make([]CustomEventDescriptor, 0, len(names))
	for _, n := range names {
		descriptors = append(descriptors, CustomEventDescriptor{Event: n, Active: active[n]})
	}

	if len(active) == 0 {
		return nil, descriptors
	}

	// Iterate names, not the active map, so the tiles come out in the
	// picker's order rather than Go's randomised map order.
	activeNames := make([]string, 0, len(active))
	for _, n := range names {
		if active[n] {
			activeNames = append(activeNames, n)
		}
	}
	if len(activeNames) == 0 {
		return nil, descriptors
	}

	countFor := func(periodStart, periodEnd int64) map[string]int64 {
		extraSQL, extraArgs := filterClause(filterURL)
		q := `SELECT event, COUNT(*) FROM events
		  WHERE property_id = ? AND created_at >= ? AND created_at <= ?
		        AND event IN (` + placeholders(len(activeNames)) + `)` + extraSQL + `
		  GROUP BY event`
		a := []any{propertyID[:], periodStart, periodEnd}
		for _, n := range activeNames {
			a = append(a, n)
		}
		a = append(a, extraArgs...)

		out := make(map[string]int64, len(activeNames))
		r, err := db.QueryContext(ctx, q, a...)
		logQuery("custom_event_counts", err)
		if err != nil {
			return out
		}
		defer r.Close()
		for r.Next() {
			var name string
			var c int64
			if r.Scan(&name, &c) == nil {
				out[name] = c
			}
		}
		return out
	}

	curMap := countFor(startMS, endMS)
	prevMap := countFor(prevStartMS, prevEndMS)

	out := make([]EventCard, 0, len(activeNames))
	for _, name := range activeNames {
		v := curMap[name]
		p := prevMap[name]
		out = append(out, EventCard{
			Name:          name,
			Value:         fmt.Sprintf("%d", v),
			PercentChange: pctChange(float64(v), float64(p)),
		})
	}
	return out, descriptors
}

// eventsGraph is the time series, bucketed by day, week or month depending on
// how wide the range is, and stepping backwards from endDate.
//
// Anchored to the requested end date rather than to today, since stepping back
// from today charts any historical range as a row of zeros beside metric cards
// showing real numbers.
func eventsGraph(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS int64, filterURL string, endDate time.Time, rangeDays int64) []GraphPoint {
	extraSQL, extraArgs := filterClause(filterURL)
	query := `SELECT date(created_at / 1000, 'unixepoch') AS day, COUNT(*)
	  FROM events
	  WHERE property_id = ? AND created_at >= ? AND created_at <= ?` + extraSQL + `
	  GROUP BY day`
	args := append([]any{propertyID[:], startMS, endMS}, extraArgs...)

	byDay := map[string]int64{}
	rows, err := db.QueryContext(ctx, query, args...)
	logQuery("events_graph", err)
	if err == nil {
		for rows.Next() {
			var day string
			var c int64
			if rows.Scan(&day, &c) == nil {
				byDay[day] = c
			}
		}
		rows.Close()
	}

	key := func(t time.Time) string { return t.Format("2006-01-02") }
	bucketSum := func(start time.Time, days int) int64 {
		var sum int64
		for j := 0; j < days; j++ {
			sum += byDay[key(start.AddDate(0, 0, j))]
		}
		return sum
	}

	type point struct {
		date  time.Time
		count int64
	}
	var points []point

	switch {
	case rangeDays <= 28:
		for i := int64(0); i < rangeDays; i++ {
			d := endDate.AddDate(0, 0, -int(i))
			points = append(points, point{d, byDay[key(d)]})
		}
	case rangeDays <= 6*28:
		weeks := rangeDays / 7
		for w := int64(0); w < weeks; w++ {
			d := endDate.AddDate(0, 0, -int(7*w))
			points = append(points, point{d, bucketSum(d, 7)})
		}
	default:
		months := rangeDays / 28
		for m := int64(0); m < months; m++ {
			d := endDate.AddDate(0, 0, -int(28*m))
			points = append(points, point{d, bucketSum(d, 28)})
		}
	}

	sort.Slice(points, func(i, j int) bool { return points[i].date.Before(points[j].date) })

	out := make([]GraphPoint, 0, len(points))
	for _, p := range points {
		out = append(out, GraphPoint{Label: formatGraphLabel(p.date), Count: p.count})
	}
	return out
}

// formatGraphLabel renders "Jan 5". Go has no unpadded day verb, so the padded
// form is trimmed.
func formatGraphLabel(t time.Time) string {
	return t.Format("Jan") + " " + strings.TrimPrefix(t.Format("02"), "0")
}

// topByColumn is the shape every breakdown panel shares: group by one column,
// order by count, take the top N.
//
// column and countExpr are interpolated into the SQL rather than bound, which
// is safe only because every caller passes a literal. Nothing here takes a
// column name from a request.
func topByColumn(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS int64, filterURL, column, event string, limit int64, distinctUsers bool) []LabelCount {
	countExpr := "COUNT(*)"
	if distinctUsers {
		countExpr = "COUNT(DISTINCT user_id)"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, `SELECT %s, %s FROM events
	  WHERE property_id = ? AND created_at >= ? AND created_at <= ?
	        AND %s IS NOT NULL AND %s != ''`, column, countExpr, column, column)

	args := []any{propertyID[:], startMS, endMS}
	if distinctUsers {
		sb.WriteString(" AND user_id IS NOT NULL")
	}
	if event != "" {
		sb.WriteString(" AND event = ?")
		args = append(args, event)
	}
	extraSQL, extraArgs := filterClause(filterURL)
	sb.WriteString(extraSQL)
	args = append(args, extraArgs...)
	fmt.Fprintf(&sb, " GROUP BY %s ORDER BY %s DESC LIMIT ?", column, countExpr)
	args = append(args, limit)

	return scanLabelCounts(ctx, db, "top_by_"+column, sb.String(), args...)
}

func scanLabelCounts(ctx context.Context, db *sql.DB, what, query string, args ...any) []LabelCount {
	rows, err := db.QueryContext(ctx, query, args...)
	logQuery(what, err)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []LabelCount
	for rows.Next() {
		var lc LabelCount
		if rows.Scan(&lc.Label, &lc.Count) == nil {
			out = append(out, lc)
		}
	}
	return out
}

// eventsByScreenSize counts distinct visitors per screen size.
//
// Filtered to page_view rather than session_start, like the three breakdowns
// below it: the collector's user-id cookie suppresses session_start after a
// visitor's first visit, so a session_start breakdown would count only new
// visitors.
func eventsByScreenSize(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS int64, filterURL string, limit int64) []LabelCount {
	extraSQL, extraArgs := filterClause(filterURL)
	query := `SELECT screen_width, screen_height, COUNT(DISTINCT user_id) FROM events
	  WHERE property_id = ? AND created_at >= ? AND created_at <= ?
	        AND event = 'page_view'
	        AND screen_width IS NOT NULL
	        AND user_id IS NOT NULL` + extraSQL + `
	  GROUP BY screen_width, screen_height
	  ORDER BY COUNT(DISTINCT user_id) DESC LIMIT ?`
	args := append([]any{propertyID[:], startMS, endMS}, extraArgs...)
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	logQuery("events_by_screen_size", err)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []LabelCount
	for rows.Next() {
		var w, h sql.NullInt64
		var c int64
		if rows.Scan(&w, &h, &c) == nil {
			out = append(out, LabelCount{
				Label: fmt.Sprintf("%dx%d", w.Int64, h.Int64),
				Count: c,
			})
		}
	}
	return out
}

func eventsByDevice(ctx context.Context, db *sql.DB, id uuid.UUID, s, e int64, f string, limit int64) []LabelCount {
	return topByColumn(ctx, db, id, s, e, f, "device", "page_view", limit, true)
}

func eventsByBrowser(ctx context.Context, db *sql.DB, id uuid.UUID, s, e int64, f string, limit int64) []LabelCount {
	return topByColumn(ctx, db, id, s, e, f, "browser", "page_view", limit, true)
}

func eventsByPlatform(ctx context.Context, db *sql.DB, id uuid.UUID, s, e int64, f string, limit int64) []LabelCount {
	return topByColumn(ctx, db, id, s, e, f, "platform", "page_view", limit, true)
}

func eventsByPageURL(ctx context.Context, db *sql.DB, id uuid.UUID, s, e int64, f string, limit int64) []LabelCount {
	return topByColumn(ctx, db, id, s, e, f, "url", "", limit, false)
}

func pageViewsByPageURL(ctx context.Context, db *sql.DB, id uuid.UUID, s, e int64, f string, limit int64) []LabelCount {
	return topByColumn(ctx, db, id, s, e, f, "url", "page_view", limit, false)
}

func sessionStartsByReferrer(ctx context.Context, db *sql.DB, id uuid.UUID, s, e int64, f string, limit int64) []LabelCount {
	return topByColumn(ctx, db, id, s, e, f, "referrer", "session_start", limit, false)
}

// pageViewsByUTM maps a campaign field name to its column. The map is what
// keeps a request-supplied field out of topByColumn's interpolation: anything
// not listed returns nothing rather than becoming SQL.
func pageViewsByUTM(ctx context.Context, db *sql.DB, id uuid.UUID, s, e int64, f, field string, limit int64) []LabelCount {
	column, ok := map[string]string{
		"source":   "utm_source",
		"medium":   "utm_medium",
		"campaign": "utm_campaign",
		"term":     "utm_term",
		"content":  "utm_content",
	}[field]
	if !ok {
		return nil
	}
	return topByColumn(ctx, db, id, s, e, f, column, "page_view", limit, false)
}

func eventsByCustomEvent(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS int64, filterURL string, limit int64) []LabelCount {
	extraSQL, extraArgs := filterClause(filterURL)
	query := `SELECT event, COUNT(*) FROM events
	  WHERE property_id = ? AND created_at >= ? AND created_at <= ?
	        AND event NOT IN (` + placeholders(len(builtInEvents)) + `)` + extraSQL + `
	  GROUP BY event ORDER BY COUNT(*) DESC LIMIT ?`
	args := []any{propertyID[:], startMS, endMS}
	for _, b := range builtInEvents {
		args = append(args, b)
	}
	args = append(args, extraArgs...)
	args = append(args, limit)

	return scanLabelCounts(ctx, db, "events_by_custom_event", query, args...)
}

// sessionStartsByCountry feeds the world map, keyed by ISO country code.
func sessionStartsByCountry(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS int64, filterURL string) map[string]int64 {
	extraSQL, extraArgs := filterClause(filterURL)
	query := `SELECT country, COUNT(*) FROM events
	  WHERE property_id = ? AND created_at >= ? AND created_at <= ?
	        AND event = 'session_start' AND country IS NOT NULL` + extraSQL + `
	  GROUP BY country`
	args := append([]any{propertyID[:], startMS, endMS}, extraArgs...)

	out := map[string]int64{}
	rows, err := db.QueryContext(ctx, query, args...)
	logQuery("session_starts_by_country", err)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var country string
		var c int64
		if rows.Scan(&country, &c) == nil {
			out[country] = c
		}
	}
	return out
}

// sessionStartsByCountryRegion feeds the map's admin-1 drill-down, which
// lazy-loads a per-country topojson when a country is clicked.
func sessionStartsByCountryRegion(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS int64, filterURL string) map[string]map[string]int64 {
	extraSQL, extraArgs := filterClause(filterURL)
	query := `SELECT country, region, COUNT(*) FROM events
	  WHERE property_id = ? AND created_at >= ? AND created_at <= ?
	        AND event = 'session_start'
	        AND country IS NOT NULL AND region IS NOT NULL` + extraSQL + `
	  GROUP BY country, region`
	args := append([]any{propertyID[:], startMS, endMS}, extraArgs...)

	out := map[string]map[string]int64{}
	rows, err := db.QueryContext(ctx, query, args...)
	logQuery("session_starts_by_country_region", err)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var country, region string
		var c int64
		if rows.Scan(&country, &region, &c) != nil {
			continue
		}
		if out[country] == nil {
			out[country] = map[string]int64{}
		}
		out[country][region] = c
	}
	return out
}

func botTraffic(ctx context.Context, db *sql.DB, propertyID uuid.UUID, startMS, endMS, limit int64) BotTraffic {
	var total int64
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM bot_events
		 WHERE property_id = ? AND created_at >= ? AND created_at <= ?`,
		propertyID[:], startMS, endMS).Scan(&total)
	logQuery("bot_traffic_total", err)
	if total == 0 {
		return BotTraffic{}
	}

	return BotTraffic{
		Total: total,
		TopBots: scanLabelCounts(ctx, db, "bot_traffic_bots",
			`SELECT bot_name, COUNT(*) FROM bot_events
			 WHERE property_id = ? AND created_at >= ? AND created_at <= ?
			       AND bot_name IS NOT NULL AND bot_name != ''
			 GROUP BY bot_name ORDER BY COUNT(*) DESC LIMIT ?`,
			propertyID[:], startMS, endMS, limit),
		TopPages: scanLabelCounts(ctx, db, "bot_traffic_pages",
			`SELECT url, COUNT(*) FROM bot_events
			 WHERE property_id = ? AND created_at >= ? AND created_at <= ?
			       AND url IS NOT NULL AND url != ''
			 GROUP BY url ORDER BY COUNT(*) DESC LIMIT ?`,
			propertyID[:], startMS, endMS, limit),
	}
}

// parseDateToMS turns a "YYYY-MM-DD" query parameter into a unix-ms bound.
//
// Local time, not UTC. The operator picks dates meaning their own days, so
// resolving the boundary in the server's zone is what makes "today" mean
// today.
func parseDateToMS(date string, endOfDay bool) (int64, bool) {
	d, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return 0, false
	}
	if endOfDay {
		d = d.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	}
	return d.UnixMilli(), true
}
