package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type rebuildReply struct {
	SignedIn bool   `json:"signed_in"`
	Error    string `json:"error,omitempty"`
	*RebuildStatus
}

// rebuildState tells the page whether to offer the BRIEFS readout, and only a
// signed in caller hears anything past that.
func (s *site) rebuildState(w http.ResponseWriter, r *http.Request) {
	out := rebuildReply{SignedIn: s.auth.Authenticated(r)}
	if out.SignedIn {
		st := s.briefer.Status(time.Now())
		out.RebuildStatus = &st
	}
	writeRebuild(w, http.StatusOK, out)
}

// rebuildStart reruns every desk now. The fences are the quick chat's, since
// the cookie that proves who's asking rides along on any bythewood.me page.
func (s *site) rebuildStart(w http.ResponseWriter, r *http.Request) {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		quickError(w, http.StatusForbidden, "cross origin")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		quickError(w, http.StatusUnsupportedMediaType, "json only")
		return
	}
	if !s.auth.Authenticated(r) {
		quickError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	var in struct{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
		quickError(w, http.StatusBadRequest, "bad request")
		return
	}

	now := time.Now()
	err := s.briefer.Force(now)
	st := s.briefer.Status(now)
	out := rebuildReply{SignedIn: true, RebuildStatus: &st}
	switch {
	case err == nil:
		writeRebuild(w, http.StatusAccepted, out)
	case errors.Is(err, errBriefsBusy):
		out.Error = err.Error()
		writeRebuild(w, http.StatusConflict, out)
	case errors.Is(err, errBriefsCool):
		out.Error = err.Error()
		w.Header().Set("Retry-After", strconv.Itoa(st.ReadyIn))
		writeRebuild(w, http.StatusTooManyRequests, out)
	default:
		out.Error = err.Error()
		writeRebuild(w, http.StatusServiceUnavailable, out)
	}
}

func writeRebuild(w http.ResponseWriter, status int, out rebuildReply) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}
