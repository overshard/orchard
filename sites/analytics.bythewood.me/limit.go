package main

import (
	"sync"
	"time"
)

// A page view is a handful of events, so a browser never gets near these and a
// script hammering /collect from one address is turned away with a 429.
const (
	collectBurst     = 60
	collectPerMinute = 30
)

// ipLimiter is a token bucket per client address. The zero value is ready.
type ipLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func (b *bucket) refill(now time.Time) {
	b.tokens = min(collectBurst, b.tokens+now.Sub(b.at).Minutes()*collectPerMinute)
	b.at = now
}

func (l *ipLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*bucket{}
	}
	// A full bucket is the same as none, so those are swept to keep the map to
	// the addresses posting right now.
	if now.Sub(l.swept) > time.Minute {
		for ip, b := range l.buckets {
			if b.refill(now); b.tokens >= collectBurst {
				delete(l.buckets, ip)
			}
		}
		l.swept = now
	}
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{tokens: collectBurst, at: now}
		l.buckets[ip] = b
	}
	b.refill(now)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
