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

	"analytics.bythewood.me/web"
)

const (
	dayMS = 24 * 60 * 60 * 1000
	// Long enough to show a trend, short enough that the graph buckets daily.
	defaultRangeDays = 28

	maxRangeDays = 3650
)

// PageData is everything base.html and one page template need; the fields for
// pages other than the one rendering are zero.
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

	TotalProperties int64
	TotalEvents     int64
	TotalUsers      int
	FirstEventAt    string

	Properties []PropertyRow
	Totals     PropertyTotals
	Query      string

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
	// no JavaScript to compute this themselves.
	ChartPolyline   string
	ChartLabelStart string
	ChartLabelEnd   string
	ChartPeakCount  int64
	ChartPeakLabel  string
	BreakdownTotals BreakdownTotals
	TopCountries    []LabelCount
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
// session; everything else, including a missing property, redirects.
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

	authed := s.auth.Authenticated(r)
	if !p.IsPublic && !authed {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
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

	// "custom" or absent means derive the range from the two dates.
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

	// Clamped outside the switch, so both arms are bounded. rangeDays sizes an
	// allocating bucket slice that is then inlined into the page, and a public
	// property makes this reachable with no cookie.
	rangeDays = min64(max64(rangeDays, 1), maxRangeDays)

	// The window too, so an ancient start date cannot widen the graph behind
	// the clamp above.
	if startMS < endMS-maxRangeDays*dayMS {
		startMS = endMS - maxRangeDays*dayMS
	}

	prevStartMS := startMS - rangeDays*dayMS
	prevEndMS := endMS - rangeDays*dayMS
	filterURL := q.Get("filter_url")

	// Anchor the graph to the requested end date; stepping back from today
	// charts a historical range as zeros beside real metric cards.
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

	// Report export is operator-only even for a public property: the PDF path
	// spawns an unthrottled Typst compile.
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
// block; html/template cannot work that out inside a non-JavaScript script type.
func jsonBlock(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Info(fmt.Sprintf("json block: %v", err))
		return template.JS("null")
	}
	return template.JS(b)
}
