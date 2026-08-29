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

// The code page shows the latest commit for each project, refreshed in the
// background on a ticker. The page is always servable, and a GitHub outage
// degrades it to no commit lines rather than blocking a render.
//
// Unauthenticated GitHub allows 60 requests an hour per IP. Nine repos once an
// hour is inside that, and there is no token to store.

const (
	commitRefreshInterval = time.Hour
	commitFetchTimeout    = 10 * time.Second
)

// Commit is the subset of a GitHub commit the page shows, and the shape that
// gets rendered as JSON into the card.
type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Date    string `json:"date"`
	Author  string `json:"author"`
}

// CommitCache holds the most recent successful fetch per repo.
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

// Get returns the cached commit for a repo, and whether there is one.
func (c *CommitCache) Get(slug string) (Commit, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	commit, ok := c.commits[slug]
	return commit, ok
}

// JSON renders a commit the way the card displays it, pretty-printed.
//
// SetEscapeHTML(false), which json.MarshalIndent cannot do. The output goes
// into html/template, which escapes for the HTML context already, so the
// default only makes a commit message containing an arrow render as a literal
// "\u003e" inside the <pre>.
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

// Start does one fetch immediately and then refreshes on a ticker until ctx is
// cancelled. It returns straight away, so the site does not wait on GitHub to
// begin serving.
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

	// A failed repo keeps its previous value, so a transient 502 does not
	// blank a card that was fine a minute ago.
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

	// Subject line only. The card is a few lines of JSON in a <pre>, so a
	// commit with a body would render its whole body as one escaped string
	// and blow the card out of shape, same reason the SHA is cut to 7.
	message, _, _ := strings.Cut(head.Commit.Message, "\n")

	return Commit{
		SHA:     sha,
		Message: strings.TrimSpace(message),
		Date:    head.Commit.Author.Date,
		Author:  head.Commit.Author.Name,
	}, nil
}
