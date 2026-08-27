package main

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"bythewood.me/orchard/internal/web"
)

// PageData is everything base.html and one page template need. One struct for
// every page rather than a per-page type: the shared chrome (menu, sidebar,
// loader, asset URLs) is most of it, and splitting that out would mean
// embedding a common struct in five places to save nothing.
type PageData struct {
	Title       string
	Description string
	Canonical   string
	ThemeColor  string
	Page        string
	Num         string
	Staging     bool
	Analytics   bool
	LoaderWaits bool
	GridArea    template.CSS

	Script   string
	Styles   []string
	NavPages []NavPage
	TopLinks []TopLink

	// The menu panel is in the chrome, so every page carries its ladder.
	MenuSrc    string
	MenuSrcset template.Srcset
	AvatarSrc  string

	HeroSrc    string
	HeroSrcset template.Srcset

	Latest     *LatestPost
	Words      []AboutWord
	Projects   []ProjectView
	Pours      []PourView
	Generative []Generative
	Methods    []ContactMethod
}

// AboutWord carries its slot as a ready-made inline style. template.CSS is
// required: html/template refuses to interpolate an untyped string into a
// style attribute, and rightly so.
type AboutWord struct {
	Text  string
	Style template.CSS
}

// ProjectView is a Project plus the two things only the request knows: the
// current commit, and the stagger delay for its entrance animation.
type ProjectView struct {
	Project
	URL    string
	Commit string
	Delay  template.CSS
}

// PourView carries a whole size ladder, not one path.
//
// next/image chose a width per viewport, and a plain <img> only matches that
// with srcset. Shipping a single size instead means a phone rendering a card at
// 380px still downloads the 960w file, which is most of the difference that
// remained after the archival scans (3900x3000, up to 16MB each) were dealt
// with.
type PourView struct {
	Pour
	CardSrc     string // fallback for browsers without srcset
	CardSrcset  template.Srcset
	LightboxSrc string
}

type site struct {
	renderer *web.Renderer
	commits  *CommitCache
	latest   *LatestCache
	script   string
	styles   []string
}

// page builds the shared half of PageData for a given nav entry.
func (s *site) page(name, title, description string) PageData {
	fullTitle := siteTitle
	if title != "" {
		// The separator is an em dash in the original. Prose in this workspace
		// does not use them, but a <title> is a label rather than prose and
		// this one is the site's existing visual identity, so it stays.
		fullTitle = title + " — " + siteTitle
	}
	if description == "" {
		description = siteDesc
	}

	num := "000"
	href := "/" + name
	if name == "index" {
		href = "/"
	}
	for _, p := range navPages {
		if p.Href == href {
			num = p.Num
		}
	}

	canonical := baseURL
	if href != "/" {
		canonical = baseURL + href
	}

	return PageData{
		// Always set, never omitted. The grid defines a named "main" area in
		// the centre columns; a <main> with no grid-area auto-places into the
		// first cell instead, which is the 60px sidebar gutter.
		GridArea:    template.CSS("main"),
		Title:       fullTitle,
		Description: description,
		Canonical:   canonical,
		ThemeColor:  themeColor,
		Page:        name,
		Num:         num,
		Staging:     Staging,
		// Deliberately off on the test hostname. Every visit here is Isaac
		// checking his own rebuild, and letting it report into the real site's
		// analytics ID would put test traffic in the numbers he actually
		// reads. Flip when next.isaacbythewood.com becomes the apex.
		Analytics: !Staging,
		Script:    s.script,
		Styles:    s.styles,
		NavPages:  navPages,
		TopLinks:  topLinks,
		// 40vw wide but full height, and object-fit: cover on a 4:3 source
		// means height drives the scale, so it needs more width than 40vw
		// suggests.
		AvatarSrc:  avatarURL(),
		MenuSrc:    pourURL("006", images.CardWidths[len(images.CardWidths)-1]),
		MenuSrcset: pourSrcset("006", images.CardWidths[len(images.CardWidths)-1], images.LightboxWidth),
	}
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	// Go's "/" matches everything, so an unmatched path lands here. That is
	// the exact bug the tunnel test shipped with, where /nope answered 200 and
	// the rig could not show an error crossing the tunnel. "/{$}" would avoid
	// it at the mux, but an explicit check also covers the 404 page.
	if r.URL.Path != "/" {
		s.notFound(w, r)
		return
	}

	data := s.page("index", "Senior Solutions Architect at Craftmaster Furniture", "")
	// The promo slot. Absent rather than stale when the blog cannot be
	// reached, so the template simply does not render the card.
	if post, ok := s.latest.Get(); ok {
		data.Latest = &post
	}
	// The only page that holds the curtain: it waits on the hero image.
	data.LoaderWaits = true
	// Full-bleed 100vw, so it is the one image that earns a 2400w candidate.
	data.HeroSrc = pourURL(images.Hero, images.LightboxWidth)
	data.HeroSrcset = pourSrcset(images.Hero,
		images.CardWidths[len(images.CardWidths)-1], images.LightboxWidth, images.HeroWidth)
	s.renderer.Render(w, http.StatusOK, "index.html", data)
}

