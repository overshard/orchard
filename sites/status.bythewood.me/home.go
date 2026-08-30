package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"
)

// PageData is everything every template needs. A struct rather than a map:
// html/template errors on a missing field but renders nothing for a missing key.
type PageData struct {
	Title         string
	Description   string
	Path          string
	Canonical     string
	Staging       bool
	Analytics     bool
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

	// Home
	TotalChecks     int64
	TotalProperties int64
	FirstCheckAt    string

	// Properties list and dashboard
	Properties    []*PropertyView
	Query         string
	Property      *PropertyView
	InsightGroups []InsightGroup
	ResponseTimes []ResponseTimePoint
	StatusCodes   []LabelCount
	UptimeSlices  []LabelPercent
	GeneratedAt   string
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	if isAuthenticated(r, s.cookieKey) {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}

	data := s.page(r, "Self-hosted uptime monitoring",
		"Self-hosted uptime monitoring with public status pages, response time history, "+
			"Lighthouse audits and crawl findings.")

	var firstCheck sql.NullInt64
	err := s.db.QueryRowContext(r.Context(),
		`SELECT (SELECT COUNT(*) FROM checks),
		        (SELECT COUNT(*) FROM properties),
		        (SELECT MIN(created_at) FROM checks)`).
		Scan(&data.TotalChecks, &data.TotalProperties, &firstCheck)
	if err != nil {
		// These numbers are decoration, so an error renders zeros rather than a 500.
		slog.Info(fmt.Sprintf("home totals: %v", err))
	}
	if firstCheck.Valid {
		data.FirstCheckAt = time.UnixMilli(firstCheck.Int64).UTC().Format("Jan 2, 2006")
	}

	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	s.renderer.Render(w, http.StatusOK, "home.html", data)
}

func (s *site) changelog(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "Changelog",
		"An ongoing changelog and upcoming list of features for Status.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	s.renderer.Render(w, http.StatusOK, "changelog.html", data)
}

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(faviconSVG))
}

const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
<polyline points="2,34 18,34 24,28 30,14 36,52 42,20 48,34 62,34" fill="none" stroke="#6b9e78" stroke-width="6" stroke-linejoin="round" stroke-linecap="round"/>
<circle cx="30" cy="14" r="3.5" fill="#c9a84c"/>
</svg>`

// robots refuses the crawl on staging. Both this and the noindex meta tag in
// base.html are needed: a crawler obeying robots.txt never fetches the tag.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if Staging {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	_, _ = fmt.Fprintf(w, "User-agent: *\nAllow: /\nSitemap: %s/sitemap.xml\n", baseURL)
}

// sitemap lists the two public pages. Property dashboards stay out even when
// public: a status page is handed to somebody, not searched for.
func sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	today := time.Now().UTC().Format("2006-01-02")
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>%s/</loc><lastmod>%s</lastmod></url>
  <url><loc>%s/changelog</loc><lastmod>%s</lastmod></url>
</urlset>
`, baseURL, today, baseURL, today)
}
