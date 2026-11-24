package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PropertyView is one property, with everything the templates need already
// computed.
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
	ResponseSeries   []float64
	TotalChecks      int64

	CrawlState          string
	CrawlerInsights     []Insight
	LastCrawlSuccessAt  *time.Time
	LastCrawlError      string
	LastCrawlDurationMS *int64
	LastCrawlPagesCount *int64
	NextRunAtCrawler    *time.Time
	CrawlStartedAt      *time.Time

	AuditState          string
	AuditScores         *Scores
	AuditDetails        *Details
	LastAuditSuccessAt  *time.Time
	LastAuditError      string
	LastAuditDurationMS *int64
	NextAuditRunAt      *time.Time
	AuditStartedAt      *time.Time
	AvgAuditScore       *int64

	AlertState string
	CreatedAt  time.Time
	UpdatedAt  time.Time

	// The leaf certificate's days left, nil until a check has seen one.
	CertDays    *int
	CertIssuer  string
	CertExpires string

	H *History

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

type InsightGroup struct {
	Type  string
	Items []Insight
}

// ResponseTimePoint is one bar of the phase-breakdown chart. The phases are
// pointers because rows written before those columns existed have none.
type ResponseTimePoint struct {
	Label string `json:"label"`
	Code  int64  `json:"code"`
	Total int64  `json:"total"`
	DNS   *int64 `json:"dns"`
	TCP   *int64 `json:"tcp"`
	TLS   *int64 `json:"tls"`
	TTFB  *int64 `json:"ttfb"`
}

// CodeBar is one status code as an hbar.
type CodeBar struct {
	Code  int64
	Count int64
	Pct   float64
	Width float64
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

// phaseChecks is six hours of checks, the span of the phase chart.
const phaseChecks = 120

// buildPropertyView computes everything the dashboard shows for one property,
// over one 100-check window so every figure on it covers the same span.
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

		AuditState:          p.AuditState,
		LastAuditSuccessAt:  msToTime(p.LastAuditSuccessAt),
		LastAuditError:      deref(p.LastAuditError),
		LastAuditDurationMS: p.LastAuditDurationMS,
		NextAuditRunAt:      msToTime(p.NextAuditRunAt),
		AuditStartedAt:      msToTime(p.AuditStartedAt),

		AlertState: p.AlertState,
		CreatedAt:  time.UnixMilli(p.CreatedAt).UTC(),
		UpdatedAt:  time.UnixMilli(p.UpdatedAt).UTC(),
	}

	// A property with no checks yet reads as 200 instead of 0, so a freshly
	// added site isn't a red zero until its first probe.
	v.CurrentStatus = 200
	if len(recent) > 0 {
		v.CurrentStatus = recent[0].StatusCode
	}

	// The rolling average covers 31 checks and not 100, so it describes the
	// last hour and a half.
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
	for _, r := range slices.Backward(recent) {
		if r.StatusCode == 200 {
			ticks = append(ticks, "up")
		} else {
			ticks = append(ticks, "down")
		}
	}
	if len(ticks) > 30 {
		ticks = ticks[len(ticks)-30:]
	}
	v.RecentTickStream = ticks
	for _, r := range slices.Backward(recent[:min(len(recent), 30)]) {
		v.ResponseSeries = append(v.ResponseSeries, float64(r.ResponseMS))
	}

	v.applySecurityPosture(recent)
	v.applyStoredJSON(p)
	if p.CertNotAfter != nil {
		exp := time.UnixMilli(*p.CertNotAfter).UTC()
		days := int(math.Floor(time.Until(exp).Hours() / 24))
		v.CertDays, v.CertIssuer, v.CertExpires = &days, deref(p.CertIssuer), exp.Format("2 Jan")
	}

	return v, nil
}

var hstsMaxAge = regexp.MustCompile(`max-age=(\d+)`)

