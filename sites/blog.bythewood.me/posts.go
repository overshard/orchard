package main

import (
	"html/template"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// Post is one Markdown file under content/posts, parsed once at startup. A new
// post arrives with a deploy, and a deploy restarts the process.
type Post struct {
	Filename    string
	Title       string
	Slug        string
	Date        string
	PublishDate string
	Tags        []string
	Description string
	CoverImage  string
	ReadTime    int

	// template.HTML because the Markdown is the author's own, which is where
	// this site draws its trust boundary.
	BodyHTML  template.HTML
	BodyTypst string
}

// Methods rather than template helpers, so the compiler checks them.
func (p *Post) URL() string      { return "/posts/" + p.Slug + "/" }
func (p *Post) PDFURL() string   { return "/posts/" + p.Slug + "/pdf/" }
func (p *Post) MDURL() string    { return "/posts/" + p.Slug + "/md/" }
func (p *Post) OGImage() string  { return baseURL + "/og/" + p.Slug + ".png" }
func (p *Post) CoverURL() string { return "/content/images/" + p.CoverImage }

// TagLinks pairs each tag with its filter URL.
func (p *Post) TagLinks() []TagEntry {
	out := make([]TagEntry, 0, len(p.Tags))
	for _, t := range p.Tags {
		out = append(out, TagEntry{Name: t, Display: titleCase(t), URL: tagURL(t)})
	}
	return out
}

// TagEntry is one tag wherever a tag is shown. Count is zero where the tag is
// not being used as a facet.
type TagEntry struct {
	Name    string
	Display string
	URL     string
	Count   int
}

func tagURL(tag string) string   { return "/blog/tag/" + urlPathEscape(tag) + "/" }
func yearURL(year string) string { return "/blog/year/" + urlPathEscape(year) + "/" }

// Library holds every post. The published set is cached per day, not computed
// once, so a future publish_date becomes visible without a restart.
type Library struct {
	all    []*Post
	bySlug map[string]*Post

	mu        sync.Mutex
	cachedDay string
	published []*Post
	tags      []TagEntry
	years     []string
}

// LoadLibrary reads every .md file under posts/. It takes an fs.FS rather than a
// path because the content ships inside the binary.
func LoadLibrary(content fs.FS) (*Library, error) {
	entries, err := fs.ReadDir(content, "posts")
	if err != nil {
		return nil, err
	}

	lib := &Library{bySlug: make(map[string]*Post)}
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".md" {
			continue
		}
		raw, err := fs.ReadFile(content, path.Join("posts", entry.Name()))
		if err != nil {
			return nil, err
		}
		post := parsePost(entry.Name(), string(raw))
		lib.all = append(lib.all, post)
		lib.bySlug[post.Slug] = post
	}

	// Newest first, tie-broken on filename so two posts sharing a date do not
	// swap places between builds.
	sort.SliceStable(lib.all, func(i, j int) bool {
		if lib.all[i].Date != lib.all[j].Date {
			return lib.all[i].Date > lib.all[j].Date
		}
		return lib.all[i].Filename < lib.all[j].Filename
	})

	return lib, nil
}

// parsePost splits frontmatter from body and renders both output formats.
func parsePost(filename, text string) *Post {
	meta, body := parseFrontmatter(text)

	post := &Post{
		Filename:    filename,
		Title:       meta["title"],
		Slug:        meta["slug"],
		Date:        meta["date"],
		PublishDate: meta["publish_date"],
		Description: meta["description"],
		CoverImage:  meta["cover_image"],
	}
	if post.Slug == "" {
		post.Slug = strings.TrimSuffix(filename, ".md")
	}
	if post.PublishDate == "" {
		post.PublishDate = post.Date
	}
	for _, tag := range strings.Split(meta["tags"], ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			post.Tags = append(post.Tags, tag)
		}
	}

	post.BodyHTML = renderMarkdown(body)
	post.BodyTypst = typstFromMarkdown(body)

	// 200 words a minute, rounded up, never zero.
	words := len(strings.Fields(body))
	post.ReadTime = (words + 199) / 200
	if post.ReadTime < 1 {
		post.ReadTime = 1
	}

	return post
}

