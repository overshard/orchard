package main

import (
	"bytes"
	"html/template"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// The Markdown pipeline: tables, strikethrough, and raw HTML passed through.
// Autolinks and heading anchors stay off, because turning them on would
// silently rewrite twenty-two existing posts.
var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough),
	goldmark.WithRendererOptions(
		html.WithUnsafe(),
		renderer.WithNodeRenderers(util.Prioritized(&codeRenderer{}, 100)),
	),
)

func renderMarkdown(md string) template.HTML {
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(md), &buf); err != nil {
		// Convert only fails on a writer error, and this writer is a buffer.
		panic("render markdown: " + err.Error())
	}
	return template.HTML(buf.String())
}

// base16OceanDark is the theme every existing code block was written under, so
// it is spelled out here rather than substituted: chroma does not bundle this
// one, and an approximation is a visible change to every post.
//
// Token assignments are Base16 Ocean's documented roles: base08 variables and
// tags, base09 constants and numbers, base0A classes, base0B strings, base0C
// escapes and builtins, base0D functions, base0E keywords, base0F
// deprecated.
var base16OceanDark = chroma.MustNewStyle("base16-ocean-dark", chroma.StyleEntries{
	chroma.Background:            "#c0c5ce bg:#2b303b",
	chroma.Comment:               "#65737e",
	chroma.CommentHashbang:       "#65737e",
	chroma.CommentMultiline:      "#65737e",
	chroma.CommentSingle:         "#65737e",
	chroma.CommentSpecial:        "#65737e",
	chroma.CommentPreproc:        "#b48ead",
	chroma.Keyword:               "#b48ead",
	chroma.KeywordConstant:       "#d08770",
	chroma.KeywordDeclaration:    "#b48ead",
	chroma.KeywordNamespace:      "#b48ead",
	chroma.KeywordPseudo:         "#b48ead",
	chroma.KeywordReserved:       "#b48ead",
	chroma.KeywordType:           "#ebcb8b",
	chroma.Operator:              "#c0c5ce",
	chroma.OperatorWord:          "#b48ead",
	chroma.Punctuation:           "#c0c5ce",
	chroma.Name:                  "#c0c5ce",
	chroma.NameAttribute:         "#d08770",
	chroma.NameBuiltin:           "#96b5b4",
	chroma.NameBuiltinPseudo:     "#96b5b4",
	chroma.NameClass:             "#ebcb8b",
	chroma.NameConstant:          "#d08770",
	chroma.NameDecorator:         "#96b5b4",
	chroma.NameEntity:            "#96b5b4",
	chroma.NameException:         "#bf616a",
	chroma.NameFunction:          "#8fa1b3",
	chroma.NameLabel:             "#bf616a",
	chroma.NameNamespace:         "#ebcb8b",
	chroma.NameTag:               "#bf616a",
	chroma.NameVariable:          "#bf616a",
	chroma.LiteralString:         "#a3be8c",
	chroma.LiteralStringChar:     "#a3be8c",
	chroma.LiteralStringDoc:      "#a3be8c",
	chroma.LiteralStringEscape:   "#96b5b4",
	chroma.LiteralStringInterpol: "#96b5b4",
	chroma.LiteralStringRegex:    "#96b5b4",
	chroma.LiteralStringSymbol:   "#a3be8c",
	chroma.LiteralNumber:         "#d08770",
	chroma.GenericDeleted:        "#bf616a",
	chroma.GenericEmph:           "italic #b48ead",
	chroma.GenericHeading:        "bold #8fa1b3",
	chroma.GenericInserted:       "#a3be8c",
	chroma.GenericStrong:         "bold #ebcb8b",
	chroma.GenericSubheading:     "bold #8fa1b3",
	chroma.Error:                 "#ab7967",
})

// chromaFormatter emits the highlighted spans only. The <pre> and <code>
// wrapper is written by hand below, because post.scss styles `article > pre`
// as a direct child and chroma's own wrapper div would break that selector on
// every code block.
var chromaFormatter = chromahtml.New(
	chromahtml.WithClasses(false),
	chromahtml.PreventSurroundingPre(true),
)

// codeRenderer replaces goldmark's code block rendering: a <pre> carrying the
// theme background as an inline style, wrapping a <code class="language-x">.
type codeRenderer struct{}

func (r *codeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.render)
	reg.Register(ast.KindCodeBlock, r.render)
}

func (r *codeRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	var lang string
	if fenced, ok := node.(*ast.FencedCodeBlock); ok && fenced.Info != nil {
		// The info string can carry more than the language ("go title=x"), and
		// only the first word names a lexer.
		lang = string(firstWord(fenced.Info.Segment.Value(source)))
	}

	var code bytes.Buffer
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		code.Write(line.Value(source))
	}

	// The colour is on the <pre> rather than left to the stylesheet. chroma
	// only wraps tokens it has a rule for, so unstyled code (identifiers,
	// punctuation, whitespace) would inherit article's warm #c4bdb2 against
	// this cool background.
	_, _ = w.WriteString(`<pre style="background-color:#2b303b;color:#c0c5ce;"><code`)
	if lang != "" {
		_, _ = w.WriteString(` class="language-`)
		template.HTMLEscape(w, []byte(lang))
		_, _ = w.WriteString(`"`)
	}
	_, _ = w.WriteString(`>`)

	if err := highlight(w, code.String(), lang); err != nil {
		// An unhighlightable block is still a readable block, so fall back to
		// the escaped source rather than failing the whole page.
		template.HTMLEscape(w, code.Bytes())
	}

	_, _ = w.WriteString("</code></pre>\n")
	return ast.WalkSkipChildren, nil
}

func highlight(w util.BufWriter, code, lang string) error {
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	// Coalesce merges runs of same-type tokens, which keeps a plain-text block
	// from becoming one <span> per character.
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return err
	}
	return chromaFormatter.Format(w, base16OceanDark, iterator)
}

func firstWord(b []byte) []byte {
	if i := bytes.IndexAny(b, " \t"); i >= 0 {
		return b[:i]
	}
	return b
}
