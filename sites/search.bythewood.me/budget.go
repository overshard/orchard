package main

import (
	"context"
	"sync"
	"time"
)

// Budget is a self-imposed search rate limit.
//
// DuckDuckGo publishes no threshold. What is documented is community measured:
// stay under 30 requests a minute from one address, detection fires well before
// that, and roughly one request a second is the sustainable pace. So this
// budgets conservatively against a number DDG never stated, and the point is
// less to avoid a 202 than to make the ceiling visible, since hitting one with
// no warning reads as the tool being broken.
//
// A question spends one to three searches, so the window is counted in searches
// and reported in both searches and whole questions.
const (
	budgetWindow  = 10 * time.Minute
	budgetMax     = 24
	perQuestion   = 3
	cooldownAfter = 90 * time.Second

	// One search a second is the pace community reports say DuckDuckGo
	// tolerates. It is enforced across the whole process rather than within one
	// question, because three questions asked back to back are the same burst
	// to DDG as one question firing three queries.
	minGap = 1100 * time.Millisecond
)

type Budget struct {
	mu      sync.Mutex
	spent   []time.Time
	limited time.Time // when a 202 was last seen

	// pace serialises searches so the gap between any two is at least minGap,
	// whoever asked for them.
	pace     sync.Mutex
	lastFire time.Time
}

// Wait blocks until another search may go out, and reports how long it waited
// so the page can say why it is taking a moment rather than looking stuck.
func (b *Budget) Wait(ctx context.Context) time.Duration {
	b.pace.Lock()
	defer b.pace.Unlock()

	wait := minGap - time.Since(b.lastFire)
	if cooling, left := b.Cooling(); cooling && left > wait {
		wait = left
	}
	if wait <= 0 {
		b.lastFire = time.Now()
		return 0
	}
	select {
	case <-ctx.Done():
	case <-time.After(wait):
	}
	b.lastFire = time.Now()
	return wait
}

func NewBudget() *Budget { return &Budget{} }

// BudgetState is what the UI shows.
type BudgetState struct {
	Used      int    `json:"used"`
	Max       int    `json:"max"`
	Left      int    `json:"left"`
	Questions int    `json:"questions"`
	Cooling   bool   `json:"cooling"`
	ResetIn   int    `json:"resetIn"`
	Note      string `json:"note"`
}

func (b *Budget) prune(now time.Time) {
	cut := now.Add(-budgetWindow)
	keep := b.spent[:0]
	for _, t := range b.spent {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	b.spent = keep
}

// Spend records one search.
func (b *Budget) Spend() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.prune(now)
	b.spent = append(b.spent, now)
}

// Limited records that DuckDuckGo answered 202, which starts a cooldown.
func (b *Budget) Limited() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.limited = time.Now()
}

// Cooling reports whether a recent 202 means searching should wait.
func (b *Budget) Cooling() (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limited.IsZero() {
		return false, 0
	}
	left := cooldownAfter - time.Since(b.limited)
	if left <= 0 {
		return false, 0
	}
	return true, left
}

func (b *Budget) State() BudgetState {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.prune(now)

	st := BudgetState{Used: len(b.spent), Max: budgetMax}
	st.Left = budgetMax - st.Used
	if st.Left < 0 {
		st.Left = 0
	}
	st.Questions = st.Left / perQuestion

	if !b.limited.IsZero() {
		if left := cooldownAfter - time.Since(b.limited); left > 0 {
			st.Cooling = true
			st.ResetIn = int(left.Seconds()) + 1
			st.Note = "the search source asked us to slow down, waiting it out"
			return st
		}
	}
	// When the window is full, the oldest search leaving it is when room opens.
	if st.Left == 0 && len(b.spent) > 0 {
		st.ResetIn = int(budgetWindow-time.Since(b.spent[0])) + 1
		st.Note = "search budget spent, room opens as the window rolls"
	} else if st.Questions <= 1 {
		st.Note = "close to the search budget"
	}
	return st
}
