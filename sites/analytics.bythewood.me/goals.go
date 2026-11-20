package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// goalEvents is the custom events a property counts as goals, the first marked
// first, since that one takes the tile.
func goalEvents(ctx context.Context, db *sql.DB, id uuid.UUID) []string {
	rows, err := db.QueryContext(ctx, `SELECT event FROM goals WHERE property_id = ? ORDER BY created_at, event`, id[:])
	logQuery("goals", err)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// propertyGoals replaces a property's goals with the boxes ticked on the form
// and goes back to the dashboard as it was.
func (s *site) propertyGoals(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	goals := r.PostForm["goal"]
	if len(goals) > 50 {
		goals = goals[:50]
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		slog.Info("property goals", slog.Any("err", err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	var keep []string
	for _, g := range goals {
		if g != "" && len(g) <= maxFilterLen {
			keep = append(keep, g)
		}
	}
	// Goals still ticked keep their row, so the first marked stays first.
	args := []any{id[:]}
	for _, g := range keep {
		args = append(args, g)
	}
	q := `DELETE FROM goals WHERE property_id = ?`
	if len(keep) > 0 {
		q += ` AND event NOT IN (` + placeholders(len(keep)) + `)`
	}
	_, err = tx.ExecContext(r.Context(), q, args...)
	now := time.Now().UnixMilli()
	for _, g := range keep {
		if err != nil {
			break
		}
		_, err = tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO goals (property_id, event, created_at)
			SELECT id, ?, ? FROM properties WHERE id = ?`, g, now, id[:])
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Info("property goals", slog.Any("err", err))
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	// Only the query string comes back from the form, so the redirect can't
	// leave this dashboard.
	back := "/" + id.String()
	if q, err := url.ParseQuery(r.PostFormValue("back")); err == nil && len(q) > 0 {
		back += "?" + q.Encode()
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
