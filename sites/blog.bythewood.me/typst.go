package main

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// typstFromMarkdown walks the same AST the HTML renderer uses and emits Typst
// markup. Raw HTML is dropped, since Typst has no equivalent and passing it
// through takes the whole PDF down over an inline <kbd>.
func typstFromMarkdown(md string) string {
	source := []byte(md)
	doc := markdownRenderer.Parser().Parse(text.NewReader(source))

	var b strings.Builder
	b.Grow(len(md) * 2)
	writeBlocks(&b, doc, source)
	return b.String()
}

func writeBlocks(b *strings.Builder, node ast.Node, source []byte) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		writeBlock(b, child, source)
	}
}

func writeBlock(b *strings.Builder, node ast.Node, source []byte) {
	switch n := node.(type) {
	case *ast.Paragraph:
		if img, ok := onlyImage(n); ok {
			writeBlockImage(b, img, source)
			return
		}
		writeInlines(b, n, source)
		b.WriteString("\n\n")

	case *ast.Heading:
		b.WriteString(strings.Repeat("=", n.Level))
		b.WriteByte(' ')
		writeInlines(b, n, source)
		b.WriteString("\n\n")

	case *ast.List:
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			writeListItem(b, item, source, n.IsOrdered())
		}
		b.WriteByte('\n')

	case *ast.Blockquote:
		b.WriteString("#quote(block: true)[\n")
		writeBlocks(b, n, source)
		b.WriteString("]\n\n")

	case *ast.ThematicBreak:
		b.WriteString("#align(center)[#line(length: 30%, stroke: 0.5pt + gray)]\n\n")

	case *ast.FencedCodeBlock:
		lang := ""
		if n.Info != nil {
			lang = string(firstWord(n.Info.Segment.Value(source)))
		}
		writeCodeBlock(b, blockText(n, source), lang)

	case *ast.CodeBlock:
		writeCodeBlock(b, blockText(n, source), "")

	case *ast.HTMLBlock:
		// Dropped, see typstFromMarkdown.

	case *east.Table:
		writeTable(b, n, source)

	default:
		writeBlocks(b, node, source)
	}
}

func writeCodeBlock(b *strings.Builder, code, lang string) {
	b.WriteString("#raw(block: true")
	if lang != "" {
		fmt.Fprintf(b, ", lang: %q", lang)
	}
	fmt.Fprintf(b, ", %q)\n\n", code)
}

func writeListItem(b *strings.Builder, item ast.Node, source []byte, ordered bool) {
	if ordered {
		b.WriteString("+ ")
	} else {
		b.WriteString("- ")
	}
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *ast.TextBlock, *ast.Paragraph:
			writeInlines(b, child, source)
		case *ast.List:
			// Typst nests by indentation, so a sublist indents each marker.
			b.WriteByte('\n')
			for sub := c.FirstChild(); sub != nil; sub = sub.NextSibling() {
				b.WriteString("  ")
				writeListItem(b, sub, source, c.IsOrdered())
			}
		default:
			writeBlock(b, child, source)
		}
	}
	b.WriteByte('\n')
}

func writeTable(b *strings.Builder, table *east.Table, source []byte) {
	columns := len(table.Alignments)
	if columns == 0 {
		return
	}

	fmt.Fprintf(b, "#table(columns: %d, align: (", columns)
	for i, alignment := range table.Alignments {
		if i > 0 {
			b.WriteString(", ")
		}
		switch alignment {
		case east.AlignRight:
			b.WriteString("right")
		case east.AlignCenter:
			b.WriteString("center")
		default:
			b.WriteString("left")
		}
	}
	b.WriteString("),\n")

	// Body rows are wrapped in a TableBody in some documents and are direct
	// children in others, so both are walked the same way.
	var writeRows func(node ast.Node)
	writeRows = func(node ast.Node) {
		for row := node.FirstChild(); row != nil; row = row.NextSibling() {
			switch row.(type) {
			case *east.TableHeader, *east.TableRow:
				for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
					b.WriteString("  [")
					writeInlines(b, cell, source)
					b.WriteString("],\n")
				}
			default:
				writeRows(row)
			}
		}
	}
	writeRows(table)

	b.WriteString(")\n\n")
}

func writeInlines(b *strings.Builder, node ast.Node, source []byte) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		writeInline(b, child, source)
	}
}

func writeInline(b *strings.Builder, node ast.Node, source []byte) {
	switch n := node.(type) {
	case *ast.Text:
		b.WriteString(escapeMarkup(textValue(n, source)))
		switch {
		case n.HardLineBreak():
			b.WriteString(" \\\n")
		case n.SoftLineBreak():
			b.WriteByte(' ')
		}

	case *ast.String:
		b.WriteString(escapeMarkup(plainText(n.Value)))

	case *ast.CodeSpan:
		fmt.Fprintf(b, "#raw(%q)", inlineText(n, source))

	case *ast.Emphasis:
		if n.Level == 2 {
			wrapInline(b, n, source, "#strong[", "]")
		} else {
			wrapInline(b, n, source, "#emph[", "]")
		}

	case *east.Strikethrough:
		wrapInline(b, n, source, "#strike[", "]")

	case *ast.Link:
		fmt.Fprintf(b, "#link(%q)[", string(n.Destination))
		writeInlines(b, n, source)
		b.WriteByte(']')

	case *ast.AutoLink:
		url := string(n.URL(source))
		fmt.Fprintf(b, "#link(%q)", url)

	case *ast.Image:
		fmt.Fprintf(b, "#image(%q)", imagePath(string(n.Destination)))

	case *ast.RawHTML:
		// Dropped, see typstFromMarkdown.

	default:
		writeInlines(b, node, source)
	}
}

