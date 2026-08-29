package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PageData is everything base.html and one page template need. One struct for
// every page rather than a type each: the shared chrome is most of it, and the
// page-specific tail is nil for whichever page is not using it.
type PageData struct {
	Title         string
	Description   string
	Path          string
	Canonical     string
	Staging       bool
	Authenticated bool
	Year          int
	BaseURL       string
	SourceURL     string
	SiteName      string
	AuthorName    string

	OGImage string
	JSONLD  template.JS

	// The analytics collector. Empty renders nothing at all, which is what a
	// staging hostname wants: phantom sessions filed against the property the
	// production dashboard reads are worse than no tracking.
	Analytics   bool
	AnalyticsID string

	Script string
	Styles []string
	// PageScript and PageStyles are the per-page Vite entry, empty on pages
	// that only need the shared bundle.
	PageScript string
	PageStyles []string

	// Login.
	Error string
	Next  string

	// Public home page.
	Stats SiteStats

	// Overview and per-source pages.
	Dash *Dashboard

	// Search.
	Search *SearchView

	ReportedAt string
}

// Dashboard is one window of the log stream, optionally narrowed to one source.
type Dashboard struct {
	// The window, as chosen and as resolved.
	RangeKey  string
	RangeName string
	DateStart string
	DateEnd   string
	StartMS   int64
	EndMS     int64

	// Empty means every source. When set, every panel below is scoped to it
	// and the page is the source detail page.
	Source string

	Totals   Totals
	Latency  Latency
	Ingest   Stats
	Retained int

	Graph         []GraphPoint
	ByLevel       []LabelCount
	BySource      []LabelCount
	ByComponent   []LabelCount
	StatusClasses []LabelCount
	TopPaths      []LabelCount

	Sources []SourceRow
	Slowest []PathRow
	Errors  []RecordRow

	// Precomputed for the report templates, which have no charting engine and
	// no JavaScript.
	ChartPolyline   string
	ChartLabelStart string
	ChartLabelEnd   string
	ChartPeakCount  int64
	ChartPeakLabel  string
}

// rangeOption is one entry of the window picker.
type rangeOption struct {
	Key  string
	Name string
	Dur  time.Duration
}

// WithinRetention reports whether the raw records table can answer this window.
// The search page offers only these, because a longer one could never widen a
// result set: past the retention only the hourly counters survive, and those
// hold no individual lines to match.
func (o rangeOption) WithinRetention() bool { return o.Dur <= rawRetention }

// The windows offered. Anything past rawRetention still charts correctly,
// because the volume graph and every count read hourly rollups; what thins out
// beyond it is the raw detail, which the page says rather than hides.
var rangeOptions = []rangeOption{
	{"1h", "Last hour", time.Hour},
	{"24h", "Last 24 hours", 24 * time.Hour},
	{"7d", "Last 7 days", 7 * 24 * time.Hour},
	{"30d", "Last 30 days", 30 * 24 * time.Hour},
	{"90d", "Last 90 days", 90 * 24 * time.Hour},
	{"1y", "Last year", 365 * 24 * time.Hour},
}

const defaultRange = "24h"

// RangeOptions is what the picker iterates. A method rather than a package
// variable in the template's dot, so the page does not have to carry it.
func (d *Dashboard) RangeOptions() []rangeOption { return rangeOptions }

// maxWindow bounds a custom range.
//
// Without it `?start=0001-01-01&end=9999-12-31` is a span of 315 billion
// seconds, and volumeGraph fills one bucket per day across it: 3.6 million
// GraphPoints, each with a formatted label, then joined into an SVG polyline
// and marshalled into the page. That is an out-of-memory kill from one
// authenticated GET, or from a mistyped year.
//
// Five years is past anything the rollups will hold for a long while and still
// only ~1,800 daily buckets.
const maxWindow = 5 * 365 * 24 * time.Hour

// resolveWindow reads the range out of the query string.
//
// A named window is relative to now, which is what a log dashboard almost
// always wants. Explicit start and end are honoured when both are present, for
// the case a named window cannot express: going back to a specific incident.
//
// **The start is floored to the hour, and that is load bearing.** `rollups.hour`
// is an hour-floored timestamp, so a clause of `hour >= start` against a
// now-relative start drops the bucket that *contains* the start, whole. Every
// rollup-backed number on the page loses everything from the start to the top
// of the next hour, while the raw-backed panels beside them use exact `ts`
// bounds and do not, so the two disagree on screen. Measured before the fix:
// "Last hour" reported 31 records where the raw window held 59, and at one
// minute past the hour it reported one minute of data.
//
// Flooring the start makes the two exactly equal rather than merely closer: the
// first bucket is then fully inside the window (its floor is the start), and the
// last bucket holds nothing after now because nothing is logged in the future.
// The cost is that "Last hour" means "since the top of the previous hour", so
// the resolved range is displayed rather than the label being trusted.
func resolveWindow(q map[string][]string) (key, name string, startMS, endMS int64) {
	now := time.Now().UTC()

	first := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}

	if s, e := first("start"), first("end"); s != "" && e != "" {
		st, okS := parseDateToMS(s, false)
		en, okE := parseDateToMS(e, true)
		// Both are midnight and therefore already hour-aligned. The span is
		// clamped, and an end in the future is pulled back to now rather than
		// rejected, so a range typed with a lazy end date still works.
		if okS && okE && en > st {
			if en > now.UnixMilli() {
				en = now.UnixMilli()
			}
			if en > st && time.Duration(en-st)*time.Millisecond <= maxWindow {
				return "custom", "Custom", st, en
			}
		}
	}

	named := func(o rangeOption) (string, string, int64, int64) {
		start := hourFloor(now.Add(-o.Dur).UnixMilli())
		return o.Key, o.Name, start, now.UnixMilli()
	}

	key = first("range")
	for _, o := range rangeOptions {
		if o.Key == key {
			return named(o)
		}
	}
	for _, o := range rangeOptions {
		if o.Key == defaultRange {
			return named(o)
		}
	}
	return named(rangeOptions[1])
}

