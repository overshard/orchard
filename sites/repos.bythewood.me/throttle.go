// A rate limit for crawlers that ignore robots.txt, since a git browser's URL
// space has no bound and one of them walks it forever. It's in Go since the edge
// runs stock caddy and a limit there needs an xcaddy build of the whole edge.
package main

import (
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"repos.bythewood.me/web"
)

// Archives get their own bucket since a git archive of a whole repository is the
// only route here that costs real work. A signed in request skips all of this, so
// these only have to be generous to a stranger reading the site.
var (
	// Reading commits, raw files and history. A person clicking through a
	// repository doesn't come near this.
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
// a well meaning client knows what to do instead of guessing.
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

// limited wraps a handler with a bucket. A refused request still goes through
// when auth says its session is live, and auth is only asked on a refusal so a
// crawler with a made up cookie can't turn every request into a call to auth.
func (s *site) limited(t *throttle, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, wait := t.allow(web.ClientIP(r), time.Now())
		if !ok && s.auth.Authenticated(r) {
			ok = true
		}
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

// Meta's crawler fetches robots.txt, which disallows everything here, and walks
// the site anyway from hundreds of addresses in this range, so no bucket keyed on
// one address ever fills.
var metaCrawler = netip.MustParsePrefix("2a03:2880::/32")

func refuseMeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if addr, err := netip.ParseAddr(web.ClientIP(r)); err == nil && metaCrawler.Contains(addr) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "forbidden, see /robots.txt", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var browsePages = map[string]bool{
	"tree": true, "blob": true, "raw": true, "log": true, "commit": true,
	"branches": true, "tags": true, "archive": true,
}

// Every Chrome since 80 and Firefox since 90 sends Sec-Fetch-Mode on a page load,
// so a browser UA without it is a script wearing one. A crawler that names itself
// says "compatible" or "bot" and is left to robots.txt.
func refuseFakeBrowsers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fakeBrowser(r) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "forbidden, see /robots.txt", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func fakeBrowser(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Mode") != "" {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 3)
	if len(parts) < 2 || !browsePages[parts[1]] {
		return false
	}
	ua := r.UserAgent()
	if !strings.Contains(ua, "Chrome/") && !strings.Contains(ua, "Firefox/") {
		return false
	}
	lower := strings.ToLower(ua)
	return !strings.Contains(lower, "compatible") && !strings.Contains(lower, "bot")
}
