package main

// Every aggregation here logs its database error and returns a zero value, so
// one failing panel leaves a blank panel instead of a 500 page. Time arithmetic
// is unix milliseconds throughout, matching events.created_at.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The events the collector emits on its own, everything else is a custom event.
var builtInEvents = []string{"session_start", "page_view", "page_leave", "click", "scroll"}

// Page-leave timings outside this band are a double fire or a tab left open.
const (
	timeOnPageMinS = 1.0
	timeOnPageMaxS = 30.0 * 60.0
)

// LabelCount is one row of any "top N by something" breakdown. Rate is a
// second percentage some rows carry, a bounce rate or a conversion rate.
type LabelCount struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
	Rate  int64  `json:"rate,omitempty"`
}

// GraphPoint is one bucket of the visitors chart and the same bucket of the
// period before it.
type GraphPoint struct {
	Label     string `json:"label"`
	Count     int64  `json:"count"`
	Prev      int64  `json:"prev"`
	PrevLabel string `json:"prev_label"`
}

// EventCard is one metric tile. Value is a string because some tiles carry
// their units in it. Note replaces the change line when a tile has no period
// to compare, and Inverse marks a number where down is good.
type EventCard struct {
	Name          string    `json:"name"`
	Value         string    `json:"value"`
	PercentChange int64     `json:"percent_change"`
	HelpText      string    `json:"help_text,omitempty"`
	Event         string    `json:"-"`
	Series        []float64 `json:"-"`
	Note          string    `json:"-"`
	Inverse       bool      `json:"-"`
	NoPrev        bool      `json:"-"`
}

// Delta is the change line under a tile's number.
func (c EventCard) Delta() string {
	switch {
	case c.Note != "":
		return c.Note
	case c.NoPrev:
		return "nothing before"
	case c.PercentChange == 0:
		return "no change"
	case c.PercentChange > 0:
		return fmt.Sprintf("+%d%%", c.PercentChange)
	}
	return fmt.Sprintf("\u2212%d%%", -c.PercentChange)
}

// DeltaClass colours the change by whether it's good, so a falling bounce
// rate is green.
func (c EventCard) DeltaClass() string {
	if c.Note != "" || c.NoPrev || c.PercentChange == 0 {
		return ""
	}
	if (c.PercentChange > 0) != c.Inverse {
		return "o-ok"
	}
	return "o-warn"
}

// SparkKind picks the spark's colour the same way.
func (c EventCard) SparkKind() string {
	if c.DeltaClass() == "o-warn" {
		return "warn"
	}
	return "ok"
}

// CustomEventDescriptor is one row of the custom event menu, whether it's
// pinned as a card and whether it counts as a goal.
type CustomEventDescriptor struct {
	Event  string `json:"event"`
	Active bool   `json:"active"`
	Goal   bool   `json:"goal"`
	Custom bool   `json:"-"`
}

// BotTraffic is the bot panel, read from the separate bot_events table.
type BotTraffic struct {
	Total    int64        `json:"total"`
	TopBots  []LabelCount `json:"top_bots"`
	TopPages []LabelCount `json:"top_pages"`
}

// pctChange is the period-over-period delta on each card. A zero previous
// returns zero since there's no percentage change from nothing.
func pctChange(current, previous float64) int64 {
	if previous == 0 {
		return 0
	}
	return int64(math.Round((current - previous) / previous * 100))
}

// placeholders renders "?,?,?" for an IN clause of n items.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func logQuery(what string, err error) {
	if err != nil && err != sql.ErrNoRows {
		slog.Info("query", slog.Any("query", what), slog.Any("err", err))
	}
}

// trimFloat uses 'f' with precision -1 instead of %v, which is %g underneath
// and flips to scientific notation at the top of the range.
func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// formatGraphLabel renders "Jan 5", Go has no unpadded day verb, so the padded
// form is trimmed.
func formatGraphLabel(t time.Time) string {
	return t.Format("Jan") + " " + strings.TrimPrefix(t.Format("02"), "0")
}