// parseDateToMS turns a YYYY-MM-DD into unix milliseconds, at the start of that
// day or at the start of the next one. UTC, because every timestamp stored here
// is UTC and mixing the two would shift a whole day's numbers by the offset.
func parseDateToMS(value string, endOfDay bool) (int64, bool) {
	t, err := time.ParseInLocation("2006-01-02", value, time.UTC)
	if err != nil {
		return 0, false
	}
	if endOfDay {
		t = t.AddDate(0, 0, 1)
	}
	return t.UnixMilli(), true
}

// overview is the whole system at a glance. sourceDetail is the same page
// narrowed to one site, which is why both go through here.
func (s *site) overview(w http.ResponseWriter, r *http.Request) {
	s.renderDashboard(w, r, "")
}

func (s *site) sourceDetail(w http.ResponseWriter, r *http.Request) {
	source := normalizeSource(r.PathValue("source"))
	if source == "" {
		http.Redirect(w, r, "/overview", http.StatusSeeOther)
		return
	}
	// A source nobody has ever shipped from gets a 404 rather than a fully
	// rendered dashboard of zeros, which is indistinguishable from a real
	// source that has gone quiet. That distinction is the whole point of the
	// page: "silent" and "does not exist" must not look the same.
	if !sourceExists(r.Context(), s.db, source) {
		s.notFound(w, r)
		return
	}
	s.renderDashboard(w, r, source)
}

func (s *site) renderDashboard(w http.ResponseWriter, r *http.Request, source string) {
	ctx := r.Context()
	q := r.URL.Query()

	key, name, startMS, endMS := resolveWindow(q)
	f := filter{StartMS: startMS, EndMS: endMS, Source: source}

	d := &Dashboard{
		RangeKey:  key,
		RangeName: name,
		// To the hour, not the day. The named windows floor their start to
		// the hour so the rollups can be exact, which means the label
		// ("Last hour") and the actual span are not the same thing. Showing
		// the resolved range is what keeps that honest rather than hidden.
		DateStart: time.UnixMilli(startMS).UTC().Format("2006-01-02 15:04"),
		DateEnd:   time.UnixMilli(endMS).UTC().Format("2006-01-02 15:04"),
		StartMS:   startMS,
		EndMS:     endMS,
		Source:    source,
		Retained:  int(rawRetention / (24 * time.Hour)),
		Ingest:    s.writer.Stats(),
	}

	d.Totals = totals(ctx, s.db, f)
	d.Latency = latency(ctx, s.db, f)
	d.Graph = volumeGraph(ctx, s.db, f)
	d.ByLevel = breakdown(ctx, s.db, f, "level", 6)
	d.ByComponent = breakdown(ctx, s.db, f, "component", 8)
	d.StatusClasses = statusClasses(ctx, s.db, f)
	d.TopPaths = topPaths(ctx, s.db, f, 10)
	d.Slowest = slowestPaths(ctx, s.db, f, 10)
	d.Errors = errorFeed(ctx, s.db, f, 25)

	if source == "" {
		d.BySource = breakdown(ctx, s.db, f, "source", 10)
		d.Sources = sources(ctx, s.db, f)
	}

	d.fillReportExtras()

	title := "Overview"
	description := "Every log line every site here has written, over " + strings.ToLower(name) + "."
	if source != "" {
		title = source
		description = "Logs from " + source + " over " + strings.ToLower(name) + "."
	}

	data := s.page(r, title, description)
	data.PageScript = s.dashScript
	data.PageStyles = s.dashStyles
	data.Dash = d
	data.ReportedAt = time.Now().UTC().Format("2006-01-02 15:04 MST")

	if format, ok := reportFormat(q); ok {
		s.renderReport(w, r, format, title, data)
		return
	}

	page := "overview.html"
	if source != "" {
		page = "source.html"
	}
	s.renderer.Render(w, http.StatusOK, page, data)
}

