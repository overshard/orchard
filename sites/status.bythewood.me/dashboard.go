package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The property dashboard: the page this app exists to render.

// PropertyView is one property, with everything the templates need already
// computed. The handlers do the work; the templates only lay it out.
type PropertyView struct {
	ID          string
	URL         string
	Name        string
	IsPublic    bool
	IsProtected bool

	CurrentStatus    int64
	AvgResponseTime  int64
	RecentUptimePct  *float64
	RecentTickStream []string
	TotalChecks      int64

	CrawlState          string
	CrawlerInsights     []Insight
	LastCrawlSuccessAt  *time.Time
	LastCrawlError      string
	LastCrawlDurationMS *int64
	LastCrawlPagesCount *int64
	NextRunAtCrawler    *time.Time
	CrawlStartedAt      *time.Time

	LighthouseState          string
	LighthouseScores         *Scores
	LighthouseDetails        *Details
	LastLighthouseSuccessAt  *time.Time
	LastLighthouseError      string
	LastLighthouseDurationMS *int64
	NextLighthouseRunAt      *time.Time
	LighthouseStartedAt      *time.Time
	AvgLighthouseScore       *int64

	AlertState string
	CreatedAt  time.Time
	UpdatedAt  time.Time

	// Security posture, read off the most recent response's headers.
	IsHTTPS                   bool
	InvalidCert               bool
	HasMIMEType               bool
	HasContentSniffProtection bool
	HasClickjackProtection    bool
	HidesServerVersion        bool
	HasHSTS                   bool
	HasHSTSPreload            bool
	HasSecurityIssue          bool
}

// InsightGroup is one crawler-insight type, with its findings ordered by
// severity.
type InsightGroup struct {
	Type  string
	Items []Insight
}

// ResponseTimePoint is one bar of the phase-breakdown chart. The four phases
// are pointers because rows written before those columns existed have none.
type ResponseTimePoint struct {
	Label string `json:"label"`
	Total int64  `json:"total"`
	DNS   *int64 `json:"dns"`
	TCP   *int64 `json:"tcp"`
	TLS   *int64 `json:"tls"`
	TTFB  *int64 `json:"ttfb"`
}

type LabelCount struct {
	Label int64 `json:"label"`
	Count int64 `json:"count"`
}

type LabelPercent struct {
	Label string  `json:"label"`
	Count float64 `json:"count"`
}

