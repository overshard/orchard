package main

import (
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"isaacbythewood.com/web"
)

// PageData is everything base.html and one page template need.
type PageData struct {
	Title       string
	Description string
	Canonical   string
	ThemeColor  string

	OGImage     string
	JSONLD      template.JS
	Page        string
	Num         string
	LoaderWaits bool
	GridArea    template.CSS

	Script   string
	Styles   []string
	NavPages []NavPage
	TopLinks []TopLink

	MenuSrc    string
	MenuSrcset template.Srcset
	AvatarSrc  string

	HeroSrc    string
	HeroSrcset template.Srcset

	Latest     *LatestPost
	Words      []AboutWord
	Groups     []GroupView
	Tape       []TapeLine
	Topo       Topology
	Stats      CodeStats
	Active     []ProjectView
	Archived   []ProjectView
	Pours      []PourView
	Generative []Generative
	Methods    []ContactMethod
}

// AboutWord carries its slot as an inline style, because html/template refuses
// to interpolate an untyped string into a style attribute.
type AboutWord struct {
	Text  string
	Style template.CSS
}

type TileView struct {
	Site
	Source string
	Commit string
	Health Health
	Open   bool
	Delay  template.CSS
}

type GroupView struct {
	Group
	Tiles []TileView
}

// TapeLine is one row of the commit tape, the latest commit under one site.
type TapeLine struct {
	SHA, Where, Message, Age string
	when                     time.Time
	more                     int
}

type CodeStats struct {
	Sites, Commits, Median int
}

type ProjectView struct {
	Project
	URL    string
	Commit string
	Delay  template.CSS
}

// PourView carries a whole size ladder instead of one path so a phone
// rendering a card at 380px doesn't download the widest scan.
type PourView struct {
	Pour
	CardSrc     string // fallback for browsers without srcset
	CardSrcset  template.Srcset
	LightboxSrc string
}

type site struct {
	renderer *web.Renderer
	commits  *CommitCache
	probes   *ProbeCache
	latest   *LatestCache
	// Resolves Next.js era /_next/image requests onto the built files.
	nextImages *nextImageIndex
	script     string
	styles     []string
}

func (s *site) page(name, title, description string) PageData {
	fullTitle := siteTitle
	switch {
	case name == "index":
		fullTitle = siteTitle + " · " + title
	case title != "":
		fullTitle = title + " · " + siteTitle
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
		// Always set, or a <main> with no grid-area auto-places into the first
		// cell, which is the 60px sidebar gutter.
		GridArea:    template.CSS("main"),
		Title:       fullTitle,
		Description: description,
		Canonical:   canonical,
		ThemeColor:  themeColor,
		Page:        name,
		Num:         num,
		// Off on the test hostname so it doesn't report into the real ID.
		Script:   s.script,
		Styles:   s.styles,
		NavPages: navPages,
		TopLinks: topLinks,
		// The menu panel is 40vw wide but full height, and cover on a 4:3
		// source means height drives the scale, so it needs the wider file.
		AvatarSrc:  avatarURL(),
		MenuSrc:    pourURL("006", images.CardWidths[len(images.CardWidths)-1]),
		MenuSrcset: pourSrcset("006", images.CardWidths[len(images.CardWidths)-1], images.LightboxWidth),
		OGImage:    baseURL + "/static/og/card.png",
		JSONLD:     pageGraph(fullTitle, description, canonical, crumbs),
	}
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	// "/" matches everything, so an unmatched path lands here and would answer
	// 200. An explicit check also covers rendering the 404 page.
	if r.URL.Path != "/" {
		s.notFound(w, r)
		return
	}

	data := s.page("index", "Senior Solutions Architect", "")
	// No card at all until the blog has answered once.
	if post, ok := s.latest.Get(); ok {
		data.Latest = &post
	}
	// The only page that holds the curtain, since it waits on the hero image.
	data.LoaderWaits = true
	// Full-bleed 100vw, so it's the one image with a HeroWidth candidate.
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

	// The template renders each list as a section with its own chrome, but the
	// delay counts across all of them so the entrance reads as one sequence
	// instead of restarting at each heading.
	delay := 0
	nextDelay := func() template.CSS {
		style := template.CSS(fmt.Sprintf("animation-delay: %dms", delay*100))
		delay++
		return style
	}

	health := func(site Site) Health {
		if site.Visit == "" {
			return s.probes.Edge()
		}
		return s.probes.Health(site.Name)
	}

	var medians []int
	for _, g := range groups {
		view := GroupView{Group: g}
		for _, site := range sites {
			if site.Group != g.Name {
				continue
			}
			h := health(site)
			if h.Median > 0 && site.Visit != "" {
				medians = append(medians, h.Median)
			}
			view.Tiles = append(view.Tiles, TileView{
				Site:   site,
				Source: "https://github.com/" + githubUser + "/" + monorepo + "/tree/main/" + site.SourcePath(),
				Commit: s.commits.JSON(site.SourcePath()),
				Health: h,
				Open:   site.Name == openSite,
				Delay:  nextDelay(),
			})
		}
		data.Groups = append(data.Groups, view)
	}

	data.Topo = buildTopology(health)
	data.Tape = s.tape(time.Now())
	data.Stats = CodeStats{Commits: s.commits.Total()}
	for _, site := range sites {
		if site.Visit != "" {
			data.Stats.Sites++
		}
	}
	if len(medians) > 0 {
		data.Stats.Median = median(medians)
	}

	for _, project := range projects {
		view := ProjectView{
			Project: project,
			URL:     "https://github.com/" + githubUser + "/" + project.Slug,
			Delay:   nextDelay(),
		}
		// A read-only repo's latest commit is always the one that archived it.
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

// tape is the latest commit under each site, newest first, with a commit
// that touched several sites shown once.
func (s *site) tape(now time.Time) []TapeLine {
	var lines []TapeLine
	seen := map[string]int{}
	for _, site := range sites {
		c, ok := s.commits.Get(site.SourcePath())
		if !ok {
			continue
		}
		if i, dup := seen[c.SHA]; dup {
			lines[i].more++
			continue
		}
		seen[c.SHA] = len(lines)
		when, err := time.Parse(time.RFC3339, c.Date)
		if err != nil {
			continue
		}
		lines = append(lines, TapeLine{SHA: c.SHA, Where: site.SourcePath(), Message: c.Message, Age: age(now.Sub(when)), when: when})
	}
	for i := range lines {
		if lines[i].more > 0 {
			lines[i].Where += fmt.Sprintf(" +%d", lines[i].more)
		}
	}
	slices.SortStableFunc(lines, func(a, b TapeLine) int { return b.when.Compare(a.when) })
	return lines
}

func age(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(1, int(d.Minutes())))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
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
	data := s.page("notfound", "Not found", "That page does not exist.")
	// A 404 has no URL of its own to name as canonical.
	data.Canonical = ""
	s.renderer.Render(w, http.StatusNotFound, "notfound.html", data)
}

func redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	}
}

func (s *site) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
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

// Thousands get a comma, the way the counter renders them once it lands.
func (c CodeStats) CommitsText() string {
	n := strconv.Itoa(c.Commits)
	for i := len(n) - 3; i > 0; i -= 3 {
		n = n[:i] + "," + n[i:]
	}
	return n
}
