package main

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
)

// Only ponds that played a game this week are hopped to. A new pond's
// played_at is its creation, so an egg a crawler laid never counts.
const hopWithin = 7 * 86400

// hopTo picks a random active pond that's neither of the two given.
func (s *store) hopTo(mine, from string) (string, error) {
	now := s.now().Unix()
	var code string
	err := s.r.QueryRow(`SELECT public_id FROM ponds
		WHERE played_at >= ? AND played_at > created_at + 60 AND public_id NOT IN (?, ?)
		ORDER BY RANDOM() LIMIT 1`, now-hopWithin, mine, from).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNoPond
	}
	return code, err
}

// hop sends the visitor to another frog's page. The slug's name part is
// left for the share page's own redirect to fill in.
func (s *site) hop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	mine := ""
	if id, _, err := s.pondOf(r); err == nil {
		_ = s.store.r.QueryRow(`SELECT public_id FROM ponds WHERE id = ?`, id).Scan(&mine)
	}
	from, _ := publicIDOf(r.URL.Query().Get("from"))
	code, err := s.store.hopTo(mine, from)
	if errors.Is(err, errNoPond) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if err != nil {
		slog.Error("pond", slog.String("path", r.URL.Path), slog.Any("err", err))
		http.Error(w, "something went wrong in the pond", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/f/frog-"+code, http.StatusFound)
}
