package main

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"logging.bythewood.me/suite"
)

// chrome is the header and status line. The dashboards add their own state to
// the status line once they know it.
func (s *site) chrome(r *http.Request) suite.Page {
	signedIn := s.auth.Authenticated(r)
	p := suite.Page{Tool: "logging", SignedIn: signedIn, Source: sourceURL, Author: authorName, Year: time.Now().Year()}
	tabs := []suite.Tab{{Name: "home", Href: "/"}, {Name: "docs", Href: "/documentation"}, {Name: "source", Href: sourceURL}}
	if signedIn {
		tabs = []suite.Tab{{Name: "overview", Href: "/overview"}, {Name: "search", Href: "/search"}, {Name: "docs", Href: "/documentation"}}
	}
	for _, t := range tabs {
		t.On = t.Href == r.URL.Path || (t.Href == "/overview" && strings.HasPrefix(r.URL.Path, "/sources/"))
		p.Tabs = append(p.Tabs, t)
	}
	p.Meta = fmt.Sprintf("raw lines kept %d days · rollups kept for good", int(rawRetention/(24*time.Hour)))
	p.Right = []suite.Seg{{Text: "times are UTC"}}
	return p
}

// dashStatus is the status line for an overview or a source page.
func dashStatus(d *Dashboard) []suite.Seg {
	out := []suite.Seg{{Text: d.RangeName}}
	if d.Source != "" {
		out = append(out, suite.Seg{Text: d.Source})
	} else {
		live := 0
		for _, src := range d.Sources {
			if src.IsLive {
				live++
			}
		}
		kind := "ok"
		if live < len(d.Sources) {
			kind = "warn"
		}
		out = append(out, suite.Seg{Text: fmt.Sprintf("● %d of %d sources live", live, len(d.Sources)), Kind: kind})
	}
	if d.Totals.Errors > 0 {
		out = append(out, suite.Seg{Text: formatNum(d.Totals.Errors) + " errors", Kind: "bad"})
	}
	out = append(out, suite.Seg{Text: formatNum(d.Totals.Records) + " records"})
	return out
}

// headline is a dashboard's title, what stands out in the window.
func headline(d *Dashboard) string {
	who := "Every source"
	if d.Source != "" {
		who = template.HTMLEscapeString(d.Source)
	}
	switch {
	case d.Totals.Records == 0:
		return who + `, <em>quiet</em> in this window.`
	case d.Totals.Server5xx > 0 && d.Totals.ErrorRate >= 1:
		return who + fmt.Sprintf(`, <em class="bad">%.1f%% of requests failing</em>.`, d.Totals.ErrorRate)
	case d.Totals.Errors > 0:
		return who + fmt.Sprintf(`, <em class="bad">%s %s</em>.`, formatNum(d.Totals.Errors), map[bool]string{true: "error", false: "errors"}[d.Totals.Errors == 1])
	}
	return who + `, <em>nothing wrong</em>.`
}

// series pulls one measure out of the volume chart's buckets for a tile spark.
func series(field string, g []GraphPoint) []float64 {
	out := make([]float64, len(g))
	for i, p := range g {
		switch field {
		case "count":
			out[i] = float64(p.Count)
		case "errors":
			out[i] = float64(p.Errors)
		case "4xx":
			out[i] = float64(p.Client4xx)
		case "direct":
			out[i] = float64(p.DirectHits)
		case "rate":
			if p.Requests > 0 {
				out[i] = float64(p.Server5xx) / float64(p.Requests)
			}
		case "latency":
			if p.DurCount > 0 {
				out[i] = p.DurSum / float64(p.DurCount)
			}
		}
	}
	return out
}
