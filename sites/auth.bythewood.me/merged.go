package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"auth.bythewood.me/suite"
	"auth.bythewood.me/web"
)

// The tools auth asks for their state, by container, over the bridge.
var stateTools = []string{"status", "logging", "analytics", "cron", "llm", "repos"}

type merged struct {
	Tools []suite.State `json:"tools"`
	At    time.Time     `json:"at"`
}

// mergedCache holds the last answer a little while, since every page of every
// tool asks for it and there's one person signed in.
type mergedCache struct {
	mu   sync.Mutex
	last merged
}

const mergedTTL = 20 * time.Second

var stateClient = &http.Client{Timeout: 3 * time.Second}

// suiteMerged is every tool's state at once, for the dots on the tool tabs and
// the jump search. Each tool checks the forwarded cookie itself.
func (s *site) suiteMerged(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, err := lookupSession(s.db, r); err != nil {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}
	c, _ := r.Cookie(web.SessionCookie)

	s.merged.mu.Lock()
	out := s.merged.last
	s.merged.mu.Unlock()
	if time.Since(out.At) > mergedTTL {
		out = s.askTools(r.Context(), c)
		s.merged.mu.Lock()
		s.merged.last = out
		s.merged.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *site) askTools(ctx context.Context, c *http.Cookie) merged {
	states := make([]suite.State, len(stateTools))
	var wg sync.WaitGroup
	for i, name := range stateTools {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st := suite.State{Tool: name, Note: name + " didn't answer"}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://orchard-%s:8000/suite/state", name), nil)
			req.AddCookie(c)
			resp, err := stateClient.Do(req)
			if err == nil {
				if resp.StatusCode == http.StatusOK {
					_ = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&st)
				}
				resp.Body.Close()
			}
			st.Tool = name
			states[i] = st
		}()
	}
	wg.Wait()
	return merged{Tools: append([]suite.State{s.ownState()}, states...), At: time.Now()}
}

// ownState is auth's own entry, its pages and whether the recovery codes are
// running low.
func (s *site) ownState() suite.State {
	var sessions int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE expires > ?`, time.Now().Unix()).Scan(&sessions)
	codes, _ := countRecoveryCodes(s.db)
	low := ""
	if codes < 3 {
		low = "warn"
	}
	st := suite.State{Tool: "auth", Level: "ok", Note: fmt.Sprintf("%s, %d recovery codes left", count(sessions, "session", "sessions"), codes)}
	if low != "" {
		st.Level, st.Count = low, codes
	}
	base := "https://auth.bythewood.me"
	st.Items = []suite.Item{
		{Kind: "page", Name: "account", Href: base + "/account", Note: "every tool behind this sign in"},
		{Kind: "page", Name: "sessions", Href: base + "/sessions", Note: count(sessions, "browser", "browsers") + " signed in"},
		{Kind: "page", Name: "security", Href: base + "/security", Level: low, Note: fmt.Sprintf("%d recovery codes left", codes)},
		{Kind: "page", Name: "activity", Href: base + "/activity", Note: "every sign in, newest first"},
	}
	return st
}
