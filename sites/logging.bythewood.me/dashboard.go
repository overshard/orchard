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

// PageData is everything base.html and one page template need; the page
// specific fields are nil on whichever page is not using them.
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

	// Off on staging, so phantom sessions are not filed against the real
	// analytics property.
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

	// Empty means every source; set scopes every panel below to it.
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

	// Precomputed for the report templates, which have no charting engine.
	ChartPolyline   string
	ChartLabelStart string
	ChartLabelEnd   string
	ChartPeakCount  int64
	ChartPeakLabel  string
}

type rangeOption struct {
	Key  string
	Name string
	Dur  time.Duration
}

// WithinRetention reports whether the raw records table can answer this window.
// The search page offers only these: past retention there are no lines to match.
func (o rangeOption) WithinRetention() bool { return o.Dur <= rawRetention }

// Anything past rawRetention still charts correctly from the hourly rollups;
// what thins out beyond it is the raw detail.
var rangeOptions = []rangeOption{
	{"1h", "Last hour", time.Hour},
	{"24h", "Last 24 hours", 24 * time.Hour},
	{"7d", "Last 7 days", 7 * 24 * time.Hour},
	{"30d", "Last 30 days", 30 * 24 * time.Hour},
	{"90d", "Last 90 days", 90 * 24 * time.Hour},
	{"1y", "Last year", 365 * 24 * time.Hour},
}

const defaultRange = "24h"

// RangeOptions is what the picker iterates, as a method so the page data does
// not have to carry it.
func (d *Dashboard) RangeOptions() []rangeOption { return rangeOptions }

// maxWindow bounds a custom range: volumeGraph fills one bucket per day across
// whatever it is given, so a mistyped year is an out-of-memory kill.
const maxWindow = 5 * 365 * 24 * time.Hour

// resolveWindow reads the range out of the query string. The start is floored to
// the hour so rollup-backed and raw-backed panels cover the same span; the cost
// is that "Last hour" means since the top of the previous hour.
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
		// Both are midnight and so already hour-aligned. An end in the future
		// is pulled back to now rather than rejected.
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

// parseDateToMS turns a YYYY-MM-DD into unix milliseconds. UTC, because every
// stored timestamp is UTC and mixing the two shifts a day's numbers.
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

func (s *site) overview(w http.ResponseWriter, r *http.Request) {
	s.renderDashboard(w, r, "")
}

func (s *site) sourceDetail(w http.ResponseWriter, r *http.Request) {
	source := normalizeSource(r.PathValue("source"))
	if source == "" {
		http.Redirect(w, r, "/overview", http.StatusSeeOther)
		return
	}
	// 404 rather than a dashboard of zeros: "silent" and "does not exist" must
	// not look the same.
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
		// To the hour, since the label and the resolved span differ once the
		// start is floored.
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

// reportFormat reads ?report; a bare "?report" means pdf.
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

// fillReportExtras precomputes the polyline, labels and peak the reports need.
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

// chartPolyline renders the series as SVG polyline points for the reports,
// which have no browser. The geometry must match the viewBox in report.typ.
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

	// A single bucket has no span to distribute across.
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

// SearchView is the form state, one page of results, and enough to paginate
// without a count over the whole table.
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

// RangeOptions mirrors the dashboard's picker.
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

	// One row more than a page, so "is there a next page" costs a row rather
	// than a COUNT(*) over a filtered scan.
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
// HTML escaping stays on, so a logged "</script>" cannot close the block.
func jsonBlock(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Info(fmt.Sprintf("json block: %v", err))
		return template.JS("null")
	}
	return template.JS(b)
}
