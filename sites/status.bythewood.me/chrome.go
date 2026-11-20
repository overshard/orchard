package main

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"status.bythewood.me/suite"
)

// chrome is the header and status line. A public property page seen signed out
// gets the header with no state, since the rest of the list isn't public.
func (s *site) chrome(r *http.Request) suite.Page {
	signedIn := s.auth.Authenticated(r)
	p := suite.Page{Tool: "status", SignedIn: signedIn, Source: sourceURL, Author: authorName, Year: time.Now().Year()}
	if !signedIn {
		p.Tabs = []suite.Tab{{Name: "home", Href: "/", On: r.URL.Path == "/"}, {Name: "source", Href: sourceURL}}
		p.Meta = "self-hosted · BSD 2-Clause"
		p.Right = []suite.Seg{{Text: "sign in at auth.bythewood.me"}}
		return p
	}
	p.Tabs = []suite.Tab{{Name: "properties", Href: "/properties", On: r.URL.Path == "/properties" || r.URL.Path == "/"}}
	if id, ok := strings.CutPrefix(r.URL.Path, "/"); ok && id != "" && id != "properties" {
		p.Tabs = append(p.Tabs, suite.Tab{Name: "this property", Href: r.URL.Path, On: true})
	}

	var total, down int
	var last, next int64
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*), COALESCE(SUM(alert_state = 'down'), 0),
		COALESCE(MAX(last_run_at), 0), COALESCE(MIN(next_run_at), 0) FROM properties`).Scan(&total, &down, &last, &next)
	if last > 0 {
		p.Meta = "last check " + since(time.UnixMilli(last))
		if next > 0 {
			p.Meta += " · next " + until(time.UnixMilli(next))
		}
	}
	p.Status = []suite.Seg{{Text: count(total, "property", "properties")}}
	if down > 0 {
		p.Status = append(p.Status, suite.Seg{Text: fmt.Sprintf("● %d down", down), Kind: "bad"})
	} else if total > 0 {
		p.Status = append(p.Status, suite.Seg{Text: "● all up", Kind: "ok"})
	}
	p.Right = []suite.Seg{{Text: "checks every 3m"}, {Text: "times are UTC"}}
	return p
}

func since(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

func until(t time.Time) string {
	d := time.Until(t)
	switch {
	case d <= 0:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("in %ds", int(d.Seconds()))
	}
	return fmt.Sprintf("in %dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// spell keeps a page title reading as a sentence for the counts it usually has.
func spell(n int) string {
	words := []string{"No", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten", "Eleven", "Twelve",
		"Thirteen", "Fourteen", "Fifteen", "Sixteen", "Seventeen", "Eighteen", "Nineteen", "Twenty"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}

// listSummary is the strip over the property list.
type listSummary struct {
	Total, Up int
	DownName  string
	Sites     []siteMark

	Up24, Up30       float64
	HasUp24, HasUp30 bool
	Up24Delta        string
	HourLabels       []string
	HourUp, HourP95  []float64
	Days30           []DayView
	BadDays          int

	Budget     float64
	HasBudget  bool
	BudgetKind string
	BudgetName string

	P95, P50 int64
	HasP95   bool

	Inc30 int
	MTTR  string

	CertsSoon int
	CertNote  string

	Lighthouse int64
	HasLH      bool
	SEOIssues  int
	SecIssues  int
}

// siteMark is one site as a tick in a tile.
type siteMark struct {
	Name     string
	Down     bool
	Score    int64
	HasLH    bool
	Budget   float64
	BudgetK  string
	CertDays int
	HasCert  bool
}

// CertHeight scales days left against 90 so a tick reads as a gauge.
func (m siteMark) CertHeight() int { return max(6, min(100, m.CertDays*100/90)) }

// CertKind is amber under two weeks and red under one.
func (m siteMark) CertKind() string { return certKind(m.CertDays) }

func certKind(days int) string {
	switch {
	case days < 7:
		return "bad"
	case days < 14:
		return "warn"
	}
	return ""
}

func summarize(views []*PropertyView) listSummary {
	out := listSummary{Total: len(views), MTTR: "none"}
	var up24, up30, lhSum, p95, p50, yday float64
	var n24, n30, lhN, nP, nY int
	var mttrSum time.Duration
	var mttrN int
	hourSum := make([]float64, 24)
	p95Sum := make([]float64, 24)
	hourN := 0
	soonest := -1
	out.Budget = 101
	for _, v := range views {
		h := v.H
		m := siteMark{Name: v.Name, Down: v.CurrentStatus != 200}
		if !m.Down {
			out.Up++
		} else if out.DownName == "" {
			out.DownName = v.Name
		}
		if v.AvgLighthouseScore != nil {
			m.Score, m.HasLH = *v.AvgLighthouseScore, true
			lhSum += float64(*v.AvgLighthouseScore)
			lhN++
		}
		if v.CertDays != nil {
			m.CertDays, m.HasCert = *v.CertDays, true
			if *v.CertDays < 14 {
				out.CertsSoon++
			}
			if soonest < 0 || *v.CertDays < soonest {
				soonest = *v.CertDays
				out.CertNote = fmt.Sprintf("%s in %d days", v.Name, *v.CertDays)
			}
		}
		out.SEOIssues += len(v.CrawlerInsights)
		if v.HasSecurityIssue {
			out.SecIssues++
		}
		if h.BudgetLeft != nil {
			m.Budget, m.BudgetK = *h.BudgetLeft, h.BudgetKind
			if *h.BudgetLeft < out.Budget {
				out.Budget, out.BudgetKind, out.BudgetName, out.HasBudget = *h.BudgetLeft, h.BudgetKind, v.Name, true
			}
		}
		if h.Up24 != nil {
			up24 += *h.Up24
			n24++
		}
		if h.Up30 != nil {
			up30 += *h.Up30
			n30++
		}
		if len(h.Days) >= 2 && h.Days[len(h.Days)-2].Kind != "none" {
			yday += h.Days[len(h.Days)-2].Uptime
			nY++
		}
		if h.P95 != nil {
			p95 += float64(*h.P95)
			p50 += float64(*h.P50)
			nP++
		}
		if len(h.HourUp) == 24 {
			hourN++
			out.HourLabels = h.HourLabels
			for i := range 24 {
				hourSum[i] += h.HourUp[i]
				p95Sum[i] += h.HourP95[i]
			}
		}
		out.Inc30 += h.Inc30
		for _, inc := range h.Incidents {
			if inc.End != nil {
				mttrSum += inc.dur
				mttrN++
			}
		}
		// Worst day across every site, so one red day anywhere shows.
		for i, d := range h.Ticks30() {
			if i >= len(out.Days30) {
				out.Days30 = append(out.Days30, DayView{Label: d.Label, Kind: d.Kind})
				continue
			}
			if rank(d.Kind) > rank(out.Days30[i].Kind) {
				out.Days30[i].Kind = d.Kind
			}
		}
		out.Sites = append(out.Sites, m)
	}
	for _, d := range out.Days30 {
		if d.Kind == "bad" || d.Kind == "warn" {
			out.BadDays++
		}
	}
	if n24 > 0 {
		out.Up24, out.HasUp24 = up24/float64(n24), true
		if nY > 0 {
			d := out.Up24 - yday/float64(nY)
			out.Up24Delta = fmt.Sprintf("%+.2f on yesterday", d)
			if math.Abs(d) < 0.005 {
				out.Up24Delta = "level with yesterday"
			}
		}
	}
	if n30 > 0 {
		out.Up30, out.HasUp30 = up30/float64(n30), true
	}
	if nP > 0 {
		out.P95, out.P50, out.HasP95 = int64(p95/float64(nP)), int64(p50/float64(nP)), true
	}
	if lhN > 0 {
		out.Lighthouse, out.HasLH = int64(lhSum/float64(lhN)), true
	}
	if mttrN > 0 {
		out.MTTR = shortDur(mttrSum / time.Duration(mttrN))
	}
	if hourN > 0 {
		for i := range 24 {
			out.HourUp = append(out.HourUp, hourSum[i]/float64(hourN))
			out.HourP95 = append(out.HourP95, p95Sum[i]/float64(hourN))
		}
	}
	return out
}

func rank(kind string) int {
	switch kind {
	case "bad":
		return 3
	case "warn":
		return 2
	case "":
		return 1
	}
	return 0
}

type secCheck struct {
	Name string
	OK   bool
}

// secChecks is the security header list, in the order the page shows it.
func secChecks(v *PropertyView) []secCheck {
	return []secCheck{
		{"Served over HTTPS", v.IsHTTPS},
		{"MIME types set", v.HasMIMEType},
		{"Content sniff protection", v.HasContentSniffProtection},
		{"Clickjack protection", v.HasClickjackProtection},
		{"Server version hidden", v.HidesServerVersion},
		{"HSTS", v.HasHSTS},
		{"HSTS preload", v.HasHSTSPreload},
	}
}

// secFirstFail names the first header check that fails, for a tile's note.
func secFirstFail(v *PropertyView) string {
	short := []string{"https", "content type", "nosniff", "frame options", "server header", "hsts", "hsts preload"}
	for i, c := range secChecks(v) {
		if !c.OK {
			return short[i]
		}
	}
	return ""
}

func secPass(v *PropertyView) int {
	n := 0
	for _, c := range secChecks(v) {
		if c.OK {
			n++
		}
	}
	return n
}

// insightTitle is a crawler insight type as a heading.
func insightTitle(kind string) string {
	if kind == typeSEO {
		return "SEO"
	}
	if kind == "" {
		return kind
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}