func msToTime(ms *int64) *time.Time {
	if ms == nil {
		return nil
	}
	t := time.UnixMilli(*ms).UTC()
	return &t
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// buildPropertyView computes everything the dashboard shows for one property.
//
// The 100-check window is the unit of "recent" throughout: the uptime
// percentage, the tick stream and the security posture all read it, so they
// describe the same span of time as each other.
func (s *site) buildPropertyView(ctx context.Context, p *Property) (*PropertyView, error) {
	recent, err := recentChecks(ctx, s.db, p.ID, 100)
	if err != nil {
		return nil, err
	}
	total, err := countChecks(ctx, s.db, p.ID)
	if err != nil {
		return nil, err
	}

	v := &PropertyView{
		ID:          p.ID.String(),
		URL:         p.URL,
		Name:        p.Name(),
		IsPublic:    p.IsPublic,
		IsProtected: p.IsProtected,
		TotalChecks: total,

		CrawlState:          p.CrawlState,
		LastCrawlSuccessAt:  msToTime(p.LastCrawlSuccessAt),
		LastCrawlError:      deref(p.LastCrawlError),
		LastCrawlDurationMS: p.LastCrawlDurationMS,
		LastCrawlPagesCount: p.LastCrawlPagesCount,
		NextRunAtCrawler:    msToTime(p.NextRunAtCrawler),
		CrawlStartedAt:      msToTime(p.CrawlStartedAt),

		LighthouseState:          p.LighthouseState,
		LastLighthouseSuccessAt:  msToTime(p.LastLighthouseSuccessAt),
		LastLighthouseError:      deref(p.LastLighthouseError),
		LastLighthouseDurationMS: p.LastLighthouseDurationMS,
		NextLighthouseRunAt:      msToTime(p.NextLighthouseRunAt),
		LighthouseStartedAt:      msToTime(p.LighthouseStartedAt),

		AlertState: p.AlertState,
		CreatedAt:  time.UnixMilli(p.CreatedAt).UTC(),
		UpdatedAt:  time.UnixMilli(p.UpdatedAt).UTC(),
	}

	// A property with no checks yet reads as 200 rather than 0, so a freshly
	// added site shows as healthy for the three minutes before its first probe
	// instead of flashing a red zero.
	v.CurrentStatus = 200
	if len(recent) > 0 {
		v.CurrentStatus = recent[0].StatusCode
	}

	// The rolling average is over 31 checks rather than 100, so it describes
	// the last hour and a half, which is the span that matters while something
	// is happening.
	if window := min(len(recent), 31); window > 0 {
		var sum int64
		for _, c := range recent[:window] {
			sum += c.ResponseMS
		}
		v.AvgResponseTime = sum / int64(window)
	}

	if len(recent) > 0 {
		var up int
		for _, c := range recent {
			if c.StatusCode == 200 {
				up++
			}
		}
		pct := math.Round(float64(up)/float64(len(recent))*1000) / 10
		v.RecentUptimePct = &pct
	}

	// Oldest first, so the bar reads left to right like the charts above it.
	ticks := make([]string, 0, len(recent))
	for i := len(recent) - 1; i >= 0; i-- {
		if recent[i].StatusCode == 200 {
			ticks = append(ticks, "up")
		} else {
			ticks = append(ticks, "down")
		}
	}
	if len(ticks) > 30 {
		ticks = ticks[len(ticks)-30:]
	}
	v.RecentTickStream = ticks

	v.applySecurityPosture(recent)
	v.applyStoredJSON(p)

	return v, nil
}

var hstsMaxAge = regexp.MustCompile(`max-age=(\d+)`)

// applySecurityPosture reads the latest response's headers.
//
// Derived from one real response rather than a separate probe, which is why it
// costs nothing and can be up to three minutes stale. These are configuration
// facts that change on a deploy, not conditions that flicker.
func (v *PropertyView) applySecurityPosture(recent []Check) {
	headers := map[string]string{}
	if len(recent) > 0 {
		var raw map[string]string
		if err := json.Unmarshal([]byte(recent[0].Headers), &raw); err == nil {
			for k, val := range raw {
				headers[strings.ToLower(k)] = strings.ToLower(val)
			}
		}
	}

	v.IsHTTPS = strings.HasPrefix(v.URL, "https://")
	v.InvalidCert = v.CurrentStatus == 526

	_, v.HasMIMEType = headers["content-type"]
	v.HasContentSniffProtection = headers["x-content-type-options"] == "nosniff"

	switch headers["x-frame-options"] {
	case "deny", "sameorigin", "allow-from":
		v.HasClickjackProtection = true
	}

	// Four spellings, because all four are used in the wild and a server
	// announcing its exact version in any of them is naming which CVEs to
	// try.
	v.HidesServerVersion = true
	for _, h := range []string{"server", "x-server", "powered-by", "x-powered-by"} {
		if _, ok := headers[h]; ok {
			v.HidesServerVersion = false
			break
		}
	}

	hsts := headers["strict-transport-security"]
	if m := hstsMaxAge.FindStringSubmatch(hsts); m != nil {
		// A year, because that is what the browser preload lists require. A
		// shorter max-age expires before it is useful, so it reports as
		// absent rather than partial.
		//
		// ParseInt rather than arithmetic on the digits: a max-age of
		// 99999999999999999999 is legal text and would overflow a hand-rolled
		// accumulator into a negative number.
		if seconds, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			v.HasHSTS = seconds >= 31_536_000
		}
	}
	v.HasHSTSPreload = strings.Contains(hsts, "preload")

	v.HasSecurityIssue = !v.IsHTTPS ||
		!v.HasMIMEType ||
		!v.HasContentSniffProtection ||
		!v.HasClickjackProtection ||
		!v.HidesServerVersion ||
		!v.HasHSTS ||
		!v.HasHSTSPreload
}

