package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The property list: create, delete, pin cards, toggle public. Everything here
// is behind requireAuth.

// PropertyRow is one row of the list, with its counts already gathered.
type PropertyRow struct {
	ID                 string
	Name               string
	IsProtected        bool
	IsPublic           bool
	IsActive           bool
	TotalEvents        int64
	TotalPageViews     int64
	TotalSessionStarts int64
}

// PropertyTotals is the summary strip above the list.
type PropertyTotals struct {
	Properties int
	Events     int64
	PageViews  int64
	Sessions   int64
}

// activeWindow is how recently a property must have seen an event to show as
// live in the list. A week, because a personal site can legitimately go quiet
// for a few days and should not read as broken.
const activeWindow = 7 * 24 * time.Hour

func (s *site) properties(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	search := strings.TrimSpace(r.URL.Query().Get("q"))

	query := "SELECT " + propertyColumns + " FROM properties"
	var args []any
	if search != "" {
		query += " WHERE name LIKE ?"
		args = append(args, "%"+search+"%")
	}
	// Proprium first, then oldest first. The app's own property is the one
	// most often wanted and would otherwise sit wherever its creation date put
	// it.
	query += " ORDER BY is_protected DESC, created_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		slog.Info(fmt.Sprintf("properties list: %v", err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var props []*Property
	for rows.Next() {
		p, err := scanProperty(rows.Scan)
		if err != nil {
			slog.Info(fmt.Sprintf("properties scan: %v", err))
			continue
		}
		props = append(props, p)
	}

	listed := make([]PropertyRow, 0, len(props))
	var totals PropertyTotals
	activeSince := time.Now().Add(-activeWindow).UnixMilli()

	for _, p := range props {
		var total, pv, ss, active int64
		// One statement with four correlated subqueries rather than four
		// round trips per property. Still N+1 across the list, which is fine
		// at the scale this runs at and would not be at a thousand properties.
		err := s.db.QueryRowContext(ctx, `SELECT
		    (SELECT COUNT(*) FROM events WHERE property_id = ?1),
		    (SELECT COUNT(*) FROM events WHERE property_id = ?1 AND event = 'page_view'),
		    (SELECT COUNT(*) FROM events WHERE property_id = ?1 AND event = 'session_start'),
		    (SELECT COUNT(*) FROM events WHERE property_id = ?1 AND created_at >= ?2)`,
			p.ID[:], activeSince).Scan(&total, &pv, &ss, &active)
		if err != nil {
			slog.Info(fmt.Sprintf("properties counts for %s: %v", p.ID, err))
		}

		totals.Events += total
		totals.PageViews += pv
		totals.Sessions += ss

		listed = append(listed, PropertyRow{
			ID:                 p.ID.String(),
			Name:               p.Name,
			IsProtected:        p.IsProtected,
			IsPublic:           p.IsPublic,
			IsActive:           active > 0,
			TotalEvents:        total,
			TotalPageViews:     pv,
			TotalSessionStarts: ss,
		})
	}
	totals.Properties = len(listed)

	data := s.page(r, "Properties", "Manage your properties.")
	data.Properties = listed
	data.Totals = totals
	data.Query = search
	s.renderer.Render(w, http.StatusOK, "properties.html", data)
}

func (s *site) propertyCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}

	id := uuid.New()
	now := time.Now().UnixMilli()
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO properties (id, name, custom_cards, is_protected, is_public, created_at, updated_at)
		 VALUES (?, ?, '[]', 0, 0, ?, ?)`,
		id[:], name, now, now); err != nil {
		slog.Info(fmt.Sprintf("property create: %v", err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/properties", http.StatusSeeOther)
}

// propertyDelete removes a property and, by ON DELETE CASCADE, every event
// recorded against it.
//
// The is_protected guard in the WHERE clause is what stops Proprium being
// deleted. It is enforced here rather than in the template that hides the
// button, because a hidden button is not a permission.
func (s *site) propertyDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := s.db.ExecContext(r.Context(),
		"DELETE FROM properties WHERE id = ? AND is_protected = 0", id[:]); err != nil {
		slog.Info(fmt.Sprintf("property delete: %v", err))
	}
	http.Redirect(w, r, "/properties", http.StatusSeeOther)
}

// propertyCards stores which custom events are pinned as dashboard tiles.
//
// The body is re-encoded rather than stored as received, so what lands in the
// column is known-good JSON of a known shape. Storing the raw body would let a
// malformed array become a value every later read has to defend against.
func (s *site) propertyCards(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	var cards []CustomCard
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&cards); err != nil {
		cards = []CustomCard{}
	}
	encoded, err := json.Marshal(cards)
	if err != nil {
		encoded = []byte("[]")
	}

	if _, err := s.db.ExecContext(r.Context(),
		"UPDATE properties SET custom_cards = ?, updated_at = ? WHERE id = ?",
		string(encoded), time.Now().UnixMilli(), id[:]); err != nil {
		slog.Info(fmt.Sprintf("property cards: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// propertyPublic flips whether a dashboard is readable without logging in.
func (s *site) propertyPublic(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := s.db.ExecContext(r.Context(),
		"UPDATE properties SET is_public = 1 - is_public, updated_at = ? WHERE id = ?",
		time.Now().UnixMilli(), id[:]); err != nil {
		slog.Info(fmt.Sprintf("property public toggle: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func parseIDPath(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	return id, err == nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
