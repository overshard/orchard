package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Search is a substring scan over titles, descriptions and tags, held in
// memory. Twenty-two posts do not need an index, and the moment they do the
// honest answer is a real search engine rather than a hand-rolled one.
//
// Post bodies are deliberately not searched, which is unchanged from the Rust
// version: matching body text without highlighting where the match was makes
// results that look wrong.
func matches(post *Post, needle string) bool {
	if strings.Contains(strings.ToLower(post.Title), needle) ||
		strings.Contains(strings.ToLower(post.Description), needle) {
		return true
	}
	for _, tag := range post.Tags {
		if strings.Contains(strings.ToLower(tag), needle) {
			return true
		}
	}
	return false
}

func search(posts []*Post, query string, limit int) []*Post {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil
	}
	var out []*Post
	for _, post := range posts {
		if matches(post, needle) {
			out = append(out, post)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out
}

func (s *site) search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	published, _, _ := s.lib.Published()

	data := s.page(r, "Search",
		"Search posts on webdev, coding, security, and sysadmin.")
	data.ShowSocial = true
	data.Heading = "Search"
	data.Query = query
	data.Breadcrumbs = []Crumb{{Title: "Home", URL: "/"}, {Title: "Search"}}

	if strings.TrimSpace(query) == "" {
		// No query is not a failed search, so the page offers somewhere to go
		// instead of an empty result list.
		data.RandomPosts = pickRandom(published, 6)
	} else {
		data.Posts = search(published, query, 0)
		data.NoResults = len(data.Posts) == 0
	}

	s.renderer.Render(w, http.StatusOK, "search.html", data)
}

// searchLive backs the type-ahead in search.js. Five results, because it
// renders into a dropdown under the input.
func (s *site) searchLive(w http.ResponseWriter, r *http.Request) {
	published, _, _ := s.lib.Published()

	type result struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
	}

	// An empty array, never null: search.js does data.length on the response
	// and null has no length.
	out := []result{}
	for _, post := range search(published, r.URL.Query().Get("q"), 5) {
		out = append(out, result{post.Title, post.Description, post.URL()})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	// SetEscapeHTML(false) because encoding/json turns <, > and & into <
	// by default. Nothing here is interpolated into a page (search.js builds
	// rows from text nodes), so the escaping only corrupts titles.
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(out)
}
