package suite

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// State is one tool's answer to /suite/state, how it is and what it holds,
// for the dot on its tab and the jump search.
type State struct {
	Tool string `json:"tool"`
	// Level is ok, warn or bad, and Count is how many things are behind it.
	Level string `json:"level,omitempty"`
	Count int    `json:"count,omitempty"`
	Note  string `json:"note,omitempty"`
	Items []Item `json:"items"`
}

// Item is one thing the jump search can open. Href is absolute, since the
// search runs on every tool's pages. Kind action lists it under > as well, and
// Post is a path the palette posts to when it runs on that tool's own origin.
type Item struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Href  string `json:"href"`
	Level string `json:"level,omitempty"`
	Note  string `json:"note,omitempty"`
	Post  string `json:"post,omitempty"`
}

// StateHandler serves a tool's state to somebody signed in, which in practice
// is auth asking over the bridge with the visitor's cookie.
func StateHandler(signedIn func(*http.Request) bool, state func(*http.Request) State) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !signedIn(r) {
			http.Error(w, "sign in first", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state(r))
	})
}

// mergedURL is auth's every tool at once, reached over the bridge so a page
// can ask its own origin and needs no CORS.
const mergedURL = "http://orchard-auth:8000/suite/merged"

// sessionCookie is web.SessionCookie, written out since suite can't import a
// site's own web package and stay the same in every site.
const sessionCookie = "bw_session"

var bridge = &http.Client{Timeout: 4 * time.Second}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, mergedURL, nil)
	req.AddCookie(c)
	resp, err := bridge.Do(req)
	if err != nil {
		http.Error(w, "auth didn't answer", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 4<<20))
}
