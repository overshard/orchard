package main

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io/fs"
	"math/rand/v2"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"blog.bythewood.me/web"
)

// Crumb is one link in the breadcrumb bar. An empty URL is the current page,
// rendered as plain text.
type Crumb struct {
	Title string
	URL   string
}

// PageData is everything base.html and one page template need. One struct for
// every page rather than a type each: the shared chrome (nav, breadcrumbs,
// footer, asset URLs, social meta) is most of it.
type PageData struct {
	Title       string
	Description string
	Canonical   string
	Staging     bool
	Analytics   bool
	AnalyticsID string
	Year        int

	Script string
	Styles []string

	// The navbar lists every tag, and highlights one when a tag page is open.
	NavTags     []TagEntry
	ActiveTag   string
	Breadcrumbs []Crumb

	// Social meta is opt in. The home and 404 pages do without it.
	ShowSocial bool
	OGImage    string

	// The page's JSON-LD graph, already marshalled. Set to the site graph for
	// every page and overridden with the article graph on a post.
	JSONLD template.JS

	FooterProjects []FooterLink
	FooterLinks    []FooterLink
	SourceURL      string

	// Search box contents, so a query survives the round trip.
	Query string

	// Page specific. A nil slice renders nothing, which is what lets one
	// template set cover six pages.
	Latest      *Post
	Posts       []*Post
	ExtraPosts  []*Post
	RandomPosts []*Post
	Post        *Post
	Related     []*Post
	Tags        []TagEntry
	Years       []string
	ActiveYear  string
	Heading     string
	Kicker      string
	NoResults   bool
}

type site struct {
	renderer *web.Renderer
	lib      *Library
	// Filesystems rather than paths, because in a release build these are
	// inside the executable and there is no path to hand out. A dev build
	// passes os.DirFS over the same directories.
	content fs.FS
	pdfs    fs.FS
	og      fs.FS
	script  string
	styles  []string
}

// page builds the shared half of PageData.
func (s *site) page(r *http.Request, title, description string) PageData {
	_, tags, _ := s.lib.Published()
	return PageData{
		Title:          title,
		Description:    description,
		Canonical:      baseURL + r.URL.Path,
		Staging:        Staging,
		Analytics:      !Staging,
		AnalyticsID:    analyticsID,
		Year:           time.Now().Year(),
		Script:         s.script,
		Styles:         s.styles,
		NavTags:        tags,
		FooterProjects: footerProjects,
		FooterLinks:    footerLinks,
		SourceURL:      sourceURL,
		OGImage:        baseURL + "/og/" + ogSiteCard + ".png",
		JSONLD:         siteGraph(),
	}
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	published, _, _ := s.lib.Published()

	data := s.page(r, siteName,
		"Writing about webdev, infrastructure, security, and tooling by Isaac Bythewood, "+
			"a Senior Solutions Architect in Elkin, NC.")
	data.ShowSocial = true
	data.Heading = siteName

	if len(published) > 0 {
		data.Latest = published[0]
		data.RandomPosts = pickRandom(published[1:], 3)
	}

	s.renderer.Render(w, http.StatusOK, "home.html", data)
}

func (s *site) blogIndex(w http.ResponseWriter, r *http.Request) {
	published, tags, years := s.lib.Published()

	data := s.page(r, "Blog",
		"Posts on webdev, coding, security, and sysadmin by Isaac Bythewood.")
	data.ShowSocial = true
	data.Heading = "Blog"
	data.Breadcrumbs = []Crumb{{Title: "Home", URL: "/"}, {Title: "Blog"}}
	data.Posts = published
	data.Tags = tags
	data.Years = years

	s.renderer.Render(w, http.StatusOK, "blog.html", data)
}

func (s *site) blogByTag(w http.ResponseWriter, r *http.Request) {
	tag, err := url.PathUnescape(r.PathValue("tag"))
	if err != nil {
		s.notFound(w, r)
		return
	}

	published, tags, years := s.lib.Published()
	matched, others := byTag(published, tag)
	if len(matched) == 0 {
		s.notFound(w, r)
		return
	}

	display := titleCase(tag)
	data := s.page(r, "Blog - Tag - "+display,
		"Posts on webdev, coding, security, and sysadmin by Isaac Bythewood. "+
			"Currently filtered by tag "+display+".")
	data.ShowSocial = true
	data.Heading = "Blog"
	data.Kicker = display
	data.Breadcrumbs = []Crumb{{Title: "Home", URL: "/"}, {Title: "Blog", URL: "/blog/"}, {Title: display}}
	data.Posts = matched
	data.Tags = tags
	data.Years = years
	data.ActiveTag = tag

	// A thin filter page is mostly empty space, so it offers a few posts from
	// outside the filter rather than stopping at one card.
	if len(matched) < 5 {
		data.ExtraPosts = take(others, 4)
	}

	s.renderer.Render(w, http.StatusOK, "blog.html", data)
}

func (s *site) blogByYear(w http.ResponseWriter, r *http.Request) {
	year := r.PathValue("year")

	published, tags, years := s.lib.Published()
	matched, others := byYear(published, year)
	if len(matched) == 0 {
		s.notFound(w, r)
		return
	}

	data := s.page(r, "Blog - Year - "+year,
		"Posts on webdev, coding, security, and sysadmin by Isaac Bythewood. "+
			"Currently filtered by year "+year+".")
	data.ShowSocial = true
	data.Heading = "Blog"
	data.Kicker = year
	data.Breadcrumbs = []Crumb{{Title: "Home", URL: "/"}, {Title: "Blog", URL: "/blog/"}, {Title: year}}
	data.Posts = matched
	data.Tags = tags
	data.Years = years
	data.ActiveYear = year

	if len(matched) < 5 {
		data.ExtraPosts = take(others, 4)
	}

	s.renderer.Render(w, http.StatusOK, "blog.html", data)
}

