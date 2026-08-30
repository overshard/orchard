package main

import (
	"strings"
	"testing"
	"time"
)

// A mistake in the Markdown to Typst walker is invisible: the HTML page still
// renders, the PDF still compiles, and the output is wrong somewhere on page
// two. Both cases below were real bugs, found by diffing every post's PDF.
func TestTypstEscapes(t *testing.T) {
	tests := []struct {
		name string
		md   string
		want string
	}{
		{
			// goldmark keeps Markdown's own escapes in the AST and resolves
			// them in its renderer. Without matching that, the Typst escaper
			// escapes the leftover backslash and the PDF reads
			// "reverse\_proxy".
			name: "markdown backslash escape is resolved before Typst escaping",
			md:   `a reverse\_proxy line`,
			want: `a reverse\_proxy line`,
		},
		{
			// The same resolution must not happen inside a code span. A post
			// writing this in backticks is talking about the entity itself,
			// and turning it into "/" changes the sentence.
			name: "entity inside a code span stays literal",
			md:   "Jinja2 escapes it to `&#x2f;` in URLs",
			want: `#raw("&#x2f;")`,
		},
		{
			name: "entity outside a code span is resolved",
			md:   `an &amp; ampersand`,
			want: `an & ampersand`,
		},
		{
			// Typst reads # as a code expression and @ as a label reference,
			// so an unescaped one is a compile error rather than a typo.
			name: "typst sigils are escaped",
			md:   `see #tag and @handle`,
			want: `see \#tag and \@handle`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := typstFromMarkdown(tt.md); !strings.Contains(got, tt.want) {
				t.Errorf("typstFromMarkdown(%q)\n got: %q\nwant substring: %q", tt.md, got, tt.want)
			}
		})
	}
}

// A single-element Typst array without a trailing comma is just a
// parenthesised string, and `for tag in tags` then iterates its characters.
// The PDF renders one letter per pill and nothing errors.
func TestTypstSourceSingleTag(t *testing.T) {
	post := &Post{Title: "T", Slug: "t", Date: "2026-01-01", ReadTime: 1, Tags: []string{"rust"}}
	if got := typstSource(post); !strings.Contains(got, `tags: ("rust",),`) {
		t.Errorf("single tag array missing its trailing comma:\n%s", got)
	}

	post.Tags = []string{"rust", "go"}
	if got := typstSource(post); !strings.Contains(got, `tags: ("rust", "go"),`) {
		t.Errorf("multi tag array wrong:\n%s", got)
	}
}

func TestParseFrontmatter(t *testing.T) {
	// The closing delimiter has to start a line. A bare search for "---"
	// stops at the horizontal rule inside this description and swallows the
	// rest of the frontmatter as body.
	meta, body := parseFrontmatter("---\ntitle: A --- B\ndate: 2026-01-01\n---\n\nBody text.\n")

	if meta["title"] != "A --- B" {
		t.Errorf("title = %q, want %q", meta["title"], "A --- B")
	}
	if meta["date"] != "2026-01-01" {
		t.Errorf("date = %q", meta["date"])
	}
	if body != "Body text.\n" {
		t.Errorf("body = %q", body)
	}

	// A file with no frontmatter at all is body from the first byte.
	if _, body := parseFrontmatter("Just a body.\n"); body != "Just a body.\n" {
		t.Errorf("bodyless parse = %q", body)
	}
}

// A scheduled post must be invisible everywhere until its date, and visible
// the moment it arrives without a restart. Getting the comparison backwards
// publishes a draft.
func TestPublishDateGating(t *testing.T) {
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")

	lib := &Library{
		all: []*Post{
			{Slug: "live", Date: yesterday, PublishDate: yesterday, Tags: []string{"go"}},
			{Slug: "scheduled", Date: tomorrow, PublishDate: tomorrow, Tags: []string{"rust"}},
		},
		bySlug: map[string]*Post{},
	}
	for _, p := range lib.all {
		lib.bySlug[p.Slug] = p
	}

	published, tags, years := lib.Published()
	if len(published) != 1 || published[0].Slug != "live" {
		t.Fatalf("published = %v, want just [live]", slugsOf(published))
	}
	// The facets come off the visible set, or a tag page exists for a post
	// nobody can read.
	if len(tags) != 1 || tags[0].Name != "go" {
		t.Errorf("tags = %v, want just [go]", tags)
	}
	if len(years) != 1 {
		t.Errorf("years = %v, want one", years)
	}

	if _, ok := lib.Lookup("scheduled"); ok {
		t.Error("Lookup returned a post whose publish date has not arrived")
	}
	if _, ok := lib.Lookup("live"); !ok {
		t.Error("Lookup missed a published post")
	}

	// Every post regardless of date, so a scheduled post's PDF is built
	// before it is needed.
	if len(lib.All()) != 2 {
		t.Errorf("All() = %d posts, want 2", len(lib.All()))
	}
}

