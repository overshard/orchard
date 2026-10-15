package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"chat.bythewood.me/guard"
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
	guard *guard.Guard
	boxes *boxes
}

func NewGuard(cool time.Duration) *Guard {
	b := &boxes{mem: guard.Memory{}}
	return &Guard{guard: guard.New(b, policy), boxes: b}
}

// policy paces each host at its own gap and boxes it on the first failure. A
// stumble never shortens a refusal already in force, and the boxes are flat, so
// trips is counted and saved but never lengthens one.
func policy(host string) (guard.Policy, bool) {
	return guard.Policy{
		Gap:         budgetFor(host).gap,
		TripAfter:   1,
		OpenFor:     refusalCool,
		FailOpenFor: stumbleCool,
		KeepLonger:  true,
	}, true
}

// boxes holds every host's state in memory and hands the penalty box to the
// store once the guard has let go of its lock, so a slow write never holds up
// a call to another host.
type boxes struct {
	mem guard.Memory

	mu      sync.Mutex
	store   PenaltyStore
	pending []func(PenaltyStore)
}

func (b *boxes) Load(host string) (guard.State, error) { return b.mem.Load(host) }

func (b *boxes) Range(fn func(string, guard.State)) { b.mem.Range(fn) }

func (b *boxes) Save(host string, s guard.State, why string) {
	old := b.mem[host]
	b.mem.Save(host, s, why)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.store == nil {
		return
	}
	switch {
	case s.Trips > old.Trips:
		till, trips := s.OpenUntil, s.Trips
		b.pending = append(b.pending, func(p PenaltyStore) { p.SavePenalty(host, till, trips) })
	case s.Trips == 0 && old.Trips > 0:
		b.pending = append(b.pending, func(p PenaltyStore) { p.ClearPenalty(host) })
	}
}

func (b *boxes) flush() {
	b.mu.Lock()
	pending, store := b.pending, b.store
	b.pending = nil
	b.mu.Unlock()
	for _, write := range pending {
		write(store)
	}
}

// Wait blocks until this host may be called again. The lock is held across the
// sleep so two turns asking the same host queue instead of both deciding the gap
// has passed, so don't narrow it. A host boxed by another turn meanwhile is refused.
func (g *Guard) Wait(host string) error {
	err := g.guard.Wait(context.Background(), host, time.Now())
	if open, ok := err.(*guard.OpenError); ok {
		return refusedErr(host, open.Left)
	}
	return err
}

func (g *Guard) Blocked(host string) (bool, time.Duration) {
	return g.guard.Open(host, time.Now())
}

func refusedErr(host string, left time.Duration) error {
	return fmt.Errorf("%s refused us and is being left alone for another %s, "+
		"so nothing can be looked up there until then", host, round(left))
}

// Long and flat, since a short box asks again several times an hour against an
// endpoint that already said no, which turns a soft limit into a hard one, and
// nothing but a call says a ban has lifted.
const refusalCool = 6 * time.Hour

// A page that timed out isn't a host refusing us, so this is long enough to stop
// a turn hammering a dead address and short enough that the next question can try.
const stumbleCool = 5 * time.Minute

// Trip is for a host that said no in as many words, a 202 or a 429.
func (g *Guard) Trip(host string) {
	g.guard.Fail(host, time.Now(), guard.Failure{Kind: guard.Refused})
	g.boxes.flush()
}

// Stumble is for a request that failed without the host refusing anything, like
// a timeout, a dead name, or a redirect to somewhere that wouldn't answer.
func (g *Guard) Stumble(host string) {
	g.guard.Fail(host, time.Now(), guard.Failure{Kind: guard.Failed})
	g.boxes.flush()
}

// Restore puts back the boxes that outlived the last process and takes the
// store to write future ones to.
func (g *Guard) Restore(store PenaltyStore, saved map[string][2]int64) {
	for host, v := range saved {
		g.guard.Put(host, guard.State{OpenUntil: time.UnixMilli(v[0]), Trips: int(v[1])})
	}
	g.boxes.mu.Lock()
	g.boxes.store = store
	g.boxes.mu.Unlock()
}

// OK clears the trip count on a call that worked, so one bad afternoon doesn't
// follow a host around for the rest of the process. The box itself stays.
func (g *Guard) OK(host string) {
	g.guard.Succeed(host)
	g.boxes.flush()
}

// Down is every host currently in the penalty box, so the page can say search
// is unavailable instead of each turn discovering it again.
func (g *Guard) Down() map[string]time.Duration {
	out := map[string]time.Duration{}
	now := time.Now()
	g.guard.Each(func(host string, s guard.State) {
		if s.OpenUntil.After(now) {
			out[host] = time.Until(s.OpenUntil).Round(time.Second)
		}
	})
	return out
}

// SearchHost is the one whose loss the page reports, since a turn without it
// cannot look anything up and every other tool is narrower.
const SearchHost = "html.duckduckgo.com"