func (s *site) post(w http.ResponseWriter, r *http.Request) {
	post, ok := s.lookup(r)
	if !ok {
		s.notFound(w, r)
		return
	}

	published, _, _ := s.lib.Published()

	data := s.page(r, post.Title, post.Description)
	data.ShowSocial = true
	data.OGImage = post.OGImage()
	data.JSONLD = postGraph(post)
	data.Breadcrumbs = []Crumb{{Title: "Home", URL: "/"}, {Title: "Blog", URL: "/blog/"}, {Title: post.Title}}
	data.Post = post
	data.Related = related(post, published, 3)

	s.renderer.Render(w, http.StatusOK, "post.html", data)
}

// postPDF serves the PDF built during the image build. It is a handler rather
// than a static mount so an unpublished post stays a 404 and the filename is
// the slug rather than whatever is on disk.
func (s *site) postPDF(w http.ResponseWriter, r *http.Request) {
	post, ok := s.lookup(r)
	if !ok {
		s.notFound(w, r)
		return
	}

	raw, err := fs.ReadFile(s.pdfs, post.Slug+".pdf")
	if err != nil {
		// Only reachable when the build skipped PDF generation, the normal
		// state of a local checkout without typst installed.
		http.Error(w, "pdf not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+post.Slug+`.pdf"`)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	// A zero modtime, so no Last-Modified goes out: an embedded file has no
	// meaningful timestamp, and revalidation is left to the Cache-Control
	// above. ServeContent still handles range requests off the reader.
	http.ServeContent(w, r, post.Slug+".pdf", time.Time{}, bytes.NewReader(raw))
}

// postMarkdown hands back the source file, which is "view source" for a blog
// whose posts are files.
func (s *site) postMarkdown(w http.ResponseWriter, r *http.Request) {
	post, ok := s.lookup(r)
	if !ok {
		s.notFound(w, r)
		return
	}

	raw, err := fs.ReadFile(s.content, path.Join("posts", post.Filename))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="`+post.Slug+`.md"`)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(raw)
}

func (s *site) lookup(r *http.Request) (*Post, bool) {
	slug, err := url.PathUnescape(r.PathValue("slug"))
	if err != nil {
		return nil, false
	}
	return s.lib.Lookup(slug)
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "404", "That means the page you are looking for doesn't exist.")
	s.renderer.Render(w, http.StatusNotFound, "notfound.html", data)
}

// redirectPost keeps the pre-2026 /blog/<slug>/ URLs alive. Cool URIs do not
// change, and one of the posts these serve says so.
func (s *site) redirectPost(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/posts/"+r.PathValue("slug")+"/", http.StatusMovedPermanently)
}

// redirectPostFormat handles the old export URLs. Only pdf and md ever
// existed, and anything else under a post is a 404 rather than a redirect to
// a URL that will 404 anyway.
func (s *site) redirectPostFormat(w http.ResponseWriter, r *http.Request) {
	format := r.PathValue("format")
	if format != "pdf" && format != "md" {
		s.notFound(w, r)
		return
	}
	http.Redirect(w, r, "/posts/"+r.PathValue("slug")+"/"+format+"/", http.StatusMovedPermanently)
}

// redirectSlash sends /blog to /blog/, so the slashless form is not a 404.
func redirectSlash(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
}

func take(posts []*Post, n int) []*Post {
	if len(posts) > n {
		return posts[:n]
	}
	return posts
}

// pickRandom returns n posts chosen without replacement, leaving the caller's
// slice alone.
func pickRandom(posts []*Post, n int) []*Post {
	shuffled := make([]*Post, len(posts))
	copy(shuffled, posts)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return take(shuffled, n)
}

// urlPathEscape encodes a tag or year for a URL path segment. url.PathEscape
// leaves "/" alone, which would let a tag containing one invent a route.
func urlPathEscape(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "/", "%2F")
}

// latestJSON publishes the newest published post as JSON, so that
// isaacbythewood.com's promo slot can render a "latest post" card without
// scraping HTML or being hardcoded to a project that might end.
//
// A hand-written shape rather than the whole Post: the consumer needs four
// fields, and a full dump would put BodyHTML on the wire and make every future
// field on Post a public API by accident.
//
// The URL is absolute because the reader is a different origin. No CORS: the
// only consumer fetches this server side.
func (s *site) latestJSON(w http.ResponseWriter, r *http.Request) {
	published, _, _ := s.lib.Published()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Short, so a new post reaches the other site within the hour rather than
	// whenever a container happens to restart.
	w.Header().Set("Cache-Control", "public, max-age=300")

	if len(published) == 0 {
		_, _ = w.Write([]byte("{}\n"))
		return
	}

	p := published[0]
	enc := json.NewEncoder(w)
	// These strings land in another site's HTML through html/template, which
	// escapes for that context already, so escaping here only corrupts an
	// ampersand in a title.
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
		Date        string `json:"date"`
	}{
		Title:       p.Title,
		Description: p.Description,
		URL:         baseURL + p.URL(),
		Date:        p.PublishDate,
	})
}
