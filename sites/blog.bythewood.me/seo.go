package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"text/template"
)

// The machine-readable half of the site: the OG card, robots.txt and the
// sitemap.
//
// None of these are HTML, so none of them go through html/template. Its
// contextual escaping is built for HTML and applying it to SVG or XML is
// guessing; encoding/xml and an explicit escape are exact.

// ogTemplate is text/template because the output is SVG. Values reach it
// pre-escaped through xmlEscape.
var ogTemplate = template.Must(template.New("og").Parse(
	`<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630">
  <defs>
    <linearGradient id="accent" x1="0%" y1="0%" x2="100%" y2="0%">
      <stop offset="0%" style="stop-color:#0e3ff4"/>
      <stop offset="100%" style="stop-color:#842bff"/>
    </linearGradient>
  </defs>
  <rect width="1200" height="630" fill="#0d1117"/>
  <rect x="80" y="80" width="5" height="160" fill="url(#accent)"/>
{{- range .Lines }}
  <text x="110" y="{{ .Y }}" font-family="sans-serif" font-size="54" font-weight="bold" fill="#f0f6fc" letter-spacing="-0.01em">{{ .Text }}</text>
{{- end }}
  <line x1="80" y1="490" x2="1120" y2="490" stroke="#30363d" stroke-width="1"/>
  <text x="80" y="548" font-family="sans-serif" font-size="28" fill="#c9d1d9">Isaac Bythewood</text>
{{- range .Tags }}
  <rect x="{{ .X }}" y="520" width="128" height="38" rx="19" fill="#21262d"/>
  <text x="{{ .LabelX }}" y="545" text-anchor="middle" font-family="sans-serif" font-size="18" fill="#c9d1d9">{{ .Text }}</text>
{{- end }}
</svg>
`))

type ogLine struct {
	Text string
	Y    int
}

type ogTag struct {
	Text   string
	X      int
	LabelX int
}

// ogImage renders a per-post social card. A slug with no post behind it, which
// is every non-post page, gets the site's own title rather than a 404: the URL
// is in a meta tag, and a broken image there is worse than a generic one.
func (s *site) ogImage(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSuffix(r.PathValue("name"), ".svg")

	title, tags := siteName, []string(nil)
	if post, ok := s.lib.Lookup(slug); ok {
		title, tags = post.Title, post.Tags
	}

	data := struct {
		Lines []ogLine
		Tags  []ogTag
	}{}

	for i, line := range wrapTitle(title, 35, 3) {
		data.Lines = append(data.Lines, ogLine{Text: xmlEscape(line), Y: 150 + i*64})
	}

	// Right aligned, so the x of each pill depends on how many there are.
	shown := tags
	if len(shown) > 4 {
		shown = shown[:4]
	}
	for i, tag := range shown {
		x := 1120 - (len(shown)-i)*140
		data.Tags = append(data.Tags, ogTag{Text: xmlEscape(tag), X: x, LabelX: x + 64})
	}

	var buf bytes.Buffer
	if err := ogTemplate.Execute(&buf, data); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = buf.WriteTo(w)
}

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
