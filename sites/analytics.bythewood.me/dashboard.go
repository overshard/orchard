package main

import (
	"analytics.bythewood.me/suite"

	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"analytics.bythewood.me/web"
)

const (
	dayMS = 24 * 60 * 60 * 1000
	// Long enough to show a trend, short enough that the graph buckets daily.
	defaultRangeDays = 28

	maxRangeDays = 3650
)

// PageData is everything base.html and one page template need, the fields for
// pages other than the one rendering are zero.
type PageData struct {
	Title         string
	Description   string
	Path          string
	Canonical     string
	Authenticated bool
	Year          int
	BaseURL       string
	SourceURL     string
	SiteName      string
	AuthorName    string

	OGImage string
	JSONLD  template.JS

	Script string
	Styles []string
	// The per-page Vite entry, empty on pages that only need the shared bundle.
	PageScript string
	PageStyles []string

	// Empty CollectorID renders no self-tracking snippet.
	CollectorID     string
	CollectorServer string

	Error string
	Next  string

	Properties []PropertyRow
	Totals     PropertyTotals
	Query      string

	Property   *DashProperty
	Dash       *Dashboard
	ReportedAt string

	Suite  suite.Page
	Footer suite.Footer
}

// DashProperty is the property identity the dashboard chrome renders.
type DashProperty struct {
	ID          string
	Name        string
	IsProtected bool
	IsPublic    bool
}

// Dashboard is one property's numbers over one date range.
type Dashboard struct {
	DateStart string
	DateEnd   string
	DateRange int64
	LiveUsers int64
	Authed    bool

	Filters []Filter
	query   url.Values

	// EventCards is every tile, for the reports. Tiles and Pinned split it
	// for the page.
	EventCards   []EventCard
	Tiles        []EventCard
	Pinned       []EventCard
	Labels       []string
	CustomEvents []CustomEventDescriptor
	GoalNames    []string

	Graph      []GraphPoint
	GraphStep  string
	GraphPeak  GraphPoint
	GraphAvg   int64
	PrevAvg    int64
	Chart      template.HTML
	ChartMarks []ChartMark
	Alerts     []Alert
	Live       Live

	ByScreenSize          []LabelCount
	ByDevice              []LabelCount
	ByBrowser             []LabelCount
	ByPlatform            []LabelCount
	ByPageURL             []LabelCount
	PageViewsByPageURL    []LabelCount
	EntryPages            []LabelCount
	ExitPages             []LabelCount
	ByCustomEvent         []LabelCount
	Goals                 []LabelCount
	SessionsByReferrer    []LabelCount
	PageViewsByUTMMedium  []LabelCount
	PageViewsByUTMSource  []LabelCount
	PageViewsByUTMCampaig []LabelCount
	Regions               []LabelCount
	RegionsOf             string

	SessionsByCountry       map[string]int64
	SessionsByCountryRegion map[string]map[string]int64

	Bots BotTraffic
	Real RealUsers

	// Precomputed for the report templates, which have no charting engine and
	// no JavaScript to compute this themselves.
	ChartPolyline   string
	ChartLabelStart string
	ChartLabelEnd   string
	ChartPeakCount  int64
	ChartPeakLabel  string
	BreakdownTotals BreakdownTotals
	TopCountries    []LabelCount
}

// FilterText is the filters as one line, for the reports.
func (d *Dashboard) FilterText() string {
	parts := make([]string, 0, len(d.Filters))
	for _, f := range d.Filters {
		parts = append(parts, f.Label+" "+f.Value)
	}
	return strings.Join(parts, ", ")
}

// Hidden is the filters as form fields, so changing the dates keeps them.
func (d *Dashboard) Hidden() map[string]string {
	out := map[string]string{}
	for _, f := range d.Filters {
		out["f_"+f.Key] = f.Value
	}
	return out
}

// BreakdownTotals holds each breakdown's sum, floored at one so a template can
// divide by it unguarded.
type BreakdownTotals struct {
	Device     int64
	Browser    int64
	Platform   int64
	ScreenSize int64
}

