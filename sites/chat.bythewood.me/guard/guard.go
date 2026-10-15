// Package guard stands in front of somebody else's server with a pace between
// calls, a budget per window and a breaker that opens when the far end says no.
// It is copied into every site that uses it and is byte identical in each.
package guard

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Policy is one upstream's limits, and a zero field turns its limit off.
type Policy struct {
	// Gap is the least time between two calls.
	Gap time.Duration
	// Jitter adds up to a fifth of Gap at random, since clients pacing to the
	// same round number arrive in step, which looks like an attack from the far end.
	Jitter bool

	// Budget calls a Window, counted in a window that starts at its first call.
	Budget int
	Window time.Duration

	// TripAfter failures in a row open the breaker.
	TripAfter int
	// OpenFor is the first open. Each one after it without a success between
	// doubles, up to MaxOpen, and a zero MaxOpen keeps every open the same.
	OpenFor time.Duration
	MaxOpen time.Duration
	// FailOpenFor is a flat open for a breaker opened by failures instead of a
	// refusal, where the two deserve different lengths.
	FailOpenFor time.Duration

	// HalfOpen forgives the failures once an open has run out, so they build up
	// again from nothing before they reopen it. A refusal still reopens at once.
	HalfOpen bool
	// FillOnRefusal sets the failures to TripAfter on a refusal instead of adding
	// one, so the streak reads as full while the breaker is open.
	FillOnRefusal bool
	// KeepLonger stops a new open cutting short one already in force.
	KeepLonger bool
}

// State is what the guard remembers about one key between calls.
type State struct {
	LastCall    time.Time
	WindowStart time.Time
	Count       int
	Fails       int
	Trips       int
	OpenUntil   time.Time
}

// Store keeps each key's State. The guard calls it with its own lock held, so a
// store only needs a lock for whatever else reads it.
type Store interface {
	Load(key string) (State, error)
	// Save is handed why the last call failed, or nothing.
	Save(key string, s State, why string)
}

// Memory keeps state for as long as the process runs.
type Memory map[string]State

func (m Memory) Load(key string) (State, error)     { return m[key], nil }
func (m Memory) Save(key string, s State, _ string) { m[key] = s }

func (m Memory) Range(fn func(key string, s State)) {
	for k, s := range m {
		fn(k, s)
	}
}

// OpenError means the breaker is open and nothing was sent.
type OpenError struct {
	Key  string
	Left time.Duration
}

func (e *OpenError) Error() string {
	return fmt.Sprintf("%s: open for another %s", e.Key, e.Left.Round(time.Second))
}

// SpentError means the window's budget is gone and nothing was sent.
type SpentError struct {
	Key    string
	Used   int
	Budget int
	Window time.Duration
}

func (e *SpentError) Error() string {
	return fmt.Sprintf("%s: budget of %d spent in this %s", e.Key, e.Budget, e.Window)
}

// ErrUnknown is a key with no policy, which is refused instead of let through
// unguarded.
var ErrUnknown = errors.New("no policy")

// Kind is how a call went wrong.
type Kind int

const (
	// Failed counts toward TripAfter and opens the breaker on reaching it.
	Failed Kind = iota
	// Refused is the far end saying stop, like a 429, and opens it at once.
	Refused
	// Noted counts toward TripAfter but never opens the breaker itself.
	Noted
)

type Failure struct {
	Kind       Kind
	RetryAfter time.Duration
	Why        string
}

type Guard struct {
	mu     sync.Mutex
	store  Store
	policy func(key string) (Policy, bool)
}

// New guards every key policy knows. A key it does not know is refused, and its
// Policy is still used for the breaker when a failure is reported against it.
func New(store Store, policy func(key string) (Policy, bool)) *Guard {
	return &Guard{store: store, policy: policy}
}

// admit applies the breaker and the budget, and reports whether it changed the
// state on the way.
func (g *Guard) admit(key string, now time.Time) (Policy, State, bool, error) {
	s, err := g.store.Load(key)
	if err != nil {
		return Policy{}, s, false, err
	}
	if now.Before(s.OpenUntil) {
		return Policy{}, s, false, &OpenError{Key: key, Left: s.OpenUntil.Sub(now)}
	}
	p, ok := g.policy(key)
	if !ok {
		return p, s, false, fmt.Errorf("%s: %w", key, ErrUnknown)
	}
	changed := false
	if p.HalfOpen && !s.OpenUntil.IsZero() {
		s.OpenUntil = time.Time{}
		s.Fails = 0
		changed = true
	}
	if p.Budget > 0 {
		if s.WindowStart.IsZero() || now.Sub(s.WindowStart) >= p.Window {
			s.WindowStart = now
			s.Count = 0
			changed = true
		}
		if s.Count >= p.Budget {
			return p, s, false, &SpentError{Key: key, Used: s.Count, Budget: p.Budget, Window: p.Window}
		}
	}
	return p, s, changed, nil
}

