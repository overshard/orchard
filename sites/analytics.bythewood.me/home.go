package main

import (
	"analytics.bythewood.me/suite"

	"net/http"
	"time"
)

// home is the public front page, and a redirect to /properties once logged in.
func (s *site) home(w http.ResponseWriter, r *http.Request) {
	if s.auth.Authenticated(r) {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}

	data := s.page(r, "Self-hosted analytics",
		"Self-hosted website analytics. Page views, clicks, scrolls, sessions, and custom events.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	data.Footer = suite.Footer{Tool: "analytics", About: "Counts the visitors to the sites I run, keeps a year of it and tells my phone when one gets busy.",
		Source: sourceURL, Author: authorName, Year: data.Year, Pages: []suite.Tab{{Name: "Home", Href: "/"}, {Name: "Docs", Href: "/documentation"}}}

	s.renderer.Render(w, http.StatusOK, "home.html", data)
}

func (s *site) documentation(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "Documentation", "How to embed and operate Analytics.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	s.renderer.Render(w, http.StatusOK, "documentation.html", data)
}

// robots refuses every crawl, production included, since nothing here is for a
// stranger. It duplicates the noindex in base.html because a crawler that obeys
// robots.txt never fetches the page to see the tag.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Cloudflare stamps its own TTL on a header-less response, and a stale
	// robots.txt at the edge is hours of the wrong answer.
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

// sitemap lists the public pages. Dashboards stay out even when public, since
// that URL is one an owner chose to hand out.
func sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")

	now := time.Now().Format("2006-01-02")
	var b []byte
	b = append(b, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"...)
	b = append(b, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"\n"...)
	for _, path := range []string{"/", "/documentation"} {
		b = append(b, "  <url><loc>"+baseURL+path+"</loc><lastmod>"+now+"</lastmod></url>\n"...)
	}
	b = append(b, "</urlset>\n"...)
	_, _ = w.Write(b)
}