// dashboard renders one property. Public properties are readable without a
// session, everything else, including a missing property, redirects.
func (s *site) dashboard(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	ctx := r.Context()

	p, err := lookupProperty(ctx, s.db, id)
	if err != nil {
		slog.Info("dashboard lookup", slog.Any("err", err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	if p == nil {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}

	authed := s.auth.Authenticated(r)
	if !p.IsPublic && !authed {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
		return
	}

	q := r.URL.Query()
	today := time.Now().In(eastern)

	dateStart := q.Get("date_start")
	if dateStart == "" {
		days := defaultRangeDays
		if n, err := strconv.Atoi(q.Get("date_range")); err == nil && n >= 1 && n <= maxRangeDays {
			days = n
		}
		dateStart = today.AddDate(0, 0, -days).Format("2006-01-02")
	}
	dateEnd := q.Get("date_end")
	if dateEnd == "" {
		dateEnd = today.Format("2006-01-02")
	}

	startMS, ok := parseDateToMS(dateStart, false)
	if !ok {
		http.Error(w, "bad date_start", http.StatusBadRequest)
		return
	}
	endMS, ok := parseDateToMS(dateEnd, true)
	if !ok {
		http.Error(w, "bad date_end", http.StatusBadRequest)
		return
	}

	// "custom" or absent means derive the range from the two dates.
	var rangeDays int64
	switch v := q.Get("date_range"); v {
	case "", "custom":
		// Counted in calendar days, since a window across a DST change is an
		// hour longer or shorter than its days.
		a, _ := time.Parse("2006-01-02", dateStart)
		b, _ := time.Parse("2006-01-02", dateEnd)
		rangeDays = max64(int64(b.Sub(a).Hours()/24), 1)
	default:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			n = defaultRangeDays
		}
		rangeDays = n
	}

	// Clamped outside the switch, so both arms are bounded. rangeDays sizes an
	// allocating bucket slice that is then inlined into the page, and a public
	// property makes this reachable with no cookie.
	rangeDays = min64(max64(rangeDays, 1), maxRangeDays)

	// The window too, so an ancient start date cannot widen the graph behind
	// the clamp above.
	if startMS < endMS-maxRangeDays*dayMS {
		startMS = endMS - maxRangeDays*dayMS
	}

	// The period before is the same span of time ending where this one starts.
	prevEndMS := startMS - 1
	prevStartMS := prevEndMS - (endMS - startMS)

	// Anchor the graph to the requested end date, stepping back from today
	// charts a historical range as zeros beside real metric cards.
	graphEnd, err := time.ParseInLocation("2006-01-02", dateEnd, eastern)
	if err != nil {
		graphEnd = today
	}

	filters := parseFilters(q, p.ID)
	d := &Dashboard{
		DateStart: dateStart,
		DateEnd:   dateEnd,
		DateRange: rangeDays,
		Authed:    authed,
		Filters:   filters,
		query:     q,
		Live:      readLive(ctx, s.db, p.ID, time.Now()),
	}
	d.LiveUsers = d.Live.Users
	cur := newScope(p.ID, startMS, endMS, filters)
	prev := newScope(p.ID, prevStartMS, prevEndMS, filters)
	// The window spans rangeDays+1 calendar days, so the period before ends
	// that many days earlier.
	curB := newBuckets(graphEnd, rangeDays)
	prevB := newBuckets(graphEnd.AddDate(0, 0, -int(rangeDays+1)), rangeDays)
	d.Labels = curB.labels
	d.GraphStep = graphStep(rangeDays)

	// Every panel reads the same cut rows, so they're cut once on one
	// connection, whose temp tables are dropped before it goes back.
	var db querier = s.db
	if conn, err := s.db.Conn(ctx); err == nil {
		defer conn.Close()
		db = conn
		for name, sc := range map[string]*scope{"cur": &cur, "prev": &prev} {
			if err := sc.materialize(ctx, conn, name); err != nil {
				slog.Info("materialize", slog.String("scope", name), slog.Any("err", err))
			}
			defer sc.drop(context.WithoutCancel(ctx), conn)
		}
	}

	d.GoalNames = goalEvents(ctx, s.db, p.ID)
	s.fillTiles(ctx, db, d, p, cur, prev, curB, prevB)

	var alerts []Alert
	if authed {
		alerts = readAlerts(ctx, s.db, p.ID, startMS, endMS, 50)
		d.Alerts = readAlerts(ctx, s.db, p.ID, 0, time.Now().UnixMilli(), 5)
	}
	d.Chart, d.ChartMarks = visitorsChart(d.Graph, alerts, curB)

	d.PageViewsByPageURL = cur.pageViewsBy(ctx, db, "url", 10)
	d.ByPageURL = cur.top(ctx, db, "events_by_url", "url", "COUNT(*)", "", 10)
	d.EntryPages = cur.visitsBy(ctx, db, "entry", 8)
	d.ExitPages = cur.visitsBy(ctx, db, "exit", 8)
	d.SessionsByReferrer = cur.visitsBy(ctx, db, "ref", 10)
	d.ByScreenSize = cur.visitorsBy(ctx, db, screenExpr, 7)
	d.ByDevice = cur.visitorsBy(ctx, db, "device", 7)
	d.ByBrowser = cur.visitorsBy(ctx, db, "browser", 7)
	d.ByPlatform = cur.visitorsBy(ctx, db, "platform", 7)
	d.ByCustomEvent = cur.customEvents(ctx, db, 10)
	d.PageViewsByUTMMedium = cur.pageViewsBy(ctx, db, "utm_medium", 10)
	d.PageViewsByUTMSource = cur.pageViewsBy(ctx, db, "utm_source", 10)
	d.PageViewsByUTMCampaig = cur.pageViewsBy(ctx, db, "utm_campaign", 10)
	d.SessionsByCountry, d.SessionsByCountryRegion = cur.visitorsByPlace(ctx, db)
	for _, f := range filters {
		if f.Key == "country" {
			d.RegionsOf = f.Value
			d.Regions = topOf(d.SessionsByCountryRegion[f.Value], 8)
		}
	}
	d.Bots = botTraffic(ctx, s.db, p.ID, startMS, endMS, 10)
	// Error text comes from the site's own scripts, which a public dashboard
	// has no reason to show strangers.
	d.Real = readRealUsers(ctx, s.db, p.ID, startMS, endMS, authed)

	d.fillReportExtras()

	data := s.page(r, p.Name, "Analytics for "+p.Name)
	data.PageScript = s.propsScript
	data.PageStyles = s.propsStyles
	data.Property = &DashProperty{
		ID:          p.ID.String(),
		Name:        p.Name,
		IsProtected: p.IsProtected,
		IsPublic:    p.IsPublic,
	}
	data.Dash = d
	data.ReportedAt = time.Now().In(eastern).Format("2006-01-02 15:04")

	// Report export is operator-only even for a public property since the PDF
	// path spawns an unthrottled Typst compile.
	if report, ok := reportFormat(q); ok {
		if !authed {
			http.Redirect(w, r, "/"+id.String(), http.StatusSeeOther)
			return
		}
		s.renderReport(w, r, report, p.Name, data)
		return
	}

	s.renderer.Render(w, http.StatusOK, "property.html", data)
}

// reportFormat reads ?report, a bare "?report" means pdf.
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

// fillReportExtras precomputes what the PDF and Markdown reports need. It runs
// on every dashboard render, being arithmetic over data already in memory.
func (d *Dashboard) fillReportExtras() {
	d.ChartPolyline = chartPolyline(d.Graph)

	if len(d.Graph) > 0 {
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

	d.BreakdownTotals = BreakdownTotals{
		Device:     sumCounts(d.ByDevice),
		Browser:    sumCounts(d.ByBrowser),
		Platform:   sumCounts(d.ByPlatform),
		ScreenSize: sumCounts(d.ByScreenSize),
	}

	// The report renders a table, which needs an order a Go map does not have.
	countries := make([]LabelCount, 0, len(d.SessionsByCountry))
	for code, count := range d.SessionsByCountry {
		countries = append(countries, LabelCount{Label: code, Count: count})
	}
	sort.Slice(countries, func(i, j int) bool {
		if countries[i].Count != countries[j].Count {
			return countries[i].Count > countries[j].Count
		}
		// Ties break by name so the report is reproducible.
		return countries[i].Label < countries[j].Label
	})
	if len(countries) > 10 {
		countries = countries[:10]
	}
	d.TopCountries = countries
}

// sumCounts is a breakdown's denominator, floored at one so a template can
// divide by it unguarded.
func sumCounts(items []LabelCount) int64 {
	var total int64
	for _, i := range items {
		total += i.Count
	}
	return max64(total, 1)
}

// chartPolyline renders the time series as SVG polyline points, for reports that
// have no browser. The geometry matches the viewBox in the report templates.
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

	// A single bucket has no horizontal span to divide by.
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

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// jsonBlock marshals a value for an inline <script type="application/json">.
// HTML escaping stays on so a value containing "</script>" cannot close the
// block, html/template cannot work that out inside a non-JavaScript script type.
func jsonBlock(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Info("json block", slog.Any("err", err))
		return template.JS("null")
	}
	return template.JS(b)
}
