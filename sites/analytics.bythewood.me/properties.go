package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

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
	Views14            []float64
	ViewsWeek          int64
}

// PropertyTotals is the summary strip above the list.
type PropertyTotals struct {
	Properties int
	Events     int64
	PageViews  int64
	Sessions   int64
	Live       int
	Views14    []float64
	ViewsWeek  int64
	ViewsPrev  int64
}

// activeWindow is how recently a property must have seen an event to show as
// live. A week, since a personal site can go quiet for days without being broken.
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
	// Protected first, so this site's own property leads the list.
	query += " ORDER BY is_protected DESC, created_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		slog.Info("properties list", slog.Any("err", err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var props []*Property
	for rows.Next() {
		p, err := scanProperty(rows.Scan)
		if err != nil {
			slog.Info("properties scan", slog.Any("err", err))
			continue
		}
		props = append(props, p)
	}
	if err := rows.Err(); err != nil {
		slog.Info("properties list", slog.Any("err", err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	listed := make([]PropertyRow, 0, len(props))
	var totals PropertyTotals
	activeSince := time.Now().Add(-activeWindow).UnixMilli()

	for _, p := range props {
		var total, pv, ss, active int64
		// One round trip for all four counts, still N+1 across the list, which
		// is fine at this scale.
		err := s.db.QueryRowContext(ctx, `SELECT
		    (SELECT COUNT(*) FROM events WHERE property_id = ?1),
		    (SELECT COUNT(*) FROM events WHERE property_id = ?1 AND event = 'page_view'),
		    (SELECT COUNT(*) FROM events WHERE property_id = ?1 AND event = 'session_start'),
		    (SELECT COUNT(*) FROM events WHERE property_id = ?1 AND created_at >= ?2)`,
			p.ID[:], activeSince).Scan(&total, &pv, &ss, &active)
		if err != nil {
			slog.Info("properties counts", slog.Any("id", p.ID), slog.Any("err", err))
		}

		totals.Events += total
		totals.PageViews += pv
		totals.Sessions += ss
		views := s.dailyViews(ctx, p.ID[:], 14)
		var week int64
		for i, v := range views {
			if totals.Views14 == nil {
				totals.Views14 = make([]float64, len(views))
			}
			totals.Views14[i] += v
			if i >= 7 {
				week += int64(v)
			} else {
				totals.ViewsPrev += int64(v)
			}
		}
		totals.ViewsWeek += week
		if active > 0 {
			totals.Live++
		}

		listed = append(listed, PropertyRow{
			ID:                 p.ID.String(),
			Name:               p.Name,
			IsProtected:        p.IsProtected,
			IsPublic:           p.IsPublic,
			IsActive:           active > 0,
			TotalEvents:        total,
			TotalPageViews:     pv,
			TotalSessionStarts: ss,
			Views14:            views,
			ViewsWeek:          week,
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
		slog.Info("property create", slog.Any("err", err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/properties", http.StatusSeeOther)
}

// propertyDelete removes a property and, by ON DELETE CASCADE, its events. The
// is_protected guard is in the WHERE clause because a hidden button in the
// template is not a permission.
func (s *site) propertyDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := s.db.ExecContext(r.Context(),
		"DELETE FROM properties WHERE id = ? AND is_protected = 0", id[:]); err != nil {
		slog.Info("property delete", slog.Any("err", err))
	}
	http.Redirect(w, r, "/properties", http.StatusSeeOther)
}

// propertyCards stores which custom events are pinned as dashboard tiles. The
// body is re-encoded, so the column always holds JSON of a known shape.
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
		slog.Info("property cards", slog.Any("err", err))
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
		slog.Info("property public toggle", slog.Any("err", err))
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

// dailyViews is a property's page views a day for the last n days, oldest
// first, for the trend beside it in the list.
func (s *site) dailyViews(ctx context.Context, id []byte, n int) []float64 {
	out := make([]float64, n)
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(n - 1))
	rows, err := s.db.QueryContext(ctx, `SELECT (created_at - ?2) / 86400000, COUNT(*) FROM events
		WHERE property_id = ?1 AND event = 'page_view' AND created_at >= ?2 GROUP BY 1`, id, start.UnixMilli())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var day, count int64
		if rows.Scan(&day, &count) == nil && day >= 0 && day < int64(n) {
			out[day] = float64(count)
		}
	}
	return out
}
