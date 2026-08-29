package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

// Renderer parses a base layout plus one template per page, and renders into a
// buffer before writing.
//
// Buffering is what makes a mid-render template error recoverable. Writing
// straight to the ResponseWriter would send the 200 and the first half of the
// page first, producing a broken document that looks like a success.
type Renderer struct {
	pages map[string]*template.Template
}

// NewRenderer parses every page template against the given base layouts.
// Layouts go into each page's own set, so a page can both {{define}} blocks and
// {{template}} the shared chrome.
func NewRenderer(files fs.FS, funcs template.FuncMap, layouts []string, pages []string) (*Renderer, error) {
	r := &Renderer{pages: make(map[string]*template.Template, len(pages))}

	for _, page := range pages {
		patterns := append(append([]string{}, layouts...), page)
		t, err := template.New("base.html").Funcs(funcs).ParseFS(files, patterns...)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", page, err)
		}
		r.pages[page] = t
	}

	return r, nil
}

// Render writes a page, or a 500 rather than a half-written body.
func (r *Renderer) Render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := r.pages[page]
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		panic("render: no such template " + page)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		panic(fmt.Sprintf("render %s: %v", page, err))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
