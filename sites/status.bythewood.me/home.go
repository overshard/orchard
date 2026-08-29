package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"
)

// The marketing pages, the SEO endpoints and the 404.

// PageData is everything every template needs, plus the page-specific fields.
//
// One struct rather than a map because html/template reports a missing struct
// field as an error at render time, while a missing map key silently renders
// nothing. The analytics port found a whole report section that had never once
// rendered for exactly that reason: minijinja treated an undefined name as
// falsy and skipped it for the life of the feature.
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

	// The social card, and the page's JSON-LD graph already marshalled.
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
	// The operator has no use for the marketing page. Anyone else has no use
	// for the dashboard.
	if isAuthenticated(r, s.cookieKey) {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}

	// "Home" carried no information: the title element is the strongest
	// on-page signal a search engine has, and "Home · Status" spent all of it
	// on two words that describe every website ever made. This says what the
	// h1 already says.
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
		// The page is a marketing page; the numbers on it are decoration. A
		// database hiccup should render zeros, not a 500.
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

// favicon is served inline rather than from disk: it is 300 bytes, it never
// changes, and a file would mean a build step and a cache policy for it.
func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(faviconSVG))
}

const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
<polyline points="2,34 18,34 24,28 30,14 36,52 42,20 48,34 62,34" fill="none" stroke="#6b9e78" stroke-width="6" stroke-linejoin="round" stroke-linecap="round"/>
<circle cx="30" cy="14" r="3.5" fill="#c9a84c"/>
</svg>`

// robots refuses the crawl on staging.
//
// The staging hostname is a complete duplicate of the real site, which is the
// one way it can damage it: two hosts serving identical content is the textbook
// duplicate-content problem, and the copy could outrank the original. This and
// the noindex meta tag in base.html are both needed, because either alone has
// a hole: a crawler that ignores robots.txt still reads the meta tag, and one
// that obeys robots.txt never fetches the page to find it.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if Staging {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	_, _ = fmt.Fprintf(w, "User-agent: *\nAllow: /\nSitemap: %s/sitemap.xml\n", baseURL)
}

// sitemap lists the two public pages.
//
// Property dashboards are deliberately absent even when public: a status page
// is something the operator hands somebody, not something that should turn up
// in a search for the site it monitors.
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