// parseFrontmatter reads the leading --- block as flat key: value pairs, which
// is not YAML. The closing delimiter has to start a line, or a horizontal rule
// inside a description ends the block early.
func parseFrontmatter(text string) (map[string]string, string) {
	meta := map[string]string{}
	if !strings.HasPrefix(text, "---") {
		return meta, text
	}
	rest := text[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return meta, text
	}
	block := rest[:end+1]
	body := strings.TrimLeft(rest[end+4:], "\r\n \t")

	for _, line := range strings.Split(strings.TrimSpace(block), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		meta[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return meta, body
}

// today is local, which is the zone publish_date is written in.
func today() string { return time.Now().Format("2006-01-02") }

// Published returns the visible posts, newest first, along with the tag and
// year facets computed from that same set.
func (l *Library) Published() ([]*Post, []TagEntry, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	day := today()
	if l.cachedDay == day {
		return l.published, l.tags, l.years
	}

	published := make([]*Post, 0, len(l.all))
	for _, p := range l.all {
		if p.PublishDate <= day {
			published = append(published, p)
		}
	}

	l.cachedDay = day
	l.published = published
	l.tags = collectTags(published)
	l.years = collectYears(published)
	return l.published, l.tags, l.years
}

// Lookup finds a post by slug, reporting false for an unpublished one. A caller
// should 404 rather than 403, since a 403 confirms the slug.
func (l *Library) Lookup(slug string) (*Post, bool) {
	post, ok := l.bySlug[slug]
	if !ok || post.PublishDate > today() {
		return nil, false
	}
	return post, true
}

// All returns every post regardless of publish date, so the PDF build has a
// scheduled post's PDF ready before it is visible.
func (l *Library) All() []*Post { return l.all }

func collectTags(posts []*Post) []TagEntry {
	counts := map[string]int{}
	for _, p := range posts {
		for _, t := range p.Tags {
			counts[t]++
		}
	}
	out := make([]TagEntry, 0, len(counts))
	for name, count := range counts {
		out = append(out, TagEntry{
			Name:    name,
			Display: titleCase(name),
			URL:     tagURL(name),
			Count:   count,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func collectYears(posts []*Post) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range posts {
		if len(p.Date) < 4 {
			continue
		}
		if year := p.Date[:4]; !seen[year] {
			seen[year] = true
			out = append(out, year)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

func byTag(posts []*Post, tag string) (matched, others []*Post) {
	for _, p := range posts {
		if containsFold(p.Tags, tag) {
			matched = append(matched, p)
		} else {
			others = append(others, p)
		}
	}
	return matched, others
}

func byYear(posts []*Post, year string) (matched, others []*Post) {
	for _, p := range posts {
		if strings.HasPrefix(p.Date, year) {
			matched = append(matched, p)
		} else {
			others = append(others, p)
		}
	}
	return matched, others
}

func containsFold(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// related picks posts sharing the most tags, topped up with recent ones.
func related(post *Post, posts []*Post, count int) []*Post {
	tags := map[string]bool{}
	for _, t := range post.Tags {
		tags[t] = true
	}

	type scored struct {
		post    *Post
		overlap int
	}
	var candidates []scored
	for _, p := range posts {
		if p.Slug == post.Slug {
			continue
		}
		overlap := 0
		for _, t := range p.Tags {
			if tags[t] {
				overlap++
			}
		}
		if overlap > 0 {
			candidates = append(candidates, scored{p, overlap})
		}
	}
	// Stable, so equal-overlap posts keep their newest-first order.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].overlap > candidates[j].overlap
	})

	out := make([]*Post, 0, count)
	taken := map[string]bool{post.Slug: true}
	for _, c := range candidates {
		if len(out) == count {
			break
		}
		out = append(out, c.post)
		taken[c.post.Slug] = true
	}
	for _, p := range posts {
		if len(out) == count {
			break
		}
		if !taken[p.Slug] {
			out = append(out, p)
			taken[p.Slug] = true
		}
	}
	return out
}

// titleCase capitalises each word of a tag name. strings.Title is deprecated and
// golang.org/x/text is a dependency, for a dozen lowercase ASCII tags.
func titleCase(s string) string {
	out := []rune(s)
	upper := true
	for i, r := range out {
		if upper && r >= 'a' && r <= 'z' {
			out[i] = r - 32
		}
		upper = r == ' ' || r == '-'
	}
	return string(out)
}
