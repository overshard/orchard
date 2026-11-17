package main

import (
	"net/netip"
	"sync"
	"time"
)

// limiter is a token bucket per key, with a cap on how many keys it holds.
type limiter struct {
	burst     float64
	perMinute float64
	maxKeys   int

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

// A page view is a handful of events, so a browser never gets near these and a
// script hammering /collect from one network is turned away with a 429.
func newClientLimiter() *limiter {
	return &limiter{burst: 60, perMinute: 30, maxKeys: 10000}
}

// The backstop for a spread of addresses all posting to one property, set far
// above what a personal site's visitors ever send.
func newPropertyLimiter() *limiter {
	return &limiter{burst: 200, perMinute: 20 * 60, maxKeys: 10000}
}

type bucket struct {
	tokens float64
	at     time.Time
}

func (l *limiter) refill(b *bucket, now time.Time) {
	b.tokens = min(l.burst, b.tokens+now.Sub(b.at).Minutes()*l.perMinute)
	b.at = now
}

// A full bucket is the same as none, so those are swept to keep the map to the
// keys posting right now.
func (l *limiter) sweep(now time.Time) {
	for key, b := range l.buckets {
		if l.refill(b, now); b.tokens >= l.burst {
			delete(l.buckets, key)
		}
	}
	l.swept = now
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*bucket{}
	}
	if now.Sub(l.swept) > time.Minute {
		l.sweep(now)
	}
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			l.sweep(now)
		}
		// Still full means that many keys are all mid burst, which is a flood,
		// so a new key waits instead of growing the map.
		if len(l.buckets) >= l.maxKeys {
			return false
		}
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	l.refill(b, now)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// clientKey groups IPv6 by /64, since one subscriber is usually handed a whole
// /64 and could otherwise post from a fresh address every time.
func clientKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr.WithZone(""), 64).Masked().String()
}
