// The read only JSON view of the repositories, for chat.bythewood.me's tools.
//
// The index page already assembles this, so this handler builds the same cards
// and hands them over as data. Hidden repositories are included, unlike on the
// index, because this is behind the session and hiding one is about the public
// listing rather than about secrecy.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

type apiRepo struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Mirror      bool      `json:"mirror"`
	Hidden      bool      `json:"hidden"`
	Empty       bool      `json:"empty"`
	SizeBytes   int64     `json:"size_bytes"`
	Branches    int       `json:"branches"`
	Tags        int       `json:"tags"`
	LastPush    time.Time `json:"last_push"`
	PushPercent int       `json:"push_percent_of_limit"`
}

func (s *site) apiRepos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	repos, err := s.store.Discover()
	if err != nil {
		slog.Error("discover repos", slog.Any("err", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	meta, err := s.db.AllRepos()
	if err != nil {
		slog.Error("read repo metadata", slog.Any("err", err))
		meta = map[string]RepoMeta{}
	}

	out := make([]apiRepo, 0, len(repos))
	var total int64
	for _, repo := range repos {
		m := meta[repo.Name]
		o := s.store.Overview(ctx, repo)
		total += o.Size
		out = append(out, apiRepo{
			Name:        repo.Name,
			Description: m.Description,
			Mirror:      m.Mirror,
			Hidden:      m.Hidden,
			Empty:       o.Empty,
			SizeBytes:   o.Size,
			Branches:    o.Branches,
			Tags:        o.Tags,
			LastPush:    o.LastPush,
			PushPercent: percentOf(o.Size, cloudflareBodyLimit),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastPush.After(out[j].LastPush) })

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"repos":            out,
		"count":            len(out),
		"total_size_bytes": total,
	})
}
