package main

import (
	"encoding/json"
	"net/http"
)

// hostCheck is the security panel's half that says what the VPS runs, which a
// public page would be handing out, so it answers only a signed in browser.
func (s *site) hostCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.auth.Authenticated(r) {
		http.Error(w, "not signed in", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(s.store.Snapshot().Advisories.Host)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
