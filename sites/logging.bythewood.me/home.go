package main

import (
	"net/http"
	"time"

	"logging.bythewood.me/suite"
)

// home is the public front page, and a redirect once you're logged in. It shows
// nothing real, since log lines carry request paths and IP addresses.
func (s *site) home(w http.ResponseWriter, r *http.Request) {
	if s.auth.Authenticated(r) {
		http.Redirect(w, r, "/overview", http.StatusSeeOther)
		return
	}

	data := s.page(r, "Self-hosted logging",
		"Structured logs from every site, shipped in process, kept in SQLite, read as graphs.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	data.Footer = suite.Footer{Tool: "logging", About: "Collects what my servers record, keeps thirty days of it searchable and tells my phone when a site goes quiet.",
		Source: sourceURL, Author: authorName, Year: data.Year, Pages: []suite.Tab{{Name: "Home", Href: "/"}, {Name: "Docs", Href: "/documentation"}}}

	s.renderer.Render(w, http.StatusOK, "home.html", data)
}

func (s *site) documentation(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "Documentation", "How a site ships its logs here, and what happens to them after.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	s.renderer.Render(w, http.StatusOK, "documentation.html", data)
}

// robots refuses every crawler, since nothing here is for a stranger.
// base.html carries a noindex tag too.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Explicit, because Cloudflare stamps its own TTL on a header-less response
	// and a stale robots.txt at the edge is four hours of the wrong answer.
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

// sitemap lists the public pages, everything behind the login is absent.
func sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")

	now := time.Now().UTC().Format("2006-01-02")
	var b []byte
	b = append(b, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"...)
	b = append(b, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"\n"...)
	for _, path := range []string{"/", "/documentation"} {
		b = append(b, "  <url><loc>"+baseURL+path+"</loc><lastmod>"+now+"</lastmod></url>\n"...)
	}
	b = append(b, "</urlset>\n"...)
	_, _ = w.Write(b)
}
