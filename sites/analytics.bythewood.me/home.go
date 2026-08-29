package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// home is the public front page, and a redirect once you are logged in. It
// shows totals across every property: what this instance has actually
// collected, rather than a claim about what it could.
func (s *site) home(w http.ResponseWriter, r *http.Request) {
	if isAuthenticated(r, s.cookieKey) {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}

	var properties, events int64
	var first sql.NullInt64
	err := s.db.QueryRowContext(r.Context(), `SELECT
	    (SELECT COUNT(*) FROM properties),
	    (SELECT COUNT(*) FROM events),
	    (SELECT MIN(created_at) FROM events)`).Scan(&properties, &events, &first)
	if err != nil {
		// A blank set of totals is a worse home page, not a broken one.
		slog.Info(fmt.Sprintf("home totals: %v", err))
	}

	data := s.page(r, "Self-hosted analytics",
		"Self-hosted website analytics. Page views, clicks, scrolls, sessions, and custom events.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	data.TotalProperties = properties
	data.TotalEvents = events
	// One operator, by construction: there is no user table to count.
	data.TotalUsers = 1
	if first.Valid {
		data.FirstEventAt = time.UnixMilli(first.Int64).Format("Jan 2, 2006")
	}

	s.renderer.Render(w, http.StatusOK, "home.html", data)
}

func (s *site) changelog(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "Changelog", "What's new in Analytics.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	s.renderer.Render(w, http.StatusOK, "changelog.html", data)
}

func (s *site) documentation(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "Documentation", "How to embed and operate Analytics.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	s.renderer.Render(w, http.StatusOK, "documentation.html", data)
}

// The favicon is drawn inline rather than shipped as a file, so it cannot
// drift from the navbar logo, which is the same markup.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
  <rect x="6"  y="38" width="10" height="22" rx="1.5" fill="#6b9e78"/>
  <rect x="20" y="28" width="10" height="32" rx="1.5" fill="#6b9e78"/>
  <rect x="34" y="18" width="10" height="42" rx="1.5" fill="#6b9e78"/>
  <rect x="48" y="8"  width="10" height="52" rx="1.5" fill="#6b9e78"/>
  <rect x="48" y="8"  width="10" height="6"  rx="1.5" fill="#c9a84c"/>
</svg>`

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(faviconSVG))
}

// robots keeps a staging hostname out of search results. Both this and the
// noindex meta tag in base.html, because a meta tag is only read if the
// crawler fetches the page, and robots.txt is what stops it fetching.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if Staging {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	_, _ = w.Write([]byte("User-agent: *\nAllow: /\nSitemap: " + baseURL + "/sitemap.xml\n"))
}

// sitemap lists the three public pages. Dashboards are absent even when a
// property is public: that is a URL its owner chose to hand out, not one to
// volunteer to a crawler.
func sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")

	now := time.Now().Format("2006-01-02")
	var b []byte
	b = append(b, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"...)
	b = append(b, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"\n"...)
	for _, path := range []string{"/", "/documentation", "/changelog"} {
		b = append(b, "  <url><loc>"+baseURL+path+"</loc><lastmod>"+now+"</lastmod></url>\n"...)
	}
	b = append(b, "</urlset>\n"...)
	_, _ = w.Write(b)
}
