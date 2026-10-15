package tools

import (
	"sync"
	"time"
)

// PenaltyStore is an interface so this package stays a leaf and doesn't import
// the database.
type PenaltyStore interface {
	SavePenalty(host string, till time.Time, trips int)
	ClearPenalty(host string)
}

// Guard is a per host circuit breaker. A host that refuses goes in the penalty
// box, and every call to it fails locally until the box empties.
type Guard struct {
	mu   sync.Mutex
	till map[string]time.Time
	last map[string]time.Time
	// How many times in a row a host has failed, saved with the box.
	trips map[string]int
	cool  time.Duration
	store PenaltyStore
}

func NewGuard(cool time.Duration) *Guard {
	return &Guard{
		till: map[string]time.Time{}, last: map[string]time.Time{},
		trips: map[string]int{}, cool: cool,
	}
}

// Wait blocks until this host may be called again. The lock is held across the
// sleep so two turns asking the same host queue instead of both deciding the gap
// has passed, so don't narrow it.
func (g *Guard) Wait(host string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	gap := budgetFor(host).gap
	if t, seen := g.last[host]; seen {
		if d := gap - time.Since(t); d > 0 {
			time.Sleep(d)
		}
	}
	g.last[host] = time.Now()
}

func (g *Guard) Blocked(host string) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.till[host]
	if !ok || time.Now().After(t) {
		return false, 0
	}
	return true, time.Until(t)
}

// Long and flat, since a short box asks again several times an hour against an
// endpoint that already said no, which turns a soft limit into a hard one, and
// nothing but a call says a ban has lifted.
const refusalCool = 6 * time.Hour

// A page that timed out isn't a host refusing us, so this is long enough to stop
// a turn hammering a dead address and short enough that the next question can try.
const stumbleCool = 5 * time.Minute

// Trip is for a host that said no in as many words, a 202 or a 429.
func (g *Guard) Trip(host string) { g.box(host, refusalCool) }

// Stumble is for a request that failed without the host refusing anything, like
// a timeout, a dead name, or a redirect to somewhere that wouldn't answer.
func (g *Guard) Stumble(host string) { g.box(host, stumbleCool) }

func (g *Guard) box(host string, cool time.Duration) {
	g.mu.Lock()
	g.trips[host]++
	till := time.Now().Add(cool)
	// A stumble never shortens a refusal already in force.
	if old, ok := g.till[host]; ok && old.After(till) {
		till = old
	}
	g.till[host] = till
	trips, store := g.trips[host], g.store
	g.mu.Unlock()
	if store != nil {
		store.SavePenalty(host, till, trips)
	}
}

// Restore puts back the boxes that outlived the last process and takes the
// store to write future ones to.
func (g *Guard) Restore(store PenaltyStore, saved map[string][2]int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.store = store
	for host, v := range saved {
		g.till[host] = time.UnixMilli(v[0])
		g.trips[host] = int(v[1])
	}
}

// OK clears the trip count on a call that worked, so one bad afternoon doesn't
// follow a host around for the rest of the process.
func (g *Guard) OK(host string) {
	g.mu.Lock()
	_, had := g.trips[host]
	delete(g.trips, host)
	store := g.store
	g.mu.Unlock()
	if had && store != nil {
		store.ClearPenalty(host)
	}
}

// Down is every host currently in the penalty box, so the page can say search
// is unavailable instead of each turn discovering it again.
func (g *Guard) Down() map[string]time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]time.Duration{}
	now := time.Now()
	for host, till := range g.till {
		if till.After(now) {
			out[host] = time.Until(till).Round(time.Second)
		}
	}
	return out
}

// SearchHost is the one whose loss the page reports, since a turn without it
// cannot look anything up and every other tool is narrower.
const SearchHost = "html.duckduckgo.com"
