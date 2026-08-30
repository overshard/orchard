package main

// README rendering and file highlighting.
//
// Two of the three dependencies here are already blog.bythewood.me's, so this
// is one new one. The Rust build used pulldown-cmark, ammonia and syntect; the
// Go equivalents are goldmark, bluemonday and chroma.
//
// The sanitiser is not optional and not belt-and-braces. A README is arbitrary
// text written by whoever wrote the repository, goldmark passes raw HTML
// through by default, and this site renders READMEs from mirrored repositories
// that were never reviewed. Without bluemonday a README containing a script tag
// is stored XSS on the repository page.

import (
	"bytes"
	"fmt"
	"html/template"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

// maxReadmeSize caps what is rendered on a repository page. A README past this
// is not a README.
const maxReadmeSize = 1 << 20

// maxHighlightSize caps syntax highlighting. Past it the file is shown as plain
// text with a note, because chroma on a multi-megabyte minified bundle is
// seconds of CPU for a page nobody reads.
const maxHighlightSize = 1 << 20

var (
	markdown = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
		),
		goldmark.WithRendererOptions(
			// Unsafe is on and the output is sanitised below. The
			// alternative, leaving raw HTML escaped, breaks the badge
			// images and anchor targets that nearly every README uses.
			goldmarkhtml.WithUnsafe(),
		),
	)

	// The policy. UGCPolicy allows the formatting a README needs and strips
	// script, style, iframe, object, form and every event handler attribute.
	sanitizer = newSanitizer()
)

func newSanitizer() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// Badges. Nearly every README opens with a row of them, and UGCPolicy
	// already allows img with src; this adds the sizing attributes that keep
	// them from rendering at full resolution.
	p.AllowAttrs("width", "height", "align").OnElements("img")
	// Anchors on headings, which is what makes a long README navigable.
	p.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	// The class attribute on code blocks, so chroma's spans survive.
	p.AllowAttrs("class").OnElements("code", "pre", "span", "div", "table", "td", "th", "tr")
	p.AllowAttrs("align").OnElements("td", "th", "p", "div")
	// Task lists render as disabled checkboxes.
	p.AllowAttrs("type", "checked", "disabled").OnElements("input")
	p.AllowElements("input")
	return p
}

// RenderMarkdown converts a README to sanitised HTML.
func RenderMarkdown(src []byte) (template.HTML, error) {
	if len(src) > maxReadmeSize {
		src = src[:maxReadmeSize]
	}
	var buf bytes.Buffer
	if err := markdown.Convert(src, &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return template.HTML(sanitizer.SanitizeBytes(buf.Bytes())), nil
}

// readmeNames is the set looked for on a repository page, in preference order.
// The plain and .txt forms are included because several of the older archived
// repositories predate the .md convention.
var readmeNames = []string{
	"README.md", "readme.md", "README.markdown",
	"README.rst", "README.txt", "README", "readme",
}

// IsMarkdown decides whether a README gets goldmark or a <pre>. A .rst or a
// bare README is not Markdown and rendering it as such mangles it.
func IsMarkdown(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mdown":
		return true
	}
	return false
}

// chromaStyle is written for this site rather than borrowed from a theme set.
//
// A stock theme (the Rust build used base16-eighties) is a rectangle of somebody
// else's palette dropped into the middle of the page. These values are the
// site's own tokens: the warm graphite ground, the periwinkle accent family for
// anything structural, and a small set of muted hues for literals that stay
// clear of the green and red the diff view reserves for meaning.
var chromaStyle = chroma.MustNewStyle("repos", chroma.StyleEntries{
	chroma.Background:          "#e4dfe4 bg:#151317",
	chroma.Comment:             "italic #8b8291",
	chroma.CommentPreproc:      "#a99bf5",
	chroma.Keyword:             "#a99bf5",
	chroma.KeywordType:         "#e6c88a",
	chroma.KeywordConstant:     "#e6c88a",
	chroma.KeywordDeclaration:  "#a99bf5",
	chroma.Operator:            "#b9b1c6",
	chroma.Punctuation:         "#9a92a5",
	chroma.Name:                "#e4dfe4",
	chroma.NameBuiltin:         "#c9b8f0",
	chroma.NameClass:           "#e6c88a",
	chroma.NameFunction:        "#c9b8f0",
	chroma.NameNamespace:       "#e6c88a",
	chroma.NameAttribute:       "#e6c88a",
	chroma.NameTag:             "#d79ec0",
	chroma.NameDecorator:       "#d79ec0",
	chroma.NameVariable:        "#e4dfe4",
	chroma.NameConstant:        "#e0a07a",
	chroma.LiteralString:       "#8fbf9f",
	chroma.LiteralStringEscape: "#e0a07a",
	chroma.LiteralNumber:       "#e0a07a",
	chroma.GenericDeleted:      "#e08a8a",
	chroma.GenericInserted:     "#8fbf9f",
	chroma.GenericHeading:      "bold #e4dfe4",
	chroma.GenericSubheading:   "bold #c9b8f0",
	chroma.GenericEmph:         "italic",
	chroma.GenericStrong:       "bold",
	chroma.Error:               "#e08a8a",
})

// Highlight renders one file as HTML.
//
// The lexer is chosen from the filename and, failing that, from the content.
// Deliberately not chroma's Match on a full path: the Rust build's review found
// that syntect's equivalent opened the request-supplied name on the server
// filesystem. Only the basename is ever passed here, and only to a matcher that
// looks at the string.
func Highlight(name string, src []byte) (template.HTML, bool) {
	if len(src) > maxHighlightSize || !utf8.Valid(src) {
		return "", false
	}

	lexer := lexers.Match(path.Base(name))
	if lexer == nil {
		lexer = lexers.Analyse(string(src))
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, string(src))
	if err != nil {
		return "", false
	}

	formatter := chromahtml.New(
		chromahtml.WithClasses(false),
		chromahtml.WithLineNumbers(true),
		chromahtml.WithLinkableLineNumbers(true, "L"),
		chromahtml.TabWidth(4),
	)

	style := chromaStyle
	if style == nil {
		style = styles.Fallback
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return "", false
	}
	return template.HTML(buf.String()), true
}

// IsBinary decides whether to offer a file for download instead of rendering
// it. The NUL test is what git itself uses, and it is right far more often than
// an extension list.
func IsBinary(src []byte) bool {
	limit := min(len(src), 8000)
	return bytes.IndexByte(src[:limit], 0) >= 0
}

// languageOf is the label on a blob page, and nothing more: it is chroma's name
// for the lexer, not a claim about the repository.
func languageOf(name string) string {
	if l := lexers.Match(path.Base(name)); l != nil {
		return l.Config().Name
	}
	return ""
}
