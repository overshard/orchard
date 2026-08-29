package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	dayMS = 24 * 60 * 60 * 1000
	// The look-back when the request names no range. Four weeks: long enough
	// to show a trend on a site with a few visitors a day, short enough that
	// the graph still buckets daily.
	defaultRangeDays = 28
)

// PageData is everything base.html and one page template need.
//
// One struct for every page rather than a type each, following the blog port.
// The shared chrome is most of it, and the page-specific tail is nil for
// whichever page is not using it, which is what lets one base layout cover
// seven pages.
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

	// The social card, and the page's JSON-LD graph already marshalled.
	OGImage string
	JSONLD  template.JS

	Script string
	Styles []string
	// PageScript and PageStyles are the per-page Vite entry, empty on pages
	// that only need the shared bundle.
	PageScript string
	PageStyles []string

	// The self-tracking snippet. Empty CollectorID renders nothing.
	CollectorID     string
	CollectorServer string

	// Login.
	Error string
	Next  string

	// Marketing home.
	TotalProperties int64
	TotalEvents     int64
	TotalUsers      int
	FirstEventAt    string

	// Property list.
	Properties []PropertyRow
	Totals     PropertyTotals
	Query      string

	// Dashboard.
	Property   *DashProperty
	Dash       *Dashboard
	ReportedAt string
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
	FilterURL string
	LiveUsers int64

	EventCards   []EventCard
	CustomEvents []CustomEventDescriptor

	Graph                 []GraphPoint
	ByScreenSize          []LabelCount
	ByDevice              []LabelCount
	ByBrowser             []LabelCount
	ByPlatform            []LabelCount
	ByPageURL             []LabelCount
	PageViewsByPageURL    []LabelCount
	ByCustomEvent         []LabelCount
	SessionsByReferrer    []LabelCount
	PageViewsByUTMMedium  []LabelCount
	PageViewsByUTMSource  []LabelCount
	PageViewsByUTMCampaig []LabelCount

	SessionsByCountry       map[string]int64
	SessionsByCountryRegion map[string]map[string]int64

	Bots BotTraffic

	// Precomputed for the report templates, which have no charting engine and
	// no JavaScript: an SVG polyline, the axis labels, the peak, and the
	// denominators each breakdown's percentages divide by.
	ChartPolyline   string
	ChartLabelStart string
	ChartLabelEnd   string
	ChartPeakCount  int64
	ChartPeakLabel  string
	BreakdownTotals BreakdownTotals
	TopCountries    []LabelCount
}

// BreakdownTotals holds each breakdown's sum, floored at one so a report
// template can divide by it without guarding every row.
type BreakdownTotals struct {
	Device     int64
	Browser    int64
	Platform   int64
	ScreenSize int64
}

