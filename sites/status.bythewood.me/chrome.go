package main

import (
	"fmt"
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

// listSummary is the tiles over the property list, each with the trend the
// properties share, averaged check by check.
type listSummary struct {
	Uptime     float64
	HasUptime  bool
	AvgMS      int64
	Response   []float64
	Lighthouse int64
	HasLH      bool
	SEOIssues  int
	SecIssues  int
	// UpShare is the share of sites up at each of the last 30 checks.
	UpShare []float64
	Sites   []siteMark
}

// siteMark is one site as a tick in a tile.
type siteMark struct {
	Name  string
	Down  bool
	Score int64
	HasLH bool
}

func summarize(views []*PropertyView) listSummary {
	var out listSummary
	var upSum float64
	var upN, lhN int
	var lhSum, msSum int64
	sums := make([]float64, 30)
	counts := make([]float64, 30)
	ups := make([]float64, 30)
	seen := make([]float64, 30)
	for _, v := range views {
		m := siteMark{Name: v.Name, Down: v.CurrentStatus != 200}
		if v.AvgLighthouseScore != nil {
			m.Score, m.HasLH = *v.AvgLighthouseScore, true
		}
		out.Sites = append(out.Sites, m)
		toff := 30 - len(v.RecentTickStream)
		for i, t := range v.RecentTickStream {
			seen[toff+i]++
			if t == "up" {
				ups[toff+i]++
			}
		}
		if v.RecentUptimePct != nil {
			upSum += *v.RecentUptimePct
			upN++
		}
		if v.AvgLighthouseScore != nil {
			lhSum += *v.AvgLighthouseScore
			lhN++
		}
		msSum += v.AvgResponseTime
		out.SEOIssues += len(v.CrawlerInsights)
		if v.HasSecurityIssue {
			out.SecIssues++
		}
		// Right aligned, so the newest check of every property lines up.
		off := 30 - len(v.ResponseSeries)
		for i, ms := range v.ResponseSeries {
			sums[off+i] += ms
			counts[off+i]++
		}
	}
	if upN > 0 {
		out.Uptime, out.HasUptime = upSum/float64(upN), true
	}
	if lhN > 0 {
		out.Lighthouse, out.HasLH = lhSum/int64(lhN), true
	}
	if len(views) > 0 {
		out.AvgMS = msSum / int64(len(views))
	}
	for i := range sums {
		if counts[i] > 0 {
			out.Response = append(out.Response, sums[i]/counts[i])
		}
		if seen[i] > 0 {
			out.UpShare = append(out.UpShare, ups[i]/seen[i]*100)
		}
	}
	return out
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