// reportFormat reads ?report. A bare "?report" means pdf, which is the form the
// dashboard's own button uses.
func reportFormat(q map[string][]string) (string, bool) {
	values, present := q["report"]
	if !present {
		return "", false
	}
	format := ""
	if len(values) > 0 {
		format = values[0]
	}
	if format == "" {
		format = "pdf"
	}
	if format != "pdf" && format != "md" {
		return "", false
	}
	return format, true
}

// fillReportExtras precomputes what the PDF and Markdown reports need: an SVG
// polyline, its axis labels and its peak. Arithmetic over data already in
// memory, so it runs on every render rather than making one handler two.
func (d *Dashboard) fillReportExtras() {
	d.ChartPolyline = chartPolyline(d.Graph)
	if len(d.Graph) == 0 {
		return
	}
	d.ChartLabelStart = d.Graph[0].Label
	d.ChartLabelEnd = d.Graph[len(d.Graph)-1].Label
	peak := d.Graph[0]
	for _, p := range d.Graph[1:] {
		if p.Count > peak.Count {
			peak = p
		}
	}
	d.ChartPeakCount = peak.Count
	d.ChartPeakLabel = peak.Label
}

// chartPolyline renders the time series as SVG polyline points, because the
// reports have no Chart.js and no browser.
//
// The geometry matches the viewBox in the report templates: change one and
// change the other.
func chartPolyline(points []GraphPoint) string {
	if len(points) == 0 {
		return ""
	}

	const (
		width   = 600.0
		height  = 100.0
		padding = 4.0
	)
	usableH := height - 2*padding

	var maxCount int64 = 1
	for _, p := range points {
		if p.Count > maxCount {
			maxCount = p.Count
		}
	}

	// A single bucket has no horizontal span to distribute across, so it is
	// drawn at the midpoint rather than dividing by zero.
	if len(points) == 1 {
		y := height - padding - float64(points[0].Count)/float64(maxCount)*usableH
		return fmt.Sprintf("%.1f,%.1f", width/2, y)
	}

	parts := make([]string, 0, len(points))
	for i, p := range points {
		x := float64(i) / float64(len(points)-1) * width
		y := height - padding - float64(p.Count)/float64(maxCount)*usableH
		parts = append(parts, fmt.Sprintf("%.1f,%.1f", x, y))
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------- search

// SearchView is the raw line search: the form's own state, the page of results,
// and enough to render pagination without a count over the whole table.
type SearchView struct {
	Q           string
	Source      string
	Level       string
	Component   string
	StatusClass int
	RangeKey    string
	RangeName   string

	Sources []string
	Levels  []string

	Results  []RecordRow
	Page     int
	PerPage  int
	HasMore  bool
	Retained int
}

const searchPerPage = 100

// RangeOptions mirrors the dashboard's picker so the two pages offer the same
// windows.
func (v *SearchView) RangeOptions() []rangeOption { return rangeOptions }

func (s *site) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	key, name, startMS, endMS := resolveWindow(q)

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	statusClass, _ := strconv.Atoi(q.Get("status"))
	level := strings.ToUpper(strings.TrimSpace(q.Get("level")))
	switch level {
	case "", "DEBUG", "INFO", "WARN", "ERROR":
	default:
		level = ""
	}

	f := filter{
		StartMS:     startMS,
		EndMS:       endMS,
		Source:      normalizeSource(q.Get("source")),
		Level:       level,
		Component:   strings.TrimSpace(q.Get("component")),
		StatusClass: statusClass,
		Q:           strings.TrimSpace(q.Get("q")),
	}

	// One row more than a page is asked for, so "is there a next page" costs
	// a row rather than a COUNT(*) over a filtered scan of everything.
	rows := recentRecords(ctx, s.db, f, searchPerPage+1, (page-1)*searchPerPage)
	hasMore := len(rows) > searchPerPage
	if hasMore {
		rows = rows[:searchPerPage]
	}

	view := &SearchView{
		Q:           f.Q,
		Source:      f.Source,
		Level:       f.Level,
		Component:   f.Component,
		StatusClass: f.StatusClass,
		RangeKey:    key,
		RangeName:   name,
		Sources:     knownSources(ctx, s.db),
		Levels:      []string{"DEBUG", "INFO", "WARN", "ERROR"},
		Results:     rows,
		Page:        page,
		PerPage:     searchPerPage,
		HasMore:     hasMore,
		Retained:    int(rawRetention / (24 * time.Hour)),
	}

	data := s.page(r, "Search", "Search every raw log line inside the retention window.")
	data.PageScript = s.dashScript
	data.PageStyles = s.dashStyles
	data.Search = view
	s.renderer.Render(w, http.StatusOK, "search.html", data)
}

// jsonBlock marshals a value for an inline <script type="application/json">.
//
// HTML escaping stays on: this string is embedded in a document, and escaping
// "<" is what stops a logged message containing "</script>" from closing the
// block and turning a log line into markup. That is not hypothetical here, where
// the data is arbitrary text written by other programs.
func jsonBlock(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Info(fmt.Sprintf("json block: %v", err))
		return template.JS("null")
	}
	return template.JS(b)
}