// dashboard renders one property.
//
// Public properties are readable without a session; everything else redirects
// to login. A property that does not exist redirects to /properties rather
// than 404ing, which is the behaviour a stale bookmark wants.
func (s *site) dashboard(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	ctx := r.Context()

	p, err := lookupProperty(ctx, s.db, id)
	if err != nil {
		slog.Info(fmt.Sprintf("dashboard lookup: %v", err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	if p == nil {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}

	authed := isAuthenticated(r, s.cookieKey)
	if !p.IsPublic && !authed {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	q := r.URL.Query()
	today := time.Now()

	dateStart := q.Get("date_start")
	if dateStart == "" {
		dateStart = today.AddDate(0, 0, -defaultRangeDays).Format("2006-01-02")
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

	// The range in days drives both the comparison window and the graph's
	// bucket width. "custom" (or absent) means derive it from the two dates.
	var rangeDays int64
	switch v := q.Get("date_range"); v {
	case "", "custom":
		rangeDays = max64((endMS-startMS)/dayMS, 1)
	default:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			n = defaultRangeDays
		}
		rangeDays = n
	}

	prevStartMS := startMS - rangeDays*dayMS
	prevEndMS := endMS - rangeDays*dayMS
	filterURL := q.Get("filter_url")

	// Anchor the graph to the requested end date, not to today. Stepping back
	// from today charts any historical range as a row of zeros next to metric
	// cards showing real numbers.
	graphEnd, err := time.ParseInLocation("2006-01-02", dateEnd, time.Local)
	if err != nil {
		graphEnd = today
	}

	d := &Dashboard{
		DateStart: dateStart,
		DateEnd:   dateEnd,
		DateRange: rangeDays,
		FilterURL: filterURL,
		LiveUsers: totalLiveUsers(ctx, s.db, p.ID),
	}

	d.EventCards = standardEventCards(ctx, s.db, p.ID, startMS, endMS, prevStartMS, prevEndMS, filterURL)
	customCards, customEvents := customEventCards(ctx, s.db, p.ID, p.CustomCards, startMS, endMS, prevStartMS, prevEndMS, filterURL)
	d.EventCards = append(d.EventCards, customCards...)
	d.CustomEvents = customEvents

	d.Graph = eventsGraph(ctx, s.db, p.ID, startMS, endMS, filterURL, graphEnd, rangeDays)
	d.ByScreenSize = eventsByScreenSize(ctx, s.db, p.ID, startMS, endMS, filterURL, 7)
	d.ByDevice = eventsByDevice(ctx, s.db, p.ID, startMS, endMS, filterURL, 7)
	d.ByBrowser = eventsByBrowser(ctx, s.db, p.ID, startMS, endMS, filterURL, 7)
	d.ByPlatform = eventsByPlatform(ctx, s.db, p.ID, startMS, endMS, filterURL, 7)
	d.ByPageURL = eventsByPageURL(ctx, s.db, p.ID, startMS, endMS, filterURL, 10)
	d.PageViewsByPageURL = pageViewsByPageURL(ctx, s.db, p.ID, startMS, endMS, filterURL, 10)
	d.ByCustomEvent = eventsByCustomEvent(ctx, s.db, p.ID, startMS, endMS, filterURL, 10)
	d.SessionsByReferrer = sessionStartsByReferrer(ctx, s.db, p.ID, startMS, endMS, filterURL, 10)
	d.PageViewsByUTMMedium = pageViewsByUTM(ctx, s.db, p.ID, startMS, endMS, filterURL, "medium", 10)
	d.PageViewsByUTMSource = pageViewsByUTM(ctx, s.db, p.ID, startMS, endMS, filterURL, "source", 10)
	d.PageViewsByUTMCampaig = pageViewsByUTM(ctx, s.db, p.ID, startMS, endMS, filterURL, "campaign", 10)
	d.SessionsByCountry = sessionStartsByCountry(ctx, s.db, p.ID, startMS, endMS, filterURL)
	d.SessionsByCountryRegion = sessionStartsByCountryRegion(ctx, s.db, p.ID, startMS, endMS, filterURL)
	d.Bots = botTraffic(ctx, s.db, p.ID, startMS, endMS, 10)

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
	data.ReportedAt = time.Now().Format("2006-01-02 15:04")

	if report, ok := reportFormat(q); ok {
		s.renderReport(w, r, report, p.Name, data)
		return
	}

	s.renderer.Render(w, http.StatusOK, "property.html", data)
}

// reportFormat reads ?report. A bare "?report" means pdf, which is the form
// the dashboard's own button uses.
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

// fillReportExtras precomputes what the PDF and Markdown reports need.
//
// It runs for every dashboard render, not only report ones. The work is
// arithmetic over data already in memory, and making it conditional would mean
// two paths through the same handler for no measurable saving.
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

	// The map data is a map, so it has no order. The report has no map, only a
	// table, so it needs one.
	countries := make([]LabelCount, 0, len(d.SessionsByCountry))
	for code, count := range d.SessionsByCountry {
		countries = append(countries, LabelCount{Label: code, Count: count})
	}
	sort.Slice(countries, func(i, j int) bool {
		if countries[i].Count != countries[j].Count {
			return countries[i].Count > countries[j].Count
		}
		// Ties broken by name so the report is reproducible run to run.
		return countries[i].Label < countries[j].Label
	})
	if len(countries) > 10 {
		countries = countries[:10]
	}
	d.TopCountries = countries
}

// sumCounts is a breakdown's denominator, floored at one so a template can
// divide by it unguarded. A zero total means every numerator is zero too, so
// the floor changes no displayed percentage.
func sumCounts(items []LabelCount) int64 {
	var total int64
	for _, i := range items {
		total += i.Count
	}
	return max64(total, 1)
}

// chartPolyline renders the time series as SVG polyline points.
//
// The geometry matches the viewBox in the report templates: change one and
// change the other. It exists because the reports have no Chart.js and no
// browser, so the only way a line gets drawn is by computing it here.
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

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// jsonBlock marshals a value for an inline <script type="application/json">.
//
// HTML escaping stays ON here, which is the opposite of what encodeExtra wants
// and for the opposite reason: this string is about to be embedded in a
// document, and escaping < to < is what makes it impossible for a page
// URL containing "</script>" to close the block and turn stored data into
// markup. template.JS then tells html/template the result is already safe,
// because it cannot tell on its own inside a non-JavaScript script type.
func jsonBlock(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Info(fmt.Sprintf("json block: %v", err))
		return template.JS("null")
	}
	return template.JS(b)
}
