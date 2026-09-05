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

var (
	doubleSpace      = regexp.MustCompile(`[ \t]{2,}`)
	spaceBeforePunct = regexp.MustCompile(` +([,.;:!?])`)
)

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

// tidyCitations collapses a line that cites the same passage over and over.
//
// The recipe shape produced "adding the cooked bacon [14], sausage [14], eggs
// [14], and hash browns [14] [14]" from an instruction to cite what each part
// came from. Repeating one ID inside a sentence adds nothing a reader can use,
// and the prompt alone does not hold on a 4B, so the duplicates are removed
// here instead. Distinct IDs on one line are kept, since those really are
// different evidence.
func tidyCitations(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		found := citation.FindAllString(line, -1)
		if len(found) < 2 {
			continue
		}
		seen := map[string]bool{}
		var order []string
		for _, c := range found {
			if !seen[c] {
				seen[c] = true
				order = append(order, c)
			}
		}
		if len(order) == len(found) {
			continue // every citation on the line is a different one
		}
		// Removing a marker from the middle of a sentence leaves a double
		// space behind it and a space in front of the punctuation after it.
		stripped := citation.ReplaceAllString(line, "")
		stripped = doubleSpace.ReplaceAllString(stripped, " ")
		stripped = spaceBeforePunct.ReplaceAllString(stripped, "$1")
		stripped = strings.TrimRight(stripped, " ")

		tail := strings.Join(order, "")
		if strings.HasSuffix(stripped, ".") {
			lines[i] = strings.TrimSuffix(stripped, ".") + " " + tail + "."
		} else {
			lines[i] = stripped + " " + tail
		}
	}
	return strings.Join(lines, "\n")
}