// applySecurityPosture reads the latest response's headers instead of probing
// again, so it costs nothing and can be up to three minutes stale.
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

	// Every one of these spellings turns up in the wild, and a server announcing
	// its exact version is naming which CVEs to try.
	v.HidesServerVersion = true
	for _, h := range []string{"server", "x-server", "powered-by", "x-powered-by"} {
		// A CDN stamps its own name on every response and nothing behind it can
		// take that off, so it says nothing about the origin.
		if h == "server" && headers[h] == "cloudflare" {
			continue
		}
		if _, ok := headers[h]; ok {
			v.HidesServerVersion = false
			break
		}
	}

	hsts := headers["strict-transport-security"]
	if m := hstsMaxAge.FindStringSubmatch(hsts); m != nil {
		// A year is what the browser preload lists require, and anything
		// shorter reports as absent. ParseInt because a twenty digit max-age
		// is legal text that would overflow a hand-rolled accumulator.
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

// applyStoredJSON decodes the stored JSON columns, each degrading to empty
// instead of failing the request, so a malformed result costs one panel.
func (v *PropertyView) applyStoredJSON(p *Property) {
	if p.CrawlerInsights != nil {
		if err := json.Unmarshal([]byte(*p.CrawlerInsights), &v.CrawlerInsights); err != nil {
			slog.Info("property: crawler insights did not decode", slog.Any("id", v.ID), slog.Any("err", err))
			v.CrawlerInsights = nil
		}
	}

	if p.AuditScores != nil {
		var scores Scores
		if err := json.Unmarshal([]byte(*p.AuditScores), &scores); err == nil {
			v.AuditScores = &scores
			avg := int64(math.Round(float64(
				scores.Performance+scores.Accessibility+scores.BestPractices+scores.SEO) / 4))
			v.AvgAuditScore = &avg
		} else {
			slog.Info("property: audit scores did not decode", slog.Any("id", v.ID), slog.Any("err", err))
		}
	}

	if p.AuditDetails != nil && *p.AuditDetails != "null" {
		var details Details
		if err := json.Unmarshal([]byte(*p.AuditDetails), &details); err == nil {
			v.AuditDetails = &details
		}
	}
}

// groupInsights buckets findings by type and orders each bucket errors first.
// Types are sorted and the inner sort is stable, so the order holds.
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

func (s *site) dashboard(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	p, err := getProperty(r.Context(), s.db, id)
	if err != nil {
		slog.Info("dashboard", slog.Any("id", id), slog.Any("err", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if p == nil {
		s.notFound(w, r)
		return
	}

	authed := s.auth.Authenticated(r)
	if !p.IsPublic && !authed {
		// A redirect to login instead of a 404, so a bookmark lands somewhere
		// useful. It leaks that the id exists, which is fine for a v4 UUID.
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	view, err := s.buildPropertyView(r.Context(), p)
	if err != nil {
		slog.Info("dashboard", slog.Any("id", id), slog.Any("err", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	hist, err := loadHistories(r.Context(), s.db, []*Property{p}, time.Now())
	if err != nil {
		slog.Info("dashboard history", slog.Any("id", id), slog.Any("err", err))
		hist = map[uuid.UUID]*History{}
	}
	view.H = hist[p.ID]
	if view.H == nil {
		view.H = &History{}
	}

	data := s.page(r, view.Name, "Status for "+view.Name)
	data.Property = view
	if authed {
		data.InsightGroups = groupInsights(view.CrawlerInsights)
		data.Fixes = groupFixes(view.CrawlerInsights)
		data.FixTypes = fixTypes(data.Fixes)
		data.RealUsers = s.realUsers.For(r.Context(), p.URL)
	}

	// Report export is operator-only even for a public property, since the PDF
	// path spawns a Typst compile for anyone who finds the URL.
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

	recent, err := recentChecks(r.Context(), s.db, id, phaseChecks)
	if err != nil {
		slog.Info("dashboard charts", slog.Any("id", id), slog.Any("err", err))
	}
	for _, c := range recent[:min(len(recent), 20)] {
		data.RecentChecks = append(data.RecentChecks, c)
	}
	// Oldest first, so the time axis reads left to right.
	for _, c := range slices.Backward(recent) {
		data.ResponseTimes = append(data.ResponseTimes, ResponseTimePoint{
			Label: time.UnixMilli(c.CreatedAt).UTC().Format(time.RFC3339),
			Code:  c.StatusCode,
			Total: c.ResponseMS,
			DNS:   c.DNSMS,
			TCP:   c.TCPMS,
			TLS:   c.TLSMS,
			TTFB:  c.TTFBMS,
		})
	}

	codes, err := countStatusCodes(r.Context(), s.db, id)
	if err != nil {
		slog.Info("dashboard status codes", slog.Any("id", id), slog.Any("err", err))
	}
	slices.SortStableFunc(codes, func(a, b StatusCount) int { return int(b.Count - a.Count) })
	var all int64
	for _, c := range codes {
		all += c.Count
	}
	for _, c := range codes {
		data.StatusCodes = append(data.StatusCodes, CodeBar{Code: c.Code, Count: c.Count,
			Pct: float64(c.Count) / float64(all) * 100, Width: float64(c.Count) / float64(codes[0].Count) * 100})
	}

	data.PageScript = s.propsScript
	data.PageStyles = s.propsStyles
	s.renderer.Render(w, http.StatusOK, "property.html", data)
}

// statusPayload is what the dashboard's polling JavaScript reads. Its shape is
// fixed by frontend/static_src/properties/scripts/property_crawl_status.js.
type statusPayload struct {
	Crawler    crawlerStatus `json:"crawler"`
	Audit      auditStatus   `json:"audit"`
	ServerTime string        `json:"server_time"`
	OK         *bool         `json:"ok,omitempty"`
	Reason     string        `json:"reason,omitempty"`
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

type auditStatus struct {
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

// crawlProgress estimates a running crawl against PageCap, capped at 0.9 so
// the bar never claims to be finished while it is still working.
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
		Audit: auditStatus{
			State:          p.AuditState,
			StartedAt:      isoOrNil(p.AuditStartedAt),
			LastAttemptAt:  isoOrNil(p.LastAuditRunAt),
			LastSuccessAt:  isoOrNil(p.LastAuditSuccessAt),
			LastError:      nilIfEmpty(p.LastAuditError),
			LastDurationMS: p.LastAuditDurationMS,
			NextRunAt:      isoOrNil(p.NextAuditRunAt),
			IsOverdue:      overdue(p.NextAuditRunAt),
		},
		ServerTime: time.Now().UTC().Format(time.RFC3339),
	}

	if p.AuditScores != nil {
		var scores Scores
		if err := json.Unmarshal([]byte(*p.AuditScores), &scores); err == nil {
			payload.Audit.Scores = &scores
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
	// Go escapes <, > and & by default, which corrupts a response fetch()
	// parses, and these error strings come from crawled sites.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		slog.Info("write json", slog.Any("err", err))
	}
}

func (s *site) propertyStatus(w http.ResponseWriter, r *http.Request) {
	p, ok := s.lookupForJSON(w, r)
	if !ok {
		return
	}
	if !p.IsPublic && !s.auth.Authenticated(r) {
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
		slog.Info("lookup", slog.Any("id", id), slog.Any("err", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "server_error"})
		return nil, false
	}
	if p == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return nil, false
	}
	return p, true
}

// requeue is the shared body of the "run it now" buttons. It runs nothing
// itself, it moves the due time into the past for the scheduler to pick up.
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
		state, dueColumn, errColumn = p.AuditState, "next_audit_run_at", "last_audit_error"
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
		slog.Info("requeue", slog.Any("kind", kind), slog.Any("url", p.URL), slog.Any("err", err))
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

func (s *site) propertyRerunAudit(w http.ResponseWriter, r *http.Request) {
	s.requeue(w, r, "audit")
}
