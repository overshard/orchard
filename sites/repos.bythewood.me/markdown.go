package main

// README rendering and file highlighting. The bluemonday pass is not optional:
// goldmark passes raw HTML through, and this renders READMEs from mirrored
// repositories nobody here reviewed.

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

// maxReadmeSize caps what is rendered on a repository page.
const maxReadmeSize = 1 << 20

// maxHighlightSize caps syntax highlighting; past it the file is shown as plain
// text, since chroma on a minified bundle is seconds of CPU.
const maxHighlightSize = 1 << 20

var (
	markdown = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
		),
		goldmark.WithRendererOptions(
			// Unsafe is on because escaping raw HTML breaks the badges and
			// anchors most READMEs use; sanitizer below is what makes it safe.
			goldmarkhtml.WithUnsafe(),
		),
	)

	// UGCPolicy allows README formatting and strips script, style, iframe,
	// object, form and every event handler attribute.
	sanitizer = newSanitizer()
)

func newSanitizer() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// Sizing attributes, so badges do not render at full resolution.
	p.AllowAttrs("width", "height", "align").OnElements("img")
	p.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	// chroma's spans carry their colours on class attributes.
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

// readmeNames is looked at in order. The plain and .txt forms are here because
// some mirrored repositories are older than the .md convention.
var readmeNames = []string{
	"README.md", "readme.md", "README.markdown",
	"README.rst", "README.txt", "README", "readme",
}

// IsMarkdown decides whether a README gets goldmark or a <pre>.
func IsMarkdown(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mdown":
		return true
	}
	return false
}

// chromaStyle uses the site's own tokens, and keeps literals clear of the green
// and red the diff view reserves for meaning.
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

// Highlight renders one file as HTML. Only the basename reaches the lexer
// matcher, never a request-supplied path that something might open.
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

// IsBinary decides whether to offer a file for download. The NUL test is what git
// itself uses.
func IsBinary(src []byte) bool {
	limit := min(len(src), 8000)
	return bytes.IndexByte(src[:limit], 0) >= 0
}

// languageOf is chroma's lexer name, used only as a label on a blob page.
func languageOf(name string) string {
	if l := lexers.Match(path.Base(name)); l != nil {
		return l.Config().Name
	}
	return ""
}
