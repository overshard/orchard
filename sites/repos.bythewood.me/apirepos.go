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
	"strconv"
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

	lines := strings.Split(string(src), "\n")
	// A file ending in a newline splits with an empty element after it, which
	// is not a line. Left in, the count is one too many and a range past the
	// end clamps onto it and returns nothing.
	if n := len(lines); n > 1 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	total := len(lines)
	from, to := lineRange(r, total)
	text := strings.Join(lines[from-1:to], "\n")
	// The trailing newline was split off above so it would not count as a line.
	// A read that runs to the end of the file gets it back, since a whole file
	// read has to hand over the file as it is.
	if to == total && strings.HasSuffix(string(src), "\n") {
		text += "\n"
	}
	body := map[string]any{
		"repo": repo.Name, "rev": rev, "path": path,
		"size": size, "lines": total,
		"language": languageOf(path), "text": text,
	}
	// Only when a slice was actually taken, so an ordinary whole file read is
	// the same shape it always was.
	if from != 1 || to != total {
		body["from"], body["to"], body["partial"] = from, to, true
	}
	writeJSON(w, body)
}

// lineRange reads from and to off the query, clamped to the file. A reader that
// wants one function out of a thousand line file should not have to take the
// thousand lines to get it, and on the other end of this the whole file is
// context that a small window pays for.
func lineRange(r *http.Request, total int) (int, int) {
	from := queryInt(r, "from", 1)
	to := queryInt(r, "to", total)
	if from < 1 {
		from = 1
	}
	if to > total || to < 1 {
		to = total
	}
	if from > total {
		from = total
	}
	if to < from {
		to = from
	}
	return from, to
}

func queryInt(r *http.Request, key string, fallback int) int {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// apiGrep searches file contents. The pathspec is how a question about one site
// stays inside it rather than reading the other ten.
func (s *site) apiGrep(w http.ResponseWriter, r *http.Request) {
	repo, rev, ok := s.apiResolve(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		apiError(w, http.StatusBadRequest, "q is required")
		return
	}
	hits, more, err := s.store.Grep(r.Context(), repo, rev, q,
		r.URL.Query().Get("path"), queryInt(r, "max", 0))
	if err != nil {
		slog.Error("grep repo", slog.String("repo", repo.Name), slog.Any("err", err))
		apiError(w, http.StatusInternalServerError, "that search could not run")
		return
	}
	if hits == nil {
		hits = []GrepHit{}
	}
	writeJSON(w, map[string]any{
		"repo": repo.Name, "rev": rev, "query": q,
		"hits": hits, "count": len(hits), "truncated": more,
	})
}

// apiFind matches on the path rather than the contents, which is the call that
// turns a file name into a full path without walking a single directory.
func (s *site) apiFind(w http.ResponseWriter, r *http.Request) {
	repo, rev, ok := s.apiResolve(w, r)
	if !ok {
		return
	}
	paths, more, err := s.store.FindPaths(r.Context(), repo, rev,
		r.URL.Query().Get("q"), queryInt(r, "max", 0))
	if err != nil {
		slog.Error("find paths", slog.String("repo", repo.Name), slog.Any("err", err))
		apiError(w, http.StatusInternalServerError, "that listing could not run")
		return
	}
	if paths == nil {
		paths = []string{}
	}
	writeJSON(w, map[string]any{
		"repo": repo.Name, "rev": rev, "query": strings.TrimSpace(r.URL.Query().Get("q")),
		"paths": paths, "count": len(paths), "truncated": more,
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
