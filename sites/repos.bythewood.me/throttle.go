// A rate limit on the three route families robots.txt already asks crawlers to
// leave alone.
//
// A git browser has no bounded URL space: every commit times every file times
// every tree, across every repository here including the archived ones. robots.txt
// is the only thing between a crawler and all of it, so a crawler that ignores
// robots.txt walks forever. One did on 2026-09-10, 1,912 requests over two and a
// half hours from a Dutch hosting range, 1,077 of them to disallowed paths, and
// 201MB served of which 104MB was tarballs. The slowest single request was 25.6
// seconds of git archiving.
//
// This is the second layer the note about the edge asks for, and the one that is
// version controlled. It is in Go rather than in Caddy because the edge image is
// stock caddy:2-alpine and a rate limit there means an xcaddy build of the whole
// edge for one site.
package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"repos.bythewood.me/web"
)

// What each bucket allows. Archives are their own because they are the only
// route here that costs real work: git archive of a whole repository, measured
// at up to 25 seconds and several megabytes, against about 50KB for a page.
//
// A signed in request skips all of this, so these numbers only ever have to be
// generous to a stranger reading the site and not to Isaac using it.
var (
	// Reading commits, raw files and history. A person clicking through a
	// repository does not approach this and the crawler averaged 29 a minute.
	pageLimit = rate{burst: 40, per: time.Minute, refill: 20}

	// Tarballs. Three an hour is enough to take a copy of what somebody came
	// for and not enough to mirror the whole estate in an afternoon.
	archiveLimit = rate{burst: 3, per: time.Hour, refill: 3}
)

type rate struct {
	burst  float64
	per    time.Duration
	refill float64
}

// bucket is one address's allowance for one family of routes.
type bucket struct {
	tokens float64
	seen   time.Time
}

type throttle struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   rate
}

func newThrottle(limit rate) *throttle {
	return &throttle{buckets: map[string]*bucket{}, limit: limit}
}

// allow spends a token for addr, refilling by how long it has been. It returns
// how long to wait when there is nothing to spend, which becomes Retry-After so
// a well meaning client knows what to do rather than guessing.
func (t *throttle) allow(addr string, now time.Time) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	b, ok := t.buckets[addr]
	if !ok {
		// A first request from an address is never refused, so the map only
		// ever grows for somebody who came back.
		t.buckets[addr] = &bucket{tokens: t.limit.burst - 1, seen: now}
		t.sweep(now)
		return true, 0
	}

	perToken := t.limit.per / time.Duration(t.limit.refill)
	b.tokens += float64(now.Sub(b.seen)) / float64(perToken)
	if b.tokens > t.limit.burst {
		b.tokens = t.limit.burst
	}
	b.seen = now

	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) * float64(perToken))
	}
	b.tokens--
	return true, 0
}

// sweep drops addresses that have been full for a whole window, since a bucket
// at its burst is the same as no bucket at all. Called on the insert path, which
// is the only one that grows the map, so there is no goroutine to stop.
func (t *throttle) sweep(now time.Time) {
	if len(t.buckets) < 1024 {
		return
	}
	for addr, b := range t.buckets {
		if now.Sub(b.seen) > t.limit.per {
			delete(t.buckets, addr)
		}
	}
}

// limited wraps a handler with one of the buckets. A signed in request goes
// straight through: whether the session is live is auth's question, and somebody
// holding a cookie is not the traffic this exists to bound.
func (s *site) limited(t *throttle, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(web.SessionCookie); err == nil && c.Value != "" {
			next(w, r)
			return
		}
		ok, wait := t.allow(web.ClientIP(r), time.Now())
		if !ok {
			// Never cache a refusal. The edge would otherwise hand the same 429
			// to everybody else asking for that path.
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Error(w, "too many requests, see /robots.txt", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
