package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Unauthenticated GitHub allows 60 requests an hour per IP, so nine repos on an
// hourly ticker fits inside it with no token to store.

const (
	commitRefreshInterval = time.Hour
	commitFetchTimeout    = 10 * time.Second
)

type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Date    string `json:"date"`
	Author  string `json:"author"`
}

// CommitCache holds the most recent successful fetch per repo and is safe for
// concurrent use.
type CommitCache struct {
	mu      sync.RWMutex
	commits map[string]Commit
	client  *http.Client
}

func NewCommitCache() *CommitCache {
	return &CommitCache{
		commits: make(map[string]Commit),
		client:  &http.Client{Timeout: commitFetchTimeout},
	}
}

func (c *CommitCache) Get(slug string) (Commit, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	commit, ok := c.commits[slug]
	return commit, ok
}

// JSON renders a commit the way the card displays it. SetEscapeHTML(false),
// which MarshalIndent cannot do, because html/template escapes already and the
// default renders an arrow in a message as a literal "\u003e" in the <pre>.
func (c *CommitCache) JSON(slug string) string {
	commit, ok := c.Get(slug)
	if !ok {
		return ""
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(commit); err != nil {
		return ""
	}
	// Encode appends a newline that MarshalIndent does not.
	return strings.TrimRight(buf.String(), "\n")
}

// Start returns straight away, fetches once, then refreshes on a ticker until
// ctx is cancelled, so the site never waits on GitHub to begin serving.
func (c *CommitCache) Start(ctx context.Context, slugs []string) {
	go func() {
		c.refresh(ctx, slugs)

		ticker := time.NewTicker(commitRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refresh(ctx, slugs)
			}
		}
	}()
}

func (c *CommitCache) refresh(ctx context.Context, slugs []string) {
	var wg sync.WaitGroup
	results := make([]struct {
		slug   string
		commit Commit
		ok     bool
	}, len(slugs))

	for i, slug := range slugs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			commit, err := c.fetch(ctx, slug)
			if err != nil {
				slog.Info(fmt.Sprintf("github: %s: %v", slug, err))
				return
			}
			results[i].slug = slug
			results[i].commit = commit
			results[i].ok = true
		}()
	}
	wg.Wait()

	// A failed repo keeps its previous value, so a 502 does not blank a card.
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range results {
		if r.ok {
			c.commits[r.slug] = r.commit
		}
	}
}

func (c *CommitCache) fetch(ctx context.Context, slug string) (Commit, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits?per_page=1", githubUser, slug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Commit{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "isaacbythewood.com")

	resp, err := c.client.Do(req)
	if err != nil {
		return Commit{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Commit{}, fmt.Errorf("status %d", resp.StatusCode)
	}

	var payload []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Commit{}, err
	}
	if len(payload) == 0 {
		return Commit{}, fmt.Errorf("no commits")
	}

	head := payload[0]
	sha := head.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}

	// Subject line only. The card is a few lines of JSON in a <pre>, and a
	// commit body would render as one escaped string and blow it out of shape.
	message, _, _ := strings.Cut(head.Commit.Message, "\n")

	return Commit{
		SHA:     sha,
		Message: strings.TrimSpace(message),
		Date:    head.Commit.Author.Date,
		Author:  head.Commit.Author.Name,
	}, nil
}
