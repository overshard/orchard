package main

import (
	"net/http"
	"time"
)

// home is the public front page, and a redirect once you are logged in. The
// numbers on it are counts and a start date: how much this instance has taken
// in, and nothing at all about what any of it says. Log lines carry request
// paths and IP addresses, so unlike analytics there is no public dashboard here
// and no public toggle to make one.
func (s *site) home(w http.ResponseWriter, r *http.Request) {
	if isAuthenticated(r, s.cookieKey) {
		http.Redirect(w, r, "/overview", http.StatusSeeOther)
		return
	}

	data := s.page(r, "Self-hosted logging",
		"Structured logs from every site, shipped in process, kept in SQLite, read as graphs.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	data.Stats = siteStats(r.Context(), s.db)

	s.renderer.Render(w, http.StatusOK, "home.html", data)
}

func (s *site) documentation(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "Documentation", "How a site ships its logs here, and what happens to them after.")
	data.PageScript = s.pagesScript
	data.PageStyles = s.pagesStyles
	s.renderer.Render(w, http.StatusOK, "documentation.html", data)
}

// The favicon is drawn inline rather than shipped as a file, so it cannot drift
// from the navbar logo, which is the same markup.
//
// The mark is four stacked lines of unequal length: a log, read top to bottom,
// with one line amber because the one that is not like the others is the whole
// reason to look.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
  <rect x="8" y="10" width="48" height="7" rx="1.5" fill="#6b9e78"/>
  <rect x="8" y="24" width="34" height="7" rx="1.5" fill="#6b9e78"/>
  <rect x="8" y="38" width="44" height="7" rx="1.5" fill="#c9a84c"/>
  <rect x="8" y="52" width="26" height="7" rx="1.5" fill="#6b9e78"/>
</svg>`

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(faviconSVG))
}

// robots keeps a staging hostname out of search results. Both this and the
// noindex meta tag in base.html, because a meta tag is only read if the crawler
// fetches the page, and robots.txt is what stops it fetching.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if Staging {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	_, _ = w.Write([]byte("User-agent: *\nAllow: /\nDisallow: /overview\nDisallow: /sources/\nDisallow: /search\nSitemap: " + baseURL + "/sitemap.xml\n"))
}

// sitemap lists the two public pages. Everything behind the login is absent,
// and the Disallow lines above say so a second time, because a dashboard URL
// answering a redirect to /login is still a URL nobody needs crawled.
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
