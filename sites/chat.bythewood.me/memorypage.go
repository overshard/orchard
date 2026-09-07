// The memory overlay's endpoints.
//
// JSON rather than pages, because this lives in a panel over the conversation
// and navigating away from a thread to delete a fact is a worse trade than the
// handful of endpoints here.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type memoryView struct {
	Facts   []Fact      `json:"facts"`
	Note    string      `json:"note,omitempty"`
	Problem string      `json:"problem,omitempty"`
	Applied []memChange `json:"applied,omitempty"`
}

func (s *site) memoryList(w http.ResponseWriter, r *http.Request) {
	facts, err := s.store.Facts()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, memoryView{Facts: facts})
}

// memoryTeach is the write path, and the only one. What is typed here is not
// stored: it goes to the model with the same rules the automatic pass uses, and
// what comes back is what gets written.
func (s *site) memoryTeach(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Said string `json:"said"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
	said := strings.TrimSpace(body.Said)
	if said == "" {
		s.memoryProblem(w, "Say what should change.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	existing, err := s.store.Facts()
	if err != nil {
		s.memoryProblem(w, err.Error())
		return
	}
	changes, err := s.proposeChanges(ctx, existing,
		"Isaac is editing his memory himself and said:\n"+said,
		"Do what he asked, following the rules above.")
	if err != nil {
		s.memoryProblem(w, "the model did not answer: "+err.Error())
		return
	}
	applied := s.applyChanges(changes, existing)
	s.store.Checkpoint()

	facts, _ := s.store.Facts()
	if len(applied) == 0 {
		writeJSON(w, memoryView{Facts: facts,
			Problem: "Nothing changed. It read that as not worth keeping, or as already known."})
		return
	}
	var parts []string
	for _, c := range applied {
		switch c.Op {
		case "add":
			parts = append(parts, "remembered that")
		case "replace":
			parts = append(parts, "rewrote one")
		case "delete":
			parts = append(parts, "forgot one")
		}
	}
	writeJSON(w, memoryView{Facts: facts, Applied: applied, Note: strings.Join(parts, ", ") + "."})
}

func (s *site) memoryProblem(w http.ResponseWriter, msg string) {
	facts, _ := s.store.Facts()
	writeJSON(w, memoryView{Facts: facts, Problem: msg})
}

func (s *site) memoryDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.store.DeleteFact(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.store.Checkpoint()
	s.memoryList(w, r)
}

func (s *site) memoryForget(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ForgetEverything(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.store.Checkpoint()
	s.memoryList(w, r)
}