// applyStoredJSON decodes the three JSON columns, each degrading to empty
// rather than failing the request. These are audit results written by a
// background job, and a dashboard that will not load because last week's crawl
// wrote something malformed is worse than an empty crawler panel.
func (v *PropertyView) applyStoredJSON(p *Property) {
	if p.CrawlerInsights != nil {
		if err := json.Unmarshal([]byte(*p.CrawlerInsights), &v.CrawlerInsights); err != nil {
			slog.Info(fmt.Sprintf("property %s: crawler insights did not decode: %v", v.ID, err))
			v.CrawlerInsights = nil
		}
	}

	if p.LighthouseScores != nil {
		var scores Scores
		if err := json.Unmarshal([]byte(*p.LighthouseScores), &scores); err == nil {
			v.LighthouseScores = &scores
			avg := int64(math.Round(float64(
				scores.Performance+scores.Accessibility+scores.BestPractices+scores.SEO) / 4))
			v.AvgLighthouseScore = &avg
		} else {
			slog.Info(fmt.Sprintf("property %s: lighthouse scores did not decode: %v", v.ID, err))
		}
	}

	if p.LighthouseDetails != nil && *p.LighthouseDetails != "null" {
		var details Details
		if err := json.Unmarshal([]byte(*p.LighthouseDetails), &details); err == nil {
			v.LighthouseDetails = &details
		}
	}
}

// groupInsights buckets findings by type and orders each bucket errors first.
//
// Sorted by type name so the panels appear in the same order every time, and
// SliceStable within a bucket so findings of equal severity keep the order the
// checks produced.
func groupInsights(insights []Insight) []InsightGroup {
	buckets := map[string][]Insight{}
	for _, i := range insights {
		kind := i.Type
		if kind == "" {
			kind = "other"
		}
		buckets[kind] = append(buckets[kind], i)
	}

	kinds := make([]string, 0, len(buckets))
	for k := range buckets {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)

	rank := func(sev string) int {
		switch sev {
		case sevError:
			return 0
		case sevWarn:
			return 1
		}
		return 2
	}

	groups := make([]InsightGroup, 0, len(kinds))
	for _, k := range kinds {
		items := buckets[k]
		sort.SliceStable(items, func(a, b int) bool {
			return rank(items[a].Severity) < rank(items[b].Severity)
		})
		groups = append(groups, InsightGroup{Type: k, Items: items})
	}
	return groups
}

