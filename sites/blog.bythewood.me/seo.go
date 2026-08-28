package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
)

// The machine-readable half of the site: robots.txt, the sitemap and the feed.
//
// None of these are HTML, so none of them go through html/template. Its
// contextual escaping is built for HTML and applying it to XML is guessing;
// encoding/xml and an explicit escape are exact.
//
// The social card used to live here too, as SVG rendered per request. It is a
// PNG compiled at build time now (og.go), because no social platform accepts
// image/svg+xml for og:image.

// wrapTitle greedily breaks a title into at most maxLines lines of about
// maxChars, which is as much typography as a 1200x630 card needs.
func wrapTitle(title string, maxChars, maxLines int) []string {
	var lines []string
	current := ""
	for _, word := range strings.Fields(title) {
		switch {
		case current == "":
			current = word
		case len(current)+len(word)+1 > maxChars:
			lines = append(lines, current)
			current = word
		default:
			current += " " + word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// favicon is generated rather than a file, so the two colours in it stay the
// two colours in the stylesheet.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 16 16">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="rgb(14, 63, 180)"/>
      <stop offset="100%" stop-color="rgb(107, 158, 120)"/>
    </linearGradient>
  </defs>
  <rect width="16" height="16" rx="2" fill="url(#g)"/>
  <text x="8" y="11.5" text-anchor="middle" font-family="monospace" font-weight="bold" font-size="9" fill="rgba(255,255,255,0.9)">
    &gt;_
  </text>
</svg>
`

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(faviconSVG))
}

// robots keeps the staging hostname out of the index entirely. A noindex meta
// tag on its own does nothing here, because a crawler told not to fetch the
// page never sees the tag.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if Staging {
		_, _ = fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
		return
	}
	_, _ = fmt.Fprintf(w, "User-agent: *\nAllow: /\n\nSitemap: %s/sitemap.xml\n", baseURL)
}

type urlEntry struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
}

type urlSet struct {
	XMLName xml.Name   `xml:"urlset"`
	NS      string     `xml:"xmlns,attr"`
	URLs    []urlEntry `xml:"url"`
}

func (s *site) sitemap(w http.ResponseWriter, r *http.Request) {
	published, tags, years := s.lib.Published()

	set := urlSet{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	set.URLs = append(set.URLs,
		urlEntry{Loc: baseURL + "/", ChangeFreq: "weekly"},
		urlEntry{Loc: baseURL + "/blog/", ChangeFreq: "weekly"},
	)

	// A tag or year page is as fresh as its newest post, which is the only
	// lastmod that means anything for a page that is entirely derived.
	tagLastMod := map[string]string{}
	yearLastMod := map[string]string{}
	for _, post := range published {
		set.URLs = append(set.URLs, urlEntry{
			Loc: baseURL + post.URL(), LastMod: post.Date, ChangeFreq: "yearly",
		})
		for _, tag := range post.Tags {
			if post.Date > tagLastMod[tag] {
				tagLastMod[tag] = post.Date
			}
		}
		if len(post.Date) >= 4 {
			if year := post.Date[:4]; post.Date > yearLastMod[year] {
				yearLastMod[year] = post.Date
			}
		}
	}
	for _, tag := range tags {
		set.URLs = append(set.URLs, urlEntry{
			Loc: baseURL + tag.URL, LastMod: tagLastMod[tag.Name], ChangeFreq: "monthly",
		})
	}
	for _, year := range years {
		set.URLs = append(set.URLs, urlEntry{
			Loc: baseURL + yearURL(year), LastMod: yearLastMod[year], ChangeFreq: "yearly",
		})
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buf)
	encoder.Indent("", "  ")
	if err := encoder.Encode(set); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	buf.WriteByte('\n')

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = buf.WriteTo(w)
}
