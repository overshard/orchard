package main

import (
	"strings"
	"testing"
	"time"
)

// A mistake in the Markdown to Typst walker is quiet: the page still renders,
// the PDF still compiles, and the text is wrong somewhere on page two.
func TestTypstEscapes(t *testing.T) {
	tests := []struct {
		name string
		md   string
		want string
	}{
		{
			// goldmark resolves Markdown's escapes in its renderer, not its
			// parser, so the Typst side has to do the same.
			name: "markdown backslash escape is resolved before Typst escaping",
			md:   `a reverse\_proxy line`,
			want: `a reverse\_proxy line`,
		},
		{
			// In backticks the post means the entity, not what it resolves to.
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
			// Typst reads # as a code expression and @ as a label reference, so
			// an unescaped one is a compile error rather than a typo.
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

// A single-element Typst array without a trailing comma is a parenthesised
// string, so the PDF renders one letter per pill and nothing errors.
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
	// The closing delimiter has to start a line, or a "---" inside a value
	// ends the frontmatter early.
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

	if _, body := parseFrontmatter("Just a body.\n"); body != "Just a body.\n" {
		t.Errorf("bodyless parse = %q", body)
	}
}

// Getting the publish date comparison backwards publishes a draft, so it is
// asserted from both sides.
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
	// Facets come off the visible set, or a tag page exists for a hidden post.
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

	// All() ignores the date, so a scheduled post's PDF is built ahead of it.
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

// The card and the title size are both fixed, so a long title has to wrap and
// then stop rather than running off the edge.
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

// A tag with a slash in it would invent a route that matches nothing, and
// url.PathEscape leaves "/" alone.
func TestTagURLEscaping(t *testing.T) {
	if got := tagURL("c++"); got != "/blog/tag/c++/" {
		t.Errorf("tagURL(c++) = %q", got)
	}
	if got := tagURL("a/b"); strings.Count(got, "/") != 4 {
		t.Errorf("tagURL(a/b) = %q, slash should be encoded", got)
	}
}

// Both renderers have to agree on how a relative image reference resolves, and
// they share one parser, so this goes through their real entry points. Asserting
// the two helpers alone misses Typst prefixing what the HTML side rewrote.
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

	// The double prefix, asserted directly in both.
	for name, out := range map[string]string{"html": html, "typst": typ} {
		if strings.Contains(out, "/content/images//content/") ||
			strings.Contains(out, "/content/content/") {
			t.Fatalf("%s: path prefixed twice: %s", name, out)
		}
	}

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