func slugsOf(posts []*Post) []string {
	out := make([]string, 0, len(posts))
	for _, p := range posts {
		out = append(out, p.Slug)
	}
	return out
}

func TestTitleCase(t *testing.T) {
	for in, want := range map[string]string{
		"rust":      "Rust",
		"dark mode": "Dark Mode",
		"self-host": "Self-Host",
		"SQL":       "SQL",
		"":          "",
	} {
		if got := titleCase(in); got != want {
			t.Errorf("titleCase(%q) = %q, want %q", in, got, want)
		}
	}
}

// The OG card is 1200x630 and the title is drawn at a fixed size, so a long
// one has to wrap and then stop rather than running off the edge.
func TestWrapTitle(t *testing.T) {
	lines := wrapTitle("Cool URIs don't change unless an AI rewrites your blog and keeps going", 35, 3)
	if len(lines) > 3 {
		t.Fatalf("got %d lines, want at most 3", len(lines))
	}
	for _, line := range lines {
		if len(line) > 45 {
			t.Errorf("line too long for the card: %q", line)
		}
	}
	if got := wrapTitle("Short", 35, 3); len(got) != 1 || got[0] != "Short" {
		t.Errorf("short title = %v", got)
	}
}

// A tag with a slash in it would otherwise invent a route: /blog/tag/a/b/
// matches nothing and 404s, and url.PathEscape leaves "/" alone.
func TestTagURLEscaping(t *testing.T) {
	if got := tagURL("c++"); got != "/blog/tag/c++/" {
		t.Errorf("tagURL(c++) = %q", got)
	}
	if got := tagURL("a/b"); strings.Count(got, "/") != 4 {
		t.Errorf("tagURL(a/b) = %q, slash should be encoded", got)
	}
}

// The web renderer and the Typst renderer must agree on how a post's relative
// image reference resolves. They did not for a long time: Typst had imagePath
// from the start and the HTML path had nothing, so these images 404'd on the
// page while appearing correctly in the PDF export.
//
// Both renderers are asserted here through their real entry points rather than
// by calling the two helpers separately. Testing the helpers in isolation is
// what let the first version of this fix ship a bug: the two share one parser,
// so once the HTML side rewrote destinations in the AST, Typst prefixed the
// already-rewritten path a second time. Each unit was correct and the
// composition was not.
func TestRelativeImagesResolveToContent(t *testing.T) {
	const want = "/content/images/foo.webp"

	html := string(renderMarkdown("![alt](images/foo.webp)"))
	if !strings.Contains(html, `src="`+want+`"`) {
		t.Fatalf("html: relative image not rewritten to its served path: %s", html)
	}

	typ := typstFromMarkdown("![alt](images/foo.webp)")
	if !strings.Contains(typ, `"`+want+`"`) {
		t.Fatalf("typst: image path wrong: %s", typ)
	}

	// The failure mode that actually shipped, asserted directly in both.
	for name, out := range map[string]string{"html": html, "typst": typ} {
		if strings.Contains(out, "/content/images//content/") ||
			strings.Contains(out, "/content/content/") {
			t.Fatalf("%s: path prefixed twice: %s", name, out)
		}
	}

	// Anything already absolute, or off-site, is left alone by both.
	for _, dest := range []string{"/content/images/x.webp", "https://example.com/x.webp"} {
		md := "![a](" + dest + ")"
		for name, out := range map[string]string{
			"html":  string(renderMarkdown(md)),
			"typst": typstFromMarkdown(md),
		} {
			if !strings.Contains(out, dest) {
				t.Fatalf("%s: rewrote an absolute destination %q: %s", name, dest, out)
			}
		}
	}
}
