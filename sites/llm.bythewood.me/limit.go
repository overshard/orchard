// Claude calls cost money and a key is a bearer secret, so a leaked one is held
// to a pace per key and every key stops once the month's budget is spent.
package main

import (
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Serial callers wait seconds on each answer, so only a loop gets near this.
const (
	keyBurst     = 10
	keyPerSecond = 1
)

// keyLimiter is a token bucket per key. The zero value is ready.
type keyLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*keyBucket
}

type keyBucket struct {
	tokens float64
	at     time.Time
}

func (l *keyLimiter) allow(key int64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[int64]*keyBucket{}
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &keyBucket{tokens: keyBurst, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(keyBurst, b.tokens+now.Sub(b.at).Seconds()*keyPerSecond)
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// claudeAllowed turns a call away with a 429 when its key is going too fast or
// the month is over budget, and says whether it may go on.
func (s *site) claudeAllowed(w http.ResponseWriter, k Key) bool {
	now := time.Now()
	if !s.limits.allow(k.ID, now) {
		w.Header().Set("Retry-After", strconv.Itoa(1/keyPerSecond))
		writeOAError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "too many requests for this key, slow down")
		return false
	}
	if s.budget <= 0 {
		return true
	}
	m, err := s.month(now)
	if err != nil {
		// A broken read of the ledger lets the call through, so it doesn't stop
		// every caller at once.
		slog.Error("reading the month's spend", "err", err)
		return true
	}
	if m.Spent >= s.budget {
		writeOAError(w, http.StatusTooManyRequests, "insufficient_quota", "the gateway has spent this month's budget")
		return false
	}
	return true
}
