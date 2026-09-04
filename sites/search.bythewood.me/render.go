package main

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// md renders the answer. The model writes markdown so an answer can be skimmed
// rather than read as one block of prose.
//
// Raw HTML stays disabled. The input is model output built from pages this
// server fetched, so it is not trusted, and nothing an answer needs requires
// passing HTML through.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

var citation = regexp.MustCompile(`\[(\d{1,3})\]`)

const citeAnchor = `<a class="cite" href="#p$1" data-passage="$1">[$1]</a>`

func renderMarkdown(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "<p>" + escapeHTML(src) + "</p>"
	}
	return linkCitations(buf.String())
}

// linkCitations turns [3] into an anchor after rendering rather than before,
// because goldmark drops raw HTML written into the markdown. It substitutes
// only in text, never inside a tag, so an attribute holding digits in brackets
// is left alone.
func linkCitations(h string) string {
	var out strings.Builder
	out.Grow(len(h) + 64)
	for {
		open := strings.IndexByte(h, '<')
		if open < 0 {
			out.WriteString(citation.ReplaceAllString(h, citeAnchor))
			break
		}
		out.WriteString(citation.ReplaceAllString(h[:open], citeAnchor))
		shut := strings.IndexByte(h[open:], '>')
		if shut < 0 {
			out.WriteString(h[open:])
			break
		}
		out.WriteString(h[open : open+shut+1])
		h = h[open+shut+1:]
	}
	return out.String()
}

func escapeHTML(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;",
	).Replace(s)
}