func wrapInline(b *strings.Builder, node ast.Node, source []byte, open, close string) {
	b.WriteString(open)
	writeInlines(b, node, source)
	b.WriteString(close)
}

func writeBlockImage(b *strings.Builder, img *ast.Image, source []byte) {
	fmt.Fprintf(b, "#align(center)[#image(%q, width: 100%%", imagePath(string(img.Destination)))
	if alt := inlineText(img, source); alt != "" {
		fmt.Fprintf(b, ", alt: %q", alt)
	}
	b.WriteString(")]\n\n")
}

func onlyImage(para *ast.Paragraph) (*ast.Image, bool) {
	first := para.FirstChild()
	if first == nil || first.NextSibling() != nil {
		return nil, false
	}
	img, ok := first.(*ast.Image)
	return img, ok
}

// imagePath resolves a post's relative image reference against the Typst root,
// and has to stay idempotent, since this walks the AST from
// markdownRenderer.Parser() whose transformer may have rewritten it already.
func imagePath(url string) string {
	if strings.HasPrefix(url, "/") || strings.Contains(url, "://") {
		return url
	}
	return "/content/images/" + strings.TrimPrefix(url, "images/")
}

// inlineText flattens a node to the plain text Typst wants for alt text.
func inlineText(node ast.Node, source []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			switch c := child.(type) {
			case *ast.Text:
				b.WriteString(textValue(c, source))
			case *ast.String:
				if c.IsRaw() {
					b.Write(c.Value)
				} else {
					b.WriteString(plainText(c.Value))
				}
			default:
				walk(child)
			}
		}
	}
	walk(node)
	return b.String()
}

// textValue reads a text node the way goldmark's own HTML renderer does. A node
// inside a code span is raw and means the exact source bytes, so a post writing
// `&#x2f;` in backticks keeps the entity. Everywhere else they are resolved.
func textValue(n *ast.Text, source []byte) string {
	raw := n.Segment.Value(source)
	if n.IsRaw() {
		return string(raw)
	}
	return plainText(raw)
}

// plainText resolves the Markdown escapes and entities goldmark leaves in the
// AST for a renderer to handle. Without it the Typst escaper escapes the
// leftover backslash and the PDF reads "reverse\_proxy".
func plainText(raw []byte) string {
	resolved := util.ResolveEntityNames(util.ResolveNumericReferences(raw))
	return string(util.UnescapePunctuations(resolved))
}

// markupEscaper covers the characters that mean something in Typst markup, so a
// post mentioning #hashtags or an @handle does not compile into a reference.
var markupEscaper = strings.NewReplacer(
	`\`, `\\`, `[`, `\[`, `]`, `\]`, `*`, `\*`, `_`, `\_`,
	"`", "\\`", `#`, `\#`, `$`, `\$`, `<`, `\<`, `@`, `\@`, `~`, `\~`,
)

func escapeMarkup(s string) string { return markupEscaper.Replace(s) }

func blockText(node ast.Node, source []byte) string {
	var b strings.Builder
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		b.Write(line.Value(source))
	}
	return b.String()
}

// typstSource wraps a post's body in the blog_post.typ template call. Typst
// string literals take the same escapes %q produces.
func typstSource(post *Post) string {
	var b strings.Builder
	b.WriteString("#import \"/typst/blog_post.typ\": render\n#render(\n")
	fmt.Fprintf(&b, "  title: %q,\n", post.Title)
	fmt.Fprintf(&b, "  date: %q,\n", post.Date)
	fmt.Fprintf(&b, "  read_time: %d,\n", post.ReadTime)

	b.WriteString("  tags: (")
	for i, tag := range post.Tags {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", tag)
	}
	// A one-element Typst array needs the trailing comma or it is just a
	// parenthesised string, and `for tag in tags` iterates its characters.
	if len(post.Tags) == 1 {
		b.WriteByte(',')
	}
	b.WriteString("),\n")

	fmt.Fprintf(&b, "  description: %q,\n", post.Description)
	if post.CoverImage == "" {
		b.WriteString("  cover_image: none,\n")
	} else {
		fmt.Fprintf(&b, "  cover_image: %q,\n", "/content/images/"+post.CoverImage)
	}

	b.WriteString("  body: [\n")
	b.WriteString(post.BodyTypst)
	b.WriteString("\n  ],\n)\n")
	return b.String()
}
