package main

import (
	"bytes"
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	texttemplate "text/template"
	"time"
)

// Report templates are text: html/template would escape a Typst "#" or a
// Markdown "*" into an entity. typstMD and typstStr do the escaping instead.
//
//go:embed reports
var reportFS embed.FS

var reportFuncs = texttemplate.FuncMap{
	"typstMD":     typstMD,
	"typstStr":    typstStr,
	"upper":       strings.ToUpper,
	"naturaltime": naturalTime,
	"num":         formatNum,
	"msSavings":   msSavings,
	"urlPath":     urlPath,
	"pct":         pct,
	"pct1":        pct1,
	"add":         func(a, b int) int { return a + b },
}

var reportTemplates = texttemplate.Must(
	texttemplate.New("reports").Funcs(reportFuncs).ParseFS(reportFS, "reports/*"))

// typstRoot is the directory Typst resolves absolute paths against, bounding
// what a compile is allowed to read.
func typstRoot() string { return dir("SITE_ROOT", ".") }

func (s *site) renderReport(w http.ResponseWriter, r *http.Request, format, propertyName string, data PageData) {
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

	filename := asciiFilename(propertyName) + "-" + time.Now().Format("2006-01-02")

	if format == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `inline; filename="`+filename+`.md"`)
		_, _ = buf.WriteTo(w)
		return
	}

	pdf, err := s.typst.Render(r.Context(), typstRoot(), buf.String())
	if err != nil {
		// A missing binary is the normal state of a dev checkout, not a fault.
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

// asciiFilename reduces a property name to bytes a Content-Disposition value can
// carry literally; Go will send a non-ASCII one the client silently ignores.
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
