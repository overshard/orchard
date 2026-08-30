package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

func (s *site) properties(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	rows, err := listProperties(r.Context(), s.db, query)
	if err != nil {
		slog.Info(fmt.Sprintf("properties list: %v", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	data := s.page(r, "Properties", "Manage your properties.")
	data.Query = query

	for _, p := range rows {
		view, err := s.buildPropertyView(r.Context(), p)
		if err != nil {
			// One property failing to summarise must not blank the list.
			slog.Info(fmt.Sprintf("properties list: building view for %s: %v", p.URL, err))
			continue
		}
		data.Properties = append(data.Properties, view)
	}

	data.PageScript = s.propsScript
	data.PageStyles = s.propsStyles
	s.renderer.Render(w, http.StatusOK, "properties.html", data)
}

// propertyCreate adds a tracked URL. https:// is required: the checker speaks
// HTTP/2 over TLS and nothing else, so an http:// property could never pass.
func (s *site) propertyCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	raw := strings.TrimSpace(r.PostFormValue("url"))
	if raw == "" || !strings.HasPrefix(strings.ToLower(raw), "https://") {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}
	// Parsed as well as prefix-checked, so "https://" on its own is refused.
	if _, err := parseHTTPURL(raw); err != nil {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}

	if _, err := createProperty(r.Context(), s.db, raw); err != nil {
		slog.Info(fmt.Sprintf("property create %s: %v", raw, err))
	}
	http.Redirect(w, r, "/properties", http.StatusSeeOther)
}

func (s *site) propertyDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}
	if err := deleteProperty(r.Context(), s.db, id); err != nil {
		slog.Info(fmt.Sprintf("property delete %s: %v", id, err))
	}
	http.Redirect(w, r, "/properties", http.StatusSeeOther)
}

// propertyPublic flips whether a property has a shareable status page. Answers
// JSON because the toggle is a fetch() that updates the switch in place.
func (s *site) propertyPublic(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "not_found"})
		return
	}

	isPublic, found, err := togglePublic(r.Context(), s.db, id)
	switch {
	case err != nil:
		slog.Info(fmt.Sprintf("property public toggle %s: %v", id, err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false})
	case !found:
		// A 404 rather than a success carrying a made-up value.
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "not_found"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "is_public": isPublic})
	}
}
