package skills

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ESPN answers 202 with an empty body when it is throttling, which is the same
// signature DuckDuckGo uses and is easy to read as an empty scoreboard rather
// than as a refusal. It showed up here because one sports question fans out to
// every league at once, so a single question is seventeen requests of about a
// megabyte, and a few questions in a row look like scraping.
//
// Two things fix it and both live in this file. The scoreboard for a league on
// a date does not change fast enough to fetch twice in five minutes, and once
// an upstream starts refusing there is no point asking again for a while.
var errThrottled = errors.New("upstream is throttling")

const (
	scoreboardTTL = 5 * time.Minute
	breakerFor    = 10 * time.Minute
)

type cached struct {
	body []byte
	at   time.Time
}

type upstream struct {
	mu      sync.Mutex
	seen    map[string]cached
	blocked map[string]time.Time
}

var shared = &upstream{seen: map[string]cached{}, blocked: map[string]time.Time{}}

// blockedUntil reports whether a host is in its cooldown.
func (u *upstream) blockedUntil(host string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	until, ok := u.blocked[host]
	return ok && time.Now().Before(until)
}

func (u *upstream) block(host string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.blocked[host] = time.Now().Add(breakerFor)
}

func (u *upstream) get(key string) ([]byte, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.seen[key]
	if !ok || time.Since(c.at) > scoreboardTTL {
		return nil, false
	}
	return c.body, true
}

func (u *upstream) put(key string, body []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	// Bounded so a long-running process cannot grow this without limit. The
	// whole point is a handful of leagues for a few minutes.
	if len(u.seen) > 64 {
		u.seen = map[string]cached{}
	}
	u.seen[key] = cached{body: body, at: time.Now()}
}

// getJSONCached is getJSON with the cache and the breaker in front of it, for
// the upstreams that get asked the same thing repeatedly.
func getJSONCached(ctx context.Context, d Deps, host, url string, out any) error {
	if shared.blockedUntil(host) {
		return errThrottled
	}
	if body, ok := shared.get(url); ok {
		return decode(body, out)
	}
	body, err := fetch(ctx, d, url)
	if err != nil {
		if errors.Is(err, errThrottled) {
			shared.block(host)
		}
		return err
	}
	shared.put(url, body)
	return decode(body, out)
}

func fetch(ctx context.Context, d Deps, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", d.UA)
	req.Header.Set("Accept", "application/json")

	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 202 with nothing in it is the throttle, not a slow answer.
	if resp.StatusCode == http.StatusAccepted {
		return nil, errThrottled
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return readAll(resp)
}
