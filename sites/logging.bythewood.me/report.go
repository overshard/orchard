package main

import (
	"bytes"
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	texttemplate "text/template"
)

// Parsed with text/template: html/template would escape a Typst "#" or a
// Markdown "*" into an HTML entity. Escaping happens through typstMD and
// typstStr instead.
//
//go:embed reports
var reportFS embed.FS

var reportFuncs = texttemplate.FuncMap{
	"typstMD":  typstMD,
	"typstStr": typstStr,
	"pct":      pct,
	"add":      func(a, b int) int { return a + b },
	// The formatters the HTML pages use, so a number reads the same on screen
	// and in the PDF.
	"ms":  formatMS,
	"num": formatNum,
	"md":  markdownCell,
}

// markdownCell escapes a value for a Markdown table cell. path is attacker
// controlled and Go percent-decodes it, so an unescaped newline and pipe forge
// table rows; HTML characters go too, since Markdown passes raw HTML through.
func markdownCell(v any) string {
	s := fmt.Sprint(v)
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)

	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\', '|', '`', '*', '_', '[', ']', '<', '>', '&', '#', '~':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

var reportTemplates = texttemplate.Must(
	texttemplate.New("reports").Funcs(reportFuncs).ParseFS(reportFS, "reports/*"))

// typstRoot bounds what a compile is allowed to read.
func typstRoot() string { return dir("SITE_ROOT", ".") }

func (s *site) renderReport(w http.ResponseWriter, r *http.Request, format, subject string, data PageData) {
	name := "report.typ"
	if format == "md" {
		name = "report.md"
	}

	var buf bytes.Buffer
	if err := reportTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		slog.Info(fmt.Sprintf("render %s: %v", name, err))
		http.Error(w, "report error", http.StatusInternalServerError)
		return
	}

	filename := asciiFilename(subject)

	if format == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `inline; filename="`+filename+`.md"`)
		_, _ = buf.WriteTo(w)
		return
	}

	pdf, err := s.typst.Render(r.Context(), typstRoot(), buf.String())
	if err != nil {
		// A missing binary is the normal state of a dev checkout.
		if err == ErrTypstMissing {
			slog.Info(fmt.Sprintf("pdf report: %v (install typst, or use ?report=md)", err))
			http.Error(w, "pdf export unavailable", http.StatusServiceUnavailable)
			return
		}
		slog.Info(fmt.Sprintf("pdf report: %v", err))
		http.Error(w, "report error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+filename+`.pdf"`)
	_, _ = w.Write(pdf)
}

// asciiFilename reduces a subject to what a Content-Disposition value can carry
// literally; Go sends a header with a non-ASCII byte rather than complaining.
func asciiFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	// A leading or trailing dot makes a hidden or extensionless file.
	out := strings.Trim(strings.TrimSpace(b.String()), ".")
	if out == "" {
		return "report"
	}
	return out
}