func scanLabelCounts(ctx context.Context, db querier, what, query string, args ...any) []LabelCount {
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
	logQuery(what, rows.Err())
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

// parseDateToMS turns a "YYYY-MM-DD" query parameter into a unix-ms bound. The
// boundary resolves in Eastern time, since the operator means their own days.
func parseDateToMS(date string, endOfDay bool) (int64, bool) {
	d, err := time.ParseInLocation("2006-01-02", date, eastern)
	if err != nil {
		return 0, false
	}
	if endOfDay {
		d = d.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	}
	return d.UnixMilli(), true
}

// top groups the scoped events by one expression and takes the top N. expr,
// count and where are written into the SQL, so every caller passes a literal.
func (s scope) top(ctx context.Context, db querier, what, expr, count, where string, limit int64) []LabelCount {
	with, args := s.with()
	q := with + ` SELECT ` + expr + `, ` + count + ` FROM ev WHERE ` + expr + ` IS NOT NULL AND ` + expr + ` != ''`
	if where != "" {
		q += ` AND ` + where
	}
	q += ` GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT ?`
	return scanLabelCounts(ctx, db, what, q, append(args, limit)...)
}

// The device, browser, platform and screen panels count visitors on page
// views, since a returning visitor's cookie suppresses their session_start.
func (s scope) visitorsBy(ctx context.Context, db querier, expr string, limit int64) []LabelCount {
	return s.top(ctx, db, "visitors_by_"+expr, expr, "COUNT(DISTINCT user_id)", "event = 'page_view' AND user_id IS NOT NULL", limit)
}

func (s scope) pageViewsBy(ctx context.Context, db querier, expr string, limit int64) []LabelCount {
	return s.top(ctx, db, "page_views_by_"+expr, expr, "COUNT(*)", "event = 'page_view'", limit)
}

const screenExpr = "screen_width || 'x' || screen_height"

func (s scope) customEvents(ctx context.Context, db querier, limit int64) []LabelCount {
	with, args := s.with()
	q := with + ` SELECT event, COUNT(*) FROM ev WHERE event NOT IN (` + placeholders(len(builtInEvents)) + `)
	  GROUP BY event ORDER BY COUNT(*) DESC, event LIMIT ?`
	for _, b := range builtInEvents {
		args = append(args, b)
	}
	return scanLabelCounts(ctx, db, "custom_events", q, append(args, limit)...)
}

// visitsBy ranks visits by their entry page, exit page or referrer. Rate is
// the share of them that saw one page and left.
func (s scope) visitsBy(ctx context.Context, db querier, col string, limit int64) []LabelCount {
	with, args := s.withVisits()
	expr := col
	if col == "ref" {
		expr = "CASE WHEN ref = '' THEN '" + directReferrer + "' ELSE ref END"
	}
	rows, err := db.QueryContext(ctx, with+` SELECT `+expr+`, COUNT(*), SUM(pv = 1) FROM vis
		WHERE pv > 0 AND `+col+` IS NOT NULL GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT ?`, append(args, limit)...)
	logQuery("visits_by_"+col, err)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []LabelCount
	for rows.Next() {
		var lc LabelCount
		var bounced int64
		if rows.Scan(&lc.Label, &lc.Count, &bounced) == nil {
			lc.Rate = pct(bounced, lc.Count)
			out = append(out, lc)
		}
	}
	logQuery("visits_by_"+col, rows.Err())
	return out
}

// visitorsByPlace counts visitors by the country, and the region in it, on
// their session_start, the one row the collector puts a place on.
func (s scope) visitorsByPlace(ctx context.Context, db querier) (map[string]int64, map[string]map[string]int64) {
	countries := map[string]int64{}
	regions := map[string]map[string]int64{}
	with, args := s.with()
	rows, err := db.QueryContext(ctx, with+`, u AS (
		    SELECT user_id, MAX(country) AS country, MAX(region) AS region FROM events
		    WHERE property_id = ? AND event = 'session_start' AND country IS NOT NULL GROUP BY user_id)
		SELECT u.country, COALESCE(u.region, ''), COUNT(DISTINCT ev.user_id)
		FROM ev JOIN u USING (user_id) GROUP BY 1, 2`, append(args, s.pid)...)
	logQuery("visitors_by_place", err)
	if err != nil {
		return countries, regions
	}
	defer rows.Close()
	for rows.Next() {
		var country, region string
		var n int64
		if rows.Scan(&country, &region, &n) != nil {
			continue
		}
		// A visitor counts once per country, and a region split only adds up
		// to that because each visitor has one place.
		countries[country] += n
		if region == "" {
			continue
		}
		if regions[country] == nil {
			regions[country] = map[string]int64{}
		}
		regions[country][region] += n
	}
	logQuery("visitors_by_place", rows.Err())
	return countries, regions
}

// Live is the last half hour, whatever window and filters the page is on.
type Live struct {
	Users int64
	Ticks []int64
	Pages []LabelCount
}

const liveMinutes = 30

func readLive(ctx context.Context, db *sql.DB, propertyID uuid.UUID, now time.Time) Live {
	l := Live{Ticks: make([]int64, liveMinutes)}
	since := now.Add(-liveMinutes * time.Minute).UnixMilli()
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM events WHERE property_id = ? AND created_at >= ? AND user_id IS NOT NULL`,
		propertyID[:], since).Scan(&l.Users)
	logQuery("live_users", err)

	rows, err := db.QueryContext(ctx, `SELECT (created_at - ?2) / 60000, COUNT(DISTINCT user_id) FROM events
		WHERE property_id = ?1 AND created_at >= ?2 AND user_id IS NOT NULL GROUP BY 1`, propertyID[:], since)
	logQuery("live_ticks", err)
	if err == nil {
		for rows.Next() {
			var m, n int64
			if rows.Scan(&m, &n) == nil && m >= 0 && m < liveMinutes {
				l.Ticks[m] = n
			}
		}
		rows.Close()
	}

	// Each visitor's latest page, since that's the one they're reading. A bare
	// column beside MAX takes its value from the row MAX picked, in SQLite.
	l.Pages = scanLabelCounts(ctx, db, "live_pages", `SELECT url, COUNT(*) FROM (
		    SELECT user_id, url, MAX(created_at) FROM events
		    WHERE property_id = ? AND created_at >= ? AND event = 'page_view' AND user_id IS NOT NULL
		    GROUP BY user_id)
		WHERE url IS NOT NULL GROUP BY url ORDER BY 2 DESC, 1 LIMIT 5`, propertyID[:], since)
	return l
}

// customEventNames is every custom event the property has ever recorded,
// unbounded by the window so the menu still lists a stale one.
func customEventNames(ctx context.Context, db *sql.DB, propertyID uuid.UUID) []string {
	args := []any{propertyID[:]}
	for _, b := range builtInEvents {
		args = append(args, b)
	}
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT event FROM events
		WHERE property_id = ? AND event NOT IN (`+placeholders(len(builtInEvents))+`) ORDER BY event`, args...)
	logQuery("custom_event_names", err)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			names = append(names, n)
		}
	}
	return names
}
