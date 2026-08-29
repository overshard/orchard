package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// The home page's promo slot, filled from the blog rather than hardcoded to
// one project, so it cannot go stale when that project is retired.
//
// Same shape as the commit cache next door: a background refresh on a ticker,
// the page always servable, and no card emitted when there is nothing cached,
// so a blog outage leaves a hero with no promo rather than a broken one.

const (
	latestRefreshInterval = time.Hour
	latestFetchTimeout    = 10 * time.Second
)

// LatestPost is the blog's /latest.json, a hand written shape on that side so
// this struct does not have to track its Post type.
type LatestPost struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Date        string `json:"date"`
}

// LatestCache holds the most recent successful fetch.
type LatestCache struct {
	mu      sync.RWMutex
	post    LatestPost
	ok      bool
	sources []string
	client  *http.Client
}

func NewLatestCache(sources []string) *LatestCache {
	return &LatestCache{
		sources: sources,
		client:  &http.Client{Timeout: latestFetchTimeout},
	}
}

// Get returns the cached post, and whether there is one worth rendering.
func (c *LatestCache) Get() (LatestPost, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.post, c.ok
}

// Start does one fetch immediately and then refreshes on a ticker until ctx is
// cancelled. It returns straight away, so the site does not wait on the blog to
// begin serving.
func (c *LatestCache) Start(ctx context.Context) {
	go func() {
		c.refresh(ctx)

		ticker := time.NewTicker(latestRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refresh(ctx)
			}
		}
	}()
}

func (c *LatestCache) refresh(ctx context.Context) {
	// First source that answers wins. In the image that is the sibling
	// container; in a local checkout it falls through to the public site.
	var post LatestPost
	var err error
	for _, src := range c.sources {
		post, err = c.fetch(ctx, src)
		if err == nil {
			break
		}
		slog.Info(fmt.Sprintf("latest post: %s: %v", src, err))
	}
	if err != nil {
		// Keep whatever was there, so a transient failure does not blank a
		// card that was fine an hour ago.
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.post = post
	c.ok = post.Title != "" && post.URL != ""
}

func (c *LatestCache) fetch(ctx context.Context, url string) (LatestPost, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return LatestPost{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "isaacbythewood.com")

	resp, err := c.client.Do(req)
	if err != nil {
		return LatestPost{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return LatestPost{}, fmt.Errorf("status %d", resp.StatusCode)
	}

	var post LatestPost
	if err := json.NewDecoder(resp.Body).Decode(&post); err != nil {
		return LatestPost{}, err
	}
	return post, nil
}