func pace(p Policy, s State, now time.Time) time.Duration {
	if p.Gap <= 0 || s.LastCall.IsZero() {
		return 0
	}
	gap := p.Gap
	if p.Jitter {
		gap += time.Duration(rand.Int64N(int64(p.Gap/5) + 1))
	}
	return gap - now.Sub(s.LastCall)
}

// Take reserves a call to key at now, or says how long the pace has left.
// Nothing is counted while the answer is a wait, so a caller that waits asks
// again and an upstream that has said no in the meantime is refused.
func (g *Guard) Take(key string, now time.Time) (time.Duration, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	p, s, changed, err := g.admit(key, now)
	if err != nil {
		return 0, err
	}
	if wait := pace(p, s, now); wait > 0 {
		if changed {
			g.store.Save(key, s, "")
		}
		return wait, nil
	}
	s.Count++
	s.LastCall = now
	g.store.Save(key, s, "")
	return 0, nil
}

// Wait sleeps out the pace and then reserves. The lock is held across the
// sleep so callers go out in the order they asked, and every other key on this
// guard waits behind it too.
func (g *Guard) Wait(ctx context.Context, key string, now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	p, s, _, err := g.admit(key, now)
	if err != nil {
		return err
	}
	if wait := pace(p, s, now); wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
	s.Count++
	s.LastCall = time.Now()
	g.store.Save(key, s, "")
	return nil
}

// Succeed clears the failures and the trips, so one bad hour doesn't follow an
// upstream around.
func (g *Guard) Succeed(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	s, err := g.store.Load(key)
	if err != nil || (s.Fails == 0 && s.Trips == 0) {
		return
	}
	s.Fails, s.Trips = 0, 0
	g.store.Save(key, s, "")
}

// Fail records a call that went wrong at now and returns how long it opened the
// breaker for, zero when it stays closed.
func (g *Guard) Fail(key string, now time.Time, f Failure) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()

	p, _ := g.policy(key)
	s, err := g.store.Load(key)
	if err != nil {
		return 0
	}
	s.Fails++
	if f.Kind == Refused && p.FillOnRefusal {
		s.Fails = p.TripAfter
	}
	trip := f.Kind == Refused || (f.Kind == Failed && p.TripAfter > 0 && s.Fails >= p.TripAfter)
	if !trip {
		g.store.Save(key, s, f.Why)
		return 0
	}

	s.Trips++
	open := Backoff(p.OpenFor, p.MaxOpen, s.Trips)
	if f.Kind == Failed && p.FailOpenFor > 0 {
		open = p.FailOpenFor
	}
	// The far end knows when it wants us back, and guessing shorter is how a
	// soft limit becomes a hard one.
	if f.RetryAfter > open {
		open = f.RetryAfter
	}
	until := now.Add(open)
	if p.KeepLonger && s.OpenUntil.After(until) {
		until = s.OpenUntil
	}
	s.OpenUntil = until
	g.store.Save(key, s, f.Why)
	return open
}

// Open reports whether key's breaker is open at now, and for how much longer.
func (g *Guard) Open(key string, now time.Time) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	s, err := g.store.Load(key)
	if err != nil || !now.Before(s.OpenUntil) {
		return false, 0
	}
	return true, s.OpenUntil.Sub(now)
}

// Put replaces key's state, for one kept somewhere the store doesn't reach.
func (g *Guard) Put(key string, s State) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.store.Save(key, s, "")
}

// Each calls fn with every key a store that can list them holds, under the
// guard's lock.
func (g *Guard) Each(fn func(key string, s State)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r, ok := g.store.(interface{ Range(func(string, State)) }); ok {
		r.Range(fn)
	}
}

// Backoff is base doubled for each trip after the first, up to ceiling.
func Backoff(base, ceiling time.Duration, trips int) time.Duration {
	d := base
	for i := 1; i < trips && d < ceiling; i++ {
		d *= 2
	}
	if ceiling > 0 && d > ceiling {
		d = ceiling
	}
	return d
}

// RetryAfter reads both forms, seconds or an HTTP date, since reading only the
// first means asking again straight away. Anything else is no answer at all.
func RetryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		if d := when.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}
