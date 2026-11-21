package main

import (
	"status.bythewood.me/suite"

	"fmt"
	"html/template"
	"net/http"
	"time"
)

// PageData is everything every template needs. It's a struct instead of a map
// since html/template errors on a missing field but renders nothing for a missing key.
type PageData struct {
	Title         string
	Description   string
	Path          string
	Canonical     string
	AnalyticsID   string
	Authenticated bool
	Year          int
	BaseURL       string
	SourceURL     string
	SiteName      string
	AuthorName    string

	OGImage    string
	JSONLD     template.JS
	Script     string
	Styles     []string
	PageScript string
	PageStyles []string

	// Login
	Next  string
	Error string

	// Properties list and dashboard
	Properties    []*PropertyView
	Query         string
	Property      *PropertyView
	InsightGroups []InsightGroup
	ResponseTimes []ResponseTimePoint
	StatusCodes   []CodeBar
	RecentChecks  []Check
	GeneratedAt   string

	Suite   suite.Page
	Footer  suite.Footer
	Down    int
	Summary listSummary
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	if s.auth.Authenticated(r) {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}

	data := s.page(r, "uptime, speed and crawls",
		"Self-hosted uptime monitoring with public status pages, response time history, "+
			"Lighthouse audits and crawl findings.")

	data.Footer = suite.Footer{Tool: "status", About: "Watches the sites I run, audits them and tells my phone when one goes down or comes back.",
		Source: sourceURL, Author: authorName, Year: data.Year, Pages: []suite.Tab{{Name: "Home", Href: "/"}}}

	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	s.renderer.Render(w, http.StatusOK, "home.html", data)
}

// robots refuses the crawl everywhere since nothing here is for a stranger. The
// noindex tag in base.html is needed too, a crawler obeying robots.txt never sees it.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Explicit, because Cloudflare stamps its own TTL on a header-less response
	// and a stale robots.txt at the edge is four hours of the wrong answer.
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

// sitemap lists the one public page. Property dashboards stay out even when
// public, since a status page is handed to somebody and not searched for.
func sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	today := time.Now().UTC().Format("2006-01-02")
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>%s/</loc><lastmod>%s</lastmod></url>
</urlset>
`, baseURL, today)
}
