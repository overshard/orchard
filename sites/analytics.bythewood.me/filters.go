package main

import (
	"context"
	"database/sql"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// A visit is a run of one visitor's events with no gap of half an hour or
// more, which is the line most analytics tools draw.
const visitGapMS = 30 * 60 * 1000

type filterKind int

const (
	// A column on every event row.
	rowFilter filterKind = iota
	// A fact about the visitor kept on only some of their rows, like the
	// country on their session_start.
	userFilter
	// A fact about the visit, read off its first page view or any event in it.
	visitFilter
)

// filterField is one thing a dashboard can be narrowed by. The request only
// picks which of these runs, so no part of it is ever written into SQL.
type filterField struct {
	Key   string
	Label string
	kind  filterKind
	cond  func(f Filter) (string, []any)
}

// Filter is one active filter, a field and the value a row was clicked for.
type Filter struct {
	Key   string
	Label string
	Value string
	kind  filterKind
	cond  func(f Filter) (string, []any)
	pid   []byte
}

// entryOf is a visit's first page view's value for col, found by sorting on
// the time padded to a fixed width with the value glued on behind it.
func entryOf(col string) string {
	return `substr(MIN(CASE WHEN event = 'page_view' THEN printf('%015d', created_at) || COALESCE(` + col + `, '') END), 16)`
}

func exitOf(col string) string {
	return `substr(MAX(CASE WHEN event = 'page_view' THEN printf('%015d', created_at) || COALESCE(` + col + `, '') END), 16)`
}

func column(col string) func(Filter) (string, []any) {
	return func(f Filter) (string, []any) { return col + " = ?", []any{f.Value} }
}

func entryColumn(col string) func(Filter) (string, []any) {
	return func(f Filter) (string, []any) { return entryOf(col) + " = ?", []any{f.Value} }
}

// Direct traffic has no referrer, and "direct" can't be a hostname since it
// has no dot.
const directReferrer = "direct"

var filterFields = []filterField{
	{Key: "page", Label: "page", kind: rowFilter, cond: column("url")},
	{Key: "entry", Label: "entry page", kind: visitFilter, cond: entryColumn("url")},
	{Key: "referrer", Label: "referrer", kind: visitFilter, cond: func(f Filter) (string, []any) {
		v := f.Value
		if v == directReferrer {
			v = ""
		}
		return entryOf("referrer") + " = ?", []any{v}
	}},
	{Key: "source", Label: "utm source", kind: visitFilter, cond: entryColumn("utm_source")},
	{Key: "medium", Label: "utm medium", kind: visitFilter, cond: entryColumn("utm_medium")},
	{Key: "campaign", Label: "utm campaign", kind: visitFilter, cond: entryColumn("utm_campaign")},
	{Key: "goal", Label: "goal", kind: visitFilter, cond: func(f Filter) (string, []any) {
		return "SUM(event = ?) > 0", []any{f.Value}
	}},
	{Key: "country", Label: "country", kind: userFilter, cond: func(f Filter) (string, []any) {
		return `user_id IN (SELECT user_id FROM events WHERE property_id = ? AND event = 'session_start' AND country = ?)`, []any{f.pid, f.Value}
	}},
	{Key: "region", Label: "region", kind: userFilter, cond: func(f Filter) (string, []any) {
		return `user_id IN (SELECT user_id FROM events WHERE property_id = ? AND event = 'session_start' AND region = ?)`, []any{f.pid, f.Value}
	}},
	{Key: "device", Label: "device", kind: rowFilter, cond: column("device")},
	{Key: "browser", Label: "browser", kind: rowFilter, cond: column("browser")},
	{Key: "platform", Label: "platform", kind: rowFilter, cond: column("platform")},
	// Only page views carry the screen, so it's the visitor's and not the row's.
	{Key: "screen", Label: "screen", kind: userFilter, cond: func(f Filter) (string, []any) {
		w, h, _ := strings.Cut(f.Value, "x")
		wi, _ := strconv.Atoi(w)
		hi, _ := strconv.Atoi(h)
		return `user_id IN (SELECT user_id FROM events WHERE property_id = ? AND event = 'page_view' AND screen_width = ? AND screen_height = ?)`, []any{f.pid, wi, hi}
	}},
}

// maxFilterLen keeps a pasted URL from binding a megabyte into every query.
const maxFilterLen = 512

// parseFilters reads f_<key> from a query string in the fixed field order, so
// the same filters always build the same SQL. filter_url is the old name for
// the page filter, and old links still use it.
func parseFilters(q url.Values, pid uuid.UUID) []Filter {
	var out []Filter
	for _, ff := range filterFields {
		v := q.Get("f_" + ff.Key)
		if v == "" && ff.Key == "page" {
			v = q.Get("filter_url")
		}
		if v == "" || len(v) > maxFilterLen {
			continue
		}
		out = append(out, Filter{Key: ff.Key, Label: ff.Label, Value: v, kind: ff.kind, cond: ff.cond, pid: pid[:]})
	}
	return out
}

// scope is one property's events over one window with the filters applied.
// Once materialized, its rows and visits sit in temp tables named by table.
type scope struct {
	pid        []byte
	start, end int64
	filters    []Filter
	table      string
}

// querier is a *sql.DB or the one *sql.Conn a dashboard's temp tables live on.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// materialize cuts the scope's rows and visits once into temp tables, so each
// panel reads them instead of running the window functions again. On failure
// the scope keeps building its CTEs.
func (s *scope) materialize(ctx context.Context, q querier, name string) error {
	with, args := s.with()
	ev, vs, vis := "temp."+name+"_ev", "temp."+name+"_vs", "temp."+name+"_vis"
	stmts := []struct {
		sql  string
		args []any
	}{
		{"DROP TABLE IF EXISTS " + ev, nil},
		{"DROP TABLE IF EXISTS " + vs, nil},
		{"DROP TABLE IF EXISTS " + vis, nil},
		{"CREATE TABLE " + ev + " AS " + with + " SELECT " + eventColumns + " FROM ev", args},
		{"CREATE INDEX " + ev + "_user ON " + name + "_ev (user_id, created_at, id)", nil},
		{"CREATE TABLE " + vs + " AS " + visitNumbersOf(visitColumns, ev), nil},
		{"CREATE TABLE " + vis + " AS " + visitsOf(vs), nil},
	}
	for _, st := range stmts {
		if _, err := q.ExecContext(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	s.table = name
	return nil
}

// drop removes the temp tables, since the connection goes back to the pool.
func (s *scope) drop(ctx context.Context, q querier) {
	if s.table == "" {
		return
	}
	for _, suffix := range []string{"_ev", "_vs", "_vis"} {
		_, _ = q.ExecContext(ctx, "DROP TABLE IF EXISTS temp."+s.table+suffix)
	}
	s.table = ""
}

func newScope(pid uuid.UUID, start, end int64, filters []Filter) scope {
	return scope{pid: pid[:], start: start, end: end, filters: filters}
}

func (s scope) hasVisitFilter() bool {
	for _, f := range s.filters {
		if f.kind == visitFilter {
			return true
		}
	}
	return false
}

// visitNumbers adds vn, which numbers each visitor's visits, to the rows of
// from. A row starts a visit when the visitor's row before it is half an hour
// or more older.
func visitNumbers(from string) string { return visitNumbersOf("*", from) }

// visitColumns is all a visit is read for, and keeping the window functions to
// them keeps their sorts small.
const visitColumns = "id, user_id, created_at, event, url, referrer"

func visitNumbersOf(cols, from string) string {
	return `SELECT ` + cols + `, SUM(brk) OVER (PARTITION BY user_id ORDER BY created_at, id ROWS UNBOUNDED PRECEDING) AS vn
	  FROM (SELECT ` + cols + `, CASE WHEN created_at - LAG(created_at) OVER (PARTITION BY user_id ORDER BY created_at, id) < ` +
		strconv.Itoa(visitGapMS) + ` THEN 0 ELSE 1 END AS brk FROM ` + from + ` WHERE user_id IS NOT NULL)`
}

// eventColumns is what ev carries when it comes out of g, named so the visit
// numbering withVisits adds doesn't collide with g's own.
const eventColumns = `id, property_id, event, created_at, user_id, url, title, referrer, platform, browser,
	device, screen_width, screen_height, country, region, city, utm_source, utm_medium, utm_campaign,
	utm_term, utm_content, time_on_page_ms`

// with is a WITH clause that names the scoped events ev. The window is read
// off the (property_id, created_at) index before anything else happens.
func (s scope) with() (string, []any) {
	if s.table != "" {
		return "WITH ev AS (SELECT * FROM temp." + s.table + "_ev)", nil
	}
	args := []any{s.pid, s.start, s.end}
	var rowConds []string
	for _, f := range s.filters {
		if f.kind == visitFilter {
			continue
		}
		c, a := f.cond(f)
		rowConds = append(rowConds, c)
		args = append(args, a...)
	}
	where := ""
	if len(rowConds) > 0 {
		where = " AND " + strings.Join(rowConds, " AND ")
	}
	window := `SELECT * FROM events WHERE property_id = ? AND created_at >= ? AND created_at <= ?`

	if !s.hasVisitFilter() {
		return "WITH ev AS (" + window + where + ")", args
	}

	// Visits are cut from every row in the window before the row filters run,
	// so a page filter keeps the visits that entered somewhere else.
	args = []any{s.pid, s.start, s.end}
	var having []string
	for _, f := range s.filters {
		if f.kind != visitFilter {
			continue
		}
		c, a := f.cond(f)
		having = append(having, c)
		args = append(args, a...)
	}
	for _, f := range s.filters {
		if f.kind == visitFilter {
			continue
		}
		_, a := f.cond(f)
		args = append(args, a...)
	}
	sql := "WITH w AS (" + window + "), g AS (" + visitNumbers("w") + "), " +
		"keep AS (SELECT user_id, vn FROM g GROUP BY user_id, vn HAVING " + strings.Join(having, " AND ") + "), " +
		"ev AS (SELECT " + eventColumns + " FROM g JOIN keep USING (user_id, vn) WHERE 1 = 1" + where + ")"
	return sql, args
}

// withVisits adds vis, one row per visit in ev with its first and last page,
// referrer and page view count.
func (s scope) withVisits() (string, []any) {
	if s.table != "" {
		t := "temp." + s.table
		return "WITH ev AS (SELECT * FROM " + t + "_ev), vs AS (SELECT * FROM " + t + "_vs), vis AS (SELECT * FROM " + t + "_vis)", nil
	}
	sql, args := s.with()
	return sql + ", vs AS (" + visitNumbers("ev") + "), vis AS (" + visitsOf("vs") + ")", args
}

func visitsOf(vs string) string {
	return "SELECT user_id, vn, MIN(created_at) AS start, SUM(event = 'page_view') AS pv, " +
		entryOf("url") + " AS entry, " + exitOf("url") + " AS exit, " + entryOf("referrer") + " AS ref " +
		"FROM " + vs + " GROUP BY user_id, vn"
}

// Toggle is the dashboard's query string with one filter flipped, so a row
// links to itself narrowed and a lit row or a chip links back out.
func (d *Dashboard) Toggle(key, value string) string {
	q := url.Values{}
	for k, v := range d.query {
		q[k] = v
	}
	q.Del("report")
	if key == "page" {
		q.Del("filter_url")
	}
	if key == "country" {
		// A region belongs to the country it was picked under.
		q.Del("f_region")
	}
	if q.Get("f_"+key) == value {
		q.Del("f_" + key)
	} else {
		q.Set("f_"+key, value)
	}
	return "?" + q.Encode()
}

// On says whether a row is the value its field is filtered to.
func (d *Dashboard) On(key, value string) bool {
	for _, f := range d.Filters {
		if f.Key == key && f.Value == value {
			return true
		}
	}
	return false
}

// Cleared is the query string with every filter dropped and the dates kept.
func (d *Dashboard) Cleared() string {
	q := url.Values{}
	for k, v := range d.query {
		if !strings.HasPrefix(k, "f_") && k != "filter_url" && k != "report" {
			q[k] = v
		}
	}
	if len(q) == 0 {
		return "?"
	}
	return "?" + q.Encode()
}

// Report is the link to a report with the same dates and filters.
func (d *Dashboard) Report(format string) string {
	q := url.Values{}
	for k, v := range d.query {
		q[k] = v
	}
	q.Set("report", format)
	return "?" + q.Encode()
}

// QueryString is the page's own query, for a form that has to come back to it.
func (d *Dashboard) QueryString() string {
	q := url.Values{}
	for k, v := range d.query {
		if k != "report" {
			q[k] = v
		}
	}
	return q.Encode()
}
