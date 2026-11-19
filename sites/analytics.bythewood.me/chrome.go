package main

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"analytics.bythewood.me/suite"
)

// chrome is the header and status line. A public property seen signed out
// gets the header with no state, since the rest isn't public.
func (s *site) chrome(r *http.Request) suite.Page {
	signedIn := s.auth.Authenticated(r)
	p := suite.Page{Tool: "analytics", SignedIn: signedIn, Source: sourceURL, Author: authorName, Year: time.Now().Year()}
	tabs := []suite.Tab{{Name: "home", Href: "/"}, {Name: "docs", Href: "/documentation"}, {Name: "source", Href: sourceURL}}
	if signedIn {
		tabs = []suite.Tab{{Name: "properties", Href: "/properties"}, {Name: "docs", Href: "/documentation"}}
	}
	path := r.URL.Path
	for _, t := range tabs {
		t.On = t.Href == path
		p.Tabs = append(p.Tabs, t)
	}
	if signedIn && path != "/properties" && path != "/documentation" && strings.Count(path, "/") == 1 && len(path) > 1 {
		p.Tabs = append(p.Tabs, suite.Tab{Name: "this property", Href: path, On: true})
	}
	p.Meta = "one row per page view · bots kept apart"
	p.Right = []suite.Seg{{Text: "times are UTC"}}
	return p
}

// listHeadline is the properties page title, the week against the one before.
func listHeadline(t PropertyTotals) template.HTML {
	if t.Properties == 0 {
		return "Nothing collected yet."
	}
	views := fmt.Sprintf("%s views this week", formatNum(t.ViewsWeek))
	if t.ViewsPrev == 0 {
		return template.HTML(template.HTMLEscapeString(views) + ".")
	}
	change := 100 * float64(t.ViewsWeek-t.ViewsPrev) / float64(t.ViewsPrev)
	switch {
	case change >= 1:
		return template.HTML(fmt.Sprintf(`%s, <em>up %.0f%%</em>.`, template.HTMLEscapeString(views), change))
	case change <= -1:
		return template.HTML(fmt.Sprintf(`%s, <em class="bad">down %.0f%%</em>.`, template.HTMLEscapeString(views), -change))
	}
	return template.HTML(template.HTMLEscapeString(views) + ", <em>steady</em>.")
}

// weekDelta is the change against the week before as a short line.
func weekDelta(week, prev int64) string {
	if prev == 0 {
		return "nothing the week before"
	}
	return fmt.Sprintf("%+.0f%% on the week before", 100*float64(week-prev)/float64(prev))
}

// graphSeries is the events chart's points as a plain series for a spark.
func graphSeries(g []GraphPoint) []float64 {
	out := make([]float64, len(g))
	for i, p := range g {
		out[i] = float64(p.Count)
	}
	return out
}

// cardHeadline is a property page's title, page views against the period
// before, since that's the number people open the page to see.
func cardHeadline(cards []EventCard) template.HTML {
	for _, c := range cards {
		if c.Name != "Total page views" {
			continue
		}
		views := template.HTMLEscapeString(c.Value) + " page views"
		switch {
		case c.PercentChange > 0:
			return template.HTML(fmt.Sprintf(`%s, <em>up %d%%</em>.`, views, c.PercentChange))
		case c.PercentChange < 0:
			return template.HTML(fmt.Sprintf(`%s, <em class="bad">down %d%%</em>.`, views, -c.PercentChange))
		}
		return template.HTML(views + ", <em>steady</em>.")
	}
	return "Nothing collected in this window."
}

// cardName drops the Total that every card name starts with.
func cardName(name string) string {
	return strings.TrimPrefix(name, "Total ")
}