// dashboard renders one property.
func (s *site) dashboard(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	p, err := getProperty(r.Context(), s.db, id)
	if err != nil {
		slog.Info(fmt.Sprintf("dashboard %s: %v", id, err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if p == nil {
		s.notFound(w, r)
		return
	}

	authed := isAuthenticated(r, s.cookieKey)
	if !p.IsPublic && !authed {
		// A redirect to login rather than a 404, so an operator following a
		// bookmark on a fresh browser lands somewhere useful. It leaks that
		// the id exists, which is acceptable for an unguessable v4 UUID.
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	view, err := s.buildPropertyView(r.Context(), p)
	if err != nil {
		slog.Info(fmt.Sprintf("dashboard %s: %v", id, err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	data := s.page(r, view.Name, "Status for "+view.Name)
	data.Property = view
	data.InsightGroups = groupInsights(view.CrawlerInsights)

	// Report export is operator-only, even for a public property. The PDF path
	// spawns a Typst compile, and a public status page must not be a free
	// CPU-burning endpoint for anyone who finds the URL.
	if format := r.URL.Query().Get("report"); format != "" {
		if !authed {
			http.Redirect(w, r, "/"+view.ID, http.StatusSeeOther)
			return
		}
		if format != "md" {
			format = "pdf"
		}
		data.GeneratedAt = time.Now().Format("2006-01-02 15:04 MST")
		s.renderReport(w, r, format, view.Name, data)
		return
	}

	recent, err := recentChecks(r.Context(), s.db, id, 31)
	if err != nil {
		slog.Info(fmt.Sprintf("dashboard %s charts: %v", id, err))
	}
	// Oldest first: a time axis reads left to right.
	for i := len(recent) - 1; i >= 0; i-- {
		c := recent[i]
		data.ResponseTimes = append(data.ResponseTimes, ResponseTimePoint{
			Label: time.UnixMilli(c.CreatedAt).UTC().Format(time.RFC3339),
			Total: c.ResponseMS,
			DNS:   c.DNSMS,
			TCP:   c.TCPMS,
			TLS:   c.TLSMS,
			TTFB:  c.TTFBMS,
		})
	}

	codes, err := countStatusCodes(r.Context(), s.db, id)
	if err != nil {
		slog.Info(fmt.Sprintf("dashboard %s status codes: %v", id, err))
	}
	for _, c := range codes {
		data.StatusCodes = append(data.StatusCodes, LabelCount{Label: c.Code, Count: c.Count})
	}

	up, down, err := countUptime(r.Context(), s.db, id)
	if err != nil {
		slog.Info(fmt.Sprintf("dashboard %s uptime: %v", id, err))
	}
	pct := func(n int64) float64 {
		if total := up + down; total > 0 {
			return math.Round(float64(n)/float64(total)*10000) / 100
		}
		return 0
	}
	data.UptimeSlices = []LabelPercent{
		{Label: "Uptime", Count: pct(up)},
		{Label: "Downtime", Count: pct(down)},
	}

	data.PageScript = s.propsScript
	data.PageStyles = s.propsStyles
	s.renderer.Render(w, http.StatusOK, "property.html", data)
}

// statusPayload is what the dashboard's polling JavaScript reads, so a running
// crawl or audit updates the page without a reload. Its shape is fixed by
// frontend/static_src/properties/scripts/property_crawl_status.js.
type statusPayload struct {
	Crawler    crawlerStatus    `json:"crawler"`
	Lighthouse lighthouseStatus `json:"lighthouse"`
	ServerTime string           `json:"server_time"`
	OK         *bool            `json:"ok,omitempty"`
	Reason     string           `json:"reason,omitempty"`
}

type crawlerStatus struct {
	State              string         `json:"state"`
	StartedAt          *string        `json:"started_at"`
	LastAttemptAt      *string        `json:"last_attempt_at"`
	LastSuccessAt      *string        `json:"last_success_at"`
	LastError          *string        `json:"last_error"`
	LastDurationMS     *int64         `json:"last_duration_ms"`
	PagesCount         *int64         `json:"pages_count"`
	NextRunAt          *string        `json:"next_run_at"`
	IsOverdue          bool           `json:"is_overdue"`
	InsightsTotal      int            `json:"insights_total"`
	InsightsBySeverity map[string]int `json:"insights_by_severity"`
	Progress           *float64       `json:"progress"`
}

type lighthouseStatus struct {
	State          string  `json:"state"`
	StartedAt      *string `json:"started_at"`
	LastAttemptAt  *string `json:"last_attempt_at"`
	LastSuccessAt  *string `json:"last_success_at"`
	LastError      *string `json:"last_error"`
	LastDurationMS *int64  `json:"last_duration_ms"`
	NextRunAt      *string `json:"next_run_at"`
	IsOverdue      bool    `json:"is_overdue"`
	Scores         *Scores `json:"scores"`
}

func isoOrNil(ms *int64) *string {
	if ms == nil {
		return nil
	}
	s := time.UnixMilli(*ms).UTC().Format(time.RFC3339)
	return &s
}

func nilIfEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

// crawlProgress estimates how far along a running crawl is, as a fraction of
// PageCap.
//
// PageCap overestimates the denominator for every real site, so the bar moves
// slowly and never reaches the end. The 0.9 cap keeps it from claiming to be
// finished while still working, since a bar sitting at 100% for two minutes
// reads as a hang. The 0.05 floor gives it visible movement the moment a crawl
// starts.
func crawlProgress(p *Property) *float64 {
	if p.CrawlState != "running" {
		return nil
	}
	pages := int64(0)
	if p.LastCrawlPagesCount != nil {
		pages = *p.LastCrawlPagesCount
	}
	progress := 0.05
	if pages > 0 {
		progress = math.Min(float64(pages)/float64(PageCap), 0.9)
	}
	return &progress
}

func buildStatusPayload(p *Property) statusPayload {
	now := nowMS()

	severity := map[string]int{sevError: 0, sevWarn: 0, sevInfo: 0}
	insightsTotal := 0
	if p.CrawlerInsights != nil {
		var insights []Insight
		if err := json.Unmarshal([]byte(*p.CrawlerInsights), &insights); err == nil {
			insightsTotal = len(insights)
			for _, i := range insights {
				sev := i.Severity
				if _, known := severity[sev]; !known {
					sev = sevInfo
				}
				severity[sev]++
			}
		}
	}

	overdue := func(next *int64) bool { return next != nil && *next <= now }

	payload := statusPayload{
		Crawler: crawlerStatus{
			State:              p.CrawlState,
			StartedAt:          isoOrNil(p.CrawlStartedAt),
			LastAttemptAt:      isoOrNil(p.LastRunAtCrawler),
			LastSuccessAt:      isoOrNil(p.LastCrawlSuccessAt),
			LastError:          nilIfEmpty(p.LastCrawlError),
			LastDurationMS:     p.LastCrawlDurationMS,
			PagesCount:         p.LastCrawlPagesCount,
			NextRunAt:          isoOrNil(p.NextRunAtCrawler),
			IsOverdue:          overdue(p.NextRunAtCrawler),
			InsightsTotal:      insightsTotal,
			InsightsBySeverity: severity,
			Progress:           crawlProgress(p),
		},
		Lighthouse: lighthouseStatus{
			State:          p.LighthouseState,
			StartedAt:      isoOrNil(p.LighthouseStartedAt),
			LastAttemptAt:  isoOrNil(p.LastLighthouseRunAt),
			LastSuccessAt:  isoOrNil(p.LastLighthouseSuccessAt),
			LastError:      nilIfEmpty(p.LastLighthouseError),
			LastDurationMS: p.LastLighthouseDurationMS,
			NextRunAt:      isoOrNil(p.NextLighthouseRunAt),
			IsOverdue:      overdue(p.NextLighthouseRunAt),
		},
		ServerTime: time.Now().UTC().Format(time.RFC3339),
	}

	if p.LighthouseScores != nil {
		var scores Scores
		if err := json.Unmarshal([]byte(*p.LighthouseScores), &scores); err == nil {
			payload.Lighthouse.Scores = &scores
		}
	}

	return payload
}

// writeJSON is the single place a JSON response is written, so the header and
// the encoder settings cannot drift between endpoints.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	// Go escapes <, > and & in strings by default, which protects JSON
	// embedded in HTML and corrupts a response fetch() parses. The error
	// strings here come from crawled sites and can contain any of them.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		slog.Info(fmt.Sprintf("write json: %v", err))
	}
}

// propertyStatus is polled by the dashboard while work is in flight.
func (s *site) propertyStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := s.lookupForJSON(w, r)
	if !ok {
		return
	}
	if !p.IsPublic && !isAuthenticated(r, s.cookieKey) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
		return
	}
	writeJSON(w, http.StatusOK, buildStatusPayload(p))
}

// lookupForJSON resolves the {id} path value, answering in JSON on failure.
func (s *site) lookupForJSON(w http.ResponseWriter, r *http.Request) (*Property, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return nil, false
	}
	p, err := getProperty(r.Context(), s.db, id)
	if err != nil {
		slog.Info(fmt.Sprintf("lookup %s: %v", id, err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return nil, false
	}
	if p == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return nil, false
	}
	return p, true
}

// requeue is the shared body of the two "run it now" buttons. It runs nothing
// itself: it moves the due time into the past and clears the last error, and
// the scheduler picks it up within thirty seconds, so there is one path that
// starts this work and a button cannot bypass the semaphores.
func (s *site) requeue(w http.ResponseWriter, r *http.Request, kind string) {
	p, ok := s.lookupForJSON(w, r)
	if !ok {
		return
	}

	var state, dueColumn, errColumn string
	switch kind {
	case "crawl":
		state, dueColumn, errColumn = p.CrawlState, "next_run_at_crawler", "last_crawl_error"
	default:
		state, dueColumn, errColumn = p.LighthouseState, "next_lighthouse_run_at", "last_lighthouse_error"
	}

	if state == "queued" || state == "running" {
		payload := buildStatusPayload(p)
		no := false
		payload.OK = &no
		payload.Reason = "already_running"
		writeJSON(w, http.StatusConflict, payload)
		return
	}

	now := nowMS()
	if _, err := s.db.ExecContext(r.Context(),
		"UPDATE properties SET "+dueColumn+" = ?, "+errColumn+" = NULL, updated_at = ? WHERE id = ?",
		now, now, p.ID[:]); err != nil {
		slog.Info(fmt.Sprintf("requeue %s for %s: %v", kind, p.URL, err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "server_error"})
		return
	}

	updated, err := getProperty(r.Context(), s.db, p.ID)
	if err != nil || updated == nil {
		updated = p
	}
	payload := buildStatusPayload(updated)
	yes := true
	payload.OK = &yes
	writeJSON(w, http.StatusOK, payload)
}

func (s *site) propertyRecrawl(w http.ResponseWriter, r *http.Request) {
	s.requeue(w, r, "crawl")
}

func (s *site) propertyRerunLighthouse(w http.ResponseWriter, r *http.Request) {
	s.requeue(w, r, "lighthouse")
}
