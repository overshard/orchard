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

// Report export: the same dashboard data, rendered as Typst markup and
// compiled to PDF, or as Markdown and served directly.

// Report templates are text, so they are parsed with text/template.
// html/template would escape a Typst "#" or a Markdown "*" into an HTML entity
// and produce a document full of &amp;. Escaping still happens, through typstMD
// and typstStr, which know what is dangerous in Typst rather than in HTML.
//
//go:embed reports
var reportFS embed.FS

var reportFuncs = texttemplate.FuncMap{
	"typstMD":  typstMD,
	"typstStr": typstStr,
	"pct":      pct,
	"add":      func(a, b int) int { return a + b },
}

var reportTemplates = texttemplate.Must(
	texttemplate.New("reports").Funcs(reportFuncs).ParseFS(reportFS, "reports/*"))

// typstRoot is the directory Typst resolves absolute paths against, and the
// boundary of what a compile is allowed to read.
func typstRoot() string { return dir("SITE_ROOT", ".") }

func (s *site) renderReport(w http.ResponseWriter, r *http.Request, format, propertyName string, data PageData) {
	// The PDF path renders Typst source, so its template is report.typ.
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

	filename := asciiFilename(propertyName)

	if format == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `inline; filename="`+filename+`.md"`)
		_, _ = buf.WriteTo(w)
		return
	}

	pdf, err := s.typst.Render(r.Context(), typstRoot(), buf.String())
	if err != nil {
		// A missing binary is the normal state of a dev checkout, so it
		// reports as unavailable rather than as a fault.
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

// asciiFilename reduces a property name to something a Content-Disposition
// header can carry literally. A non-ASCII byte cannot go into a header value
// unencoded, and Go will send a header the client silently ignores rather than
// complaining.
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
	// A leading or trailing dot makes a hidden or extensionless file on some
	// systems, and a name of pure punctuation is no name at all.
	out := strings.Trim(strings.TrimSpace(b.String()), ".")
	if out == "" {
		return "report"
	}
	return out
}
