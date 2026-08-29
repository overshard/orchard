package main

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"isaacbythewood.com/web"
)

// PageData is everything base.html and one page template need. One struct for
// every page rather than a per-page type: the shared chrome (menu, sidebar,
// loader, asset URLs) is most of it.
type PageData struct {
	Title       string
	Description string
	Canonical   string
	ThemeColor  string

	// The social card, and the page's JSON-LD graph already marshalled.
	OGImage     string
	JSONLD      template.JS
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
	Active     []ProjectView
	Archived   []ProjectView
	Pours      []PourView
	Generative []Generative
	Methods    []ContactMethod
}

// AboutWord carries its slot as a ready-made inline style. template.CSS is
// required, because html/template refuses to interpolate an untyped string
// into a style attribute.
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

// PourView carries a whole size ladder rather than one path, so a phone
// rendering a card at 380px does not download the 960w file. The sources are
// archival scans, up to 3900x3000 and 16MB each.
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
		// An em dash separator, which is the site's existing visual
		// identity.
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
	// Every page below the root is one level deep, so the trail is Home then
	// this page, and the home page has none.
	var crumbs []NavPage
	if href != "/" {
		canonical = baseURL + href
		for _, p := range navPages {
			if p.Href == href {
				crumbs = append(crumbs, p)
			}
		}
	}

	return PageData{
		// Always set. The grid defines a named "main" area in the centre
		// columns, and a <main> with no grid-area auto-places into the first
		// cell instead, which is the 60px sidebar gutter.
		GridArea:    template.CSS("main"),
		Title:       fullTitle,
		Description: description,
		Canonical:   canonical,
		ThemeColor:  themeColor,
		Page:        name,
		Num:         num,
		Staging:     Staging,
		// Off on the test hostname, so its traffic does not report into the
		// real site's analytics ID.
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
		OGImage:    baseURL + "/static/og/card.png",
		JSONLD:     pageGraph(fullTitle, description, canonical, crumbs),
	}
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	// "/" matches everything, so an unmatched path lands here and would
	// otherwise answer 200. "/{$}" would avoid it at the mux, but an explicit
	// check also covers the 404 page.
	if r.URL.Path != "/" {
		s.notFound(w, r)
		return
	}

	data := s.page("index", "Senior Solutions Architect at Craftmaster Furniture", "")
	// The promo slot. Absent rather than stale when the blog cannot be
	// reached, so the template does not render the card.
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

	// Two lists rather than one with a flag, because the template renders two
	// sections with different chrome.
	//
	// The stagger delay counts across both, so the entrance reads as one
	// sequence down the page rather than restarting at the archived heading.
	for i, project := range projects {
		view := ProjectView{
			Project: project,
			URL:     "https://github.com/" + githubUser + "/" + project.Slug,
			Delay:   template.CSS(fmt.Sprintf("animation-delay: %dms", i*100)),
		}
		// Only live projects carry a commit. On a read-only repo it would
		// always be the commit that archived it.
		if !project.Archived {
			view.Commit = s.commits.JSON(project.Slug)
		}

		if project.Archived {
			data.Archived = append(data.Archived, view)
		} else {
			data.Active = append(data.Active, view)
		}
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

// robots keeps a staging hostname out of search results. A noindex meta tag is
// not enough on its own, because it only works if the crawler fetches the
// page.
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
