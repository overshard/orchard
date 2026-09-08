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
	"strings"
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

// The tree and the file behind it, as data.
//
// chat.bythewood.me could list the repositories and read nothing inside them,
// so every question about Isaac's own code ended with the model guessing raw
// addresses, collecting 404s, and on one occasion writing a file it claimed to
// have read. These two are the same git plumbing the HTML pages use, returned
// as JSON so a tool can walk a repository the way a person does.

type apiEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // tree, blob or commit for a submodule
	Size int64  `json:"size,omitempty"`
}

// apiTree lists one directory. It does not recurse: a repository the size of
// orchard flattened into one response is most of a megabyte of paths, and the
// model only ever needs the level it is looking at.
func (s *site) apiTree(w http.ResponseWriter, r *http.Request) {
	repo, rev, ok := s.apiResolve(w, r)
	if !ok {
		return
	}
	path := strings.Trim(r.PathValue("path"), "/")
	entries, err := s.store.Tree(r.Context(), repo, rev, path)
	if err != nil {
		apiError(w, http.StatusNotFound, "no directory at that path")
		return
	}
	out := make([]apiEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, apiEntry{Name: e.Name, Path: e.Path, Type: e.Type, Size: e.Size})
	}
	writeJSON(w, map[string]any{
		"repo": repo.Name, "rev": rev, "path": path, "entries": out, "count": len(out),
	})
}

// apiFile returns one file's text. Binary is refused rather than encoded, since
// nothing that reads this can do anything with the bytes.
func (s *site) apiFile(w http.ResponseWriter, r *http.Request) {
	repo, rev, ok := s.apiResolve(w, r)
	if !ok {
		return
	}
	path := strings.Trim(r.PathValue("path"), "/")
	if path == "" {
		apiError(w, http.StatusNotFound, "no file at that path")
		return
	}
	src, size, err := s.store.Blob(r.Context(), repo, rev, path)
	switch {
	case err == errTooLarge:
		apiError(w, http.StatusRequestEntityTooLarge, "that file is too large to read")
		return
	case err != nil:
		apiError(w, http.StatusNotFound, "no file at that path")
		return
	case IsBinary(src):
		apiError(w, http.StatusUnsupportedMediaType, "that file is binary")
		return
	}
	writeJSON(w, map[string]any{
		"repo": repo.Name, "rev": rev, "path": path,
		"size": size, "lines": strings.Count(string(src), "\n") + 1,
		"language": languageOf(path), "text": string(src),
	})
}

// apiResolve is resolveRepo without the page furniture, and it answers JSON on
// the way out so a tool never has to read an HTML error.
func (s *site) apiResolve(w http.ResponseWriter, r *http.Request) (Repo, string, bool) {
	repo, ok := s.store.Open(r.PathValue("name"))
	if !ok {
		apiError(w, http.StatusNotFound, "no repository by that name")
		return Repo{}, "", false
	}
	rev := r.PathValue("rev")
	if rev == "" {
		rev = s.store.Head(r.Context(), repo)
	}
	if _, err := s.store.Resolve(r.Context(), repo, rev); err != nil {
		apiError(w, http.StatusNotFound, "no branch, tag or commit by that name")
		return Repo{}, "", false
	}
	return repo, rev, true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