func (s *site) about(w http.ResponseWriter, r *http.Request) {
	data := s.page("about", "About", "A brief professional history of myself.")

	data.Words = make([]AboutWord, 0, len(aboutWords))
	for i, word := range aboutWords {
		data.Words = append(data.Words, AboutWord{
			Text:  word,
			Style: template.CSS(fmt.Sprintf("top: %dvh", i*11)),
		})
	}

	s.renderer.Render(w, http.StatusOK, "about.html", data)
}

func (s *site) code(w http.ResponseWriter, r *http.Request) {
	data := s.page("code", "Code", "Some of my most recent coding projects.")

	data.Projects = make([]ProjectView, 0, len(projects))
	for i, project := range projects {
		data.Projects = append(data.Projects, ProjectView{
			Project: project,
			URL:     "https://github.com/" + githubUser + "/" + project.Slug,
			Commit:  s.commits.JSON(project.Slug),
			Delay:   template.CSS(fmt.Sprintf("animation-delay: %dms", i*100)),
		})
	}

	s.renderer.Render(w, http.StatusOK, "code.html", data)
}

func (s *site) art(w http.ResponseWriter, r *http.Request) {
	data := s.page("art", "Art", "Some of my art... what even is art...")

	data.Pours = make([]PourView, 0, len(pours))
	for _, pour := range pours {
		data.Pours = append(data.Pours, PourView{
			Pour:        pour,
			CardSrc:     pourURL(pour.Number, images.CardWidths[0]),
			CardSrcset:  pourSrcset(pour.Number, images.CardWidths...),
			LightboxSrc: pourURL(pour.Number, images.LightboxWidth),
		})
	}
	data.Generative = generative

	s.renderer.Render(w, http.StatusOK, "art.html", data)
}

func (s *site) contact(w http.ResponseWriter, r *http.Request) {
	data := s.page("contact", "Contact", "How to get in contact with me.")
	// The contact page breaks out of the centre column into the full grid.
	data.GridArea = template.CSS("1 / 1 / 4 / 7")
	data.Methods = contactMethods
	s.renderer.Render(w, http.StatusOK, "contact.html", data)
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	data := s.page("notfound", "Not Found", "That page does not exist.")
	s.renderer.Render(w, http.StatusNotFound, "notfound.html", data)
}

// robots keeps the staging hostname out of search results entirely.
//
// A noindex meta tag is not enough on its own here, because it only works if
// the crawler fetches the page. Both are set, and they agree.
func (s *site) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if Staging {
		fmt.Fprintf(w, "User-agent: *\nDisallow: /\n")
		return
	}
	fmt.Fprintf(w, "User-agent: *\nDisallow:\n\nSitemap: %s/sitemap.xml\n", baseURL)
}

func (s *site) sitemap(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, page := range navPages {
		loc := baseURL
		if page.Href != "/" {
			loc += page.Href
		}
		fmt.Fprintf(&b, "  <url><loc>%s</loc></url>\n", loc)
	}
	b.WriteString("</urlset>\n")

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func (s *site) manifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	fmt.Fprintf(w, `{
  "name": %q,
  "short_name": %q,
  "background_color": %q,
  "display": "standalone",
  "scope": "/",
  "start_url": "/",
  "icons": [
    {
      "src": "/static/images/favicon.png",
      "type": "image/png",
      "sizes": "512x512"
    }
  ],
  "theme_color": %q
}
`, siteTitle, siteTitle, themeColor, themeColor)
}
