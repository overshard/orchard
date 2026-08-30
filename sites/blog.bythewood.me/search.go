package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// matches is a substring scan over title, description and tags. Bodies are not
// searched, since a match nothing highlights looks like a wrong result.
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
		// An empty query offers somewhere to go rather than no results.
		data.RandomPosts = pickRandom(published, 6)
	} else {
		data.Posts = search(published, query, 0)
		data.NoResults = len(data.Posts) == 0
	}

	s.renderer.Render(w, http.StatusOK, "search.html", data)
}

// searchLive backs the type-ahead in search.js, which renders a short dropdown.
func (s *site) searchLive(w http.ResponseWriter, r *http.Request) {
	published, _, _ := s.lib.Published()

	type result struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
	}

	// An empty array, never null: search.js reads data.length.
	out := []result{}
	for _, post := range search(published, r.URL.Query().Get("q"), 5) {
		out = append(out, result{post.Title, post.Description, post.URL()})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	// search.js builds rows from text nodes, so the default escaping would only
	// corrupt titles.
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(out)
}
