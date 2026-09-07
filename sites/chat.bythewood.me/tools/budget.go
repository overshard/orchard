package tools

import (
	"fmt"
	"sync"
	"time"
)

// A spend limit on the hosts that ban an address for asking too often.
//
// The gap between two calls was never the thing that got this address blocked.
// Six seconds of spacing was already in place and DuckDuckGo blocked it anyway,
// because a gap bounds the rate and nothing bounded the total. A model given
// six tool rounds and a nudge to keep going can spend an afternoon's worth of
// searches on one question and still honour every gap.
//
// So there are three ceilings and a call has to clear all of them. When one is
// spent, searching stops and says so, which is the honest answer and the one
// that lets the limit expire instead of renewing it.
//
// The numbers come from what the duckduckgo_search library and the people
// running into this recommend, held well under their ceiling rather than at it:
// that library asks for two seconds between calls and says to wait fifteen
// after an error, and the figure repeated for an address is to stay under
// thirty requests a minute. Nothing official is published, since scraping the
// HTML endpoint is against their terms in the first place, so the right posture
// is to be a light user rather than to find the edge.
type budget struct {
	gap    time.Duration
	minute int
	hour   int
	day    int
}

var budgets = map[string]budget{
	// Six seconds is already stricter than the two that library asks for, and
	// it is kept because a person asking one question does not notice it. The
	// pools below are the part that was missing.
	SearchHost:                 {gap: 6 * time.Second, minute: 8, hour: 45, day: 300},
	"cdn.espn.com":             {gap: 3 * time.Second, minute: 15, hour: 120, day: 900},
	"query1.finance.yahoo.com": {gap: 3 * time.Second, minute: 15, hour: 120, day: 900},
	"api.coingecko.com":        {gap: 2 * time.Second, minute: 20, hour: 200, day: 1500},
}

var defaultBudget = budget{gap: 400 * time.Millisecond, minute: 60, hour: 900, day: 8000}

func budgetFor(host string) budget {
	if b, ok := budgets[host]; ok {
		return b
	}
	return defaultBudget
}

// spend is one host's running count, kept as plain timestamps because a few
// hundred a day is nothing to hold and an exact window beats a decaying
// approximation when the whole point is not to go over.
type spend struct {
	at []time.Time
}

// trim drops what has aged out of the longest window, which is what keeps the
// slice from growing all day.
func (s *spend) trim(now time.Time) {
	cut := now.Add(-24 * time.Hour)
	i := 0
	for i < len(s.at) && s.at[i].Before(cut) {
		i++
	}
	if i > 0 {
		s.at = append(s.at[:0], s.at[i:]...)
	}
}

func (s *spend) since(now time.Time, d time.Duration) int {
	cut := now.Add(-d)
	n := 0
	for i := len(s.at) - 1; i >= 0; i-- {
		if s.at[i].Before(cut) {
			break
		}
		n++
	}
	return n
}

// left reports how much of each pool remains, and when the tightest spent one
// frees up. A zero duration means nothing is spent.
func (s *spend) left(now time.Time, b budget) (minute, hour, day int, free time.Duration) {
	minute = b.minute - s.since(now, time.Minute)
	hour = b.hour - s.since(now, time.Hour)
	day = b.day - s.since(now, 24*time.Hour)
	if len(s.at) == 0 {
		return
	}
	oldest := func(d time.Duration) time.Duration {
		cut := now.Add(-d)
		for _, t := range s.at {
			if !t.Before(cut) {
				return time.Until(t.Add(d))
			}
		}
		return 0
	}
	switch {
	case minute <= 0:
		free = oldest(time.Minute)
	case hour <= 0:
		free = oldest(time.Hour)
	case day <= 0:
		free = oldest(24 * time.Hour)
	}
	return
}

// Budgets holds every host's spend. It is separate from the penalty box: the
// box is what a host told us, and this is what we decided to allow ourselves.
type Budgets struct {
	mu  sync.Mutex
	by  map[string]*spend
	now func() time.Time
}

func NewBudgets() *Budgets {
	return &Budgets{by: map[string]*spend{}, now: time.Now}
}

func (b *Budgets) get(host string) *spend {
	s, ok := b.by[host]
	if !ok {
		s = &spend{}
		b.by[host] = s
	}
	return s
}

// Take records one call against a host, or refuses when a pool is spent. The
// refusal names which ceiling and when it frees, because "search is off" with
// no reason reads like a bug.
// A nil Budgets allows everything, so a Deps built by hand in a test does not
// panic in the one place every outbound call goes through.
func (b *Budgets) Take(host string) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	bud := budgetFor(host)
	s := b.get(host)
	s.trim(now)

	minute, hour, day, free := s.left(now, bud)
	switch {
	case day <= 0:
		return fmt.Errorf("the daily search budget for %s is spent (%d), and it frees up in %s",
			host, bud.day, round(free))
	case hour <= 0:
		return fmt.Errorf("the hourly search budget for %s is spent (%d), and it frees up in %s",
			host, bud.hour, round(free))
	case minute <= 0:
		return fmt.Errorf("this minute's search budget for %s is spent (%d), and it frees up in %s",
			host, bud.minute, round(free))
	}
	s.at = append(s.at, now)
	return nil
}

// Left is what the page reports, so a reader can see the pool draining rather
// than finding out when it is gone.
func (b *Budgets) Left(host string) (minute, hour, day int, free time.Duration) {
	if b == nil {
		return 0, 0, 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	s := b.get(host)
	s.trim(now)
	return s.left(now, budgetFor(host))
}

// Restore replays the timestamps the last process wrote, so a deploy does not
// hand the model a fresh day's allowance.
func (b *Budgets) Restore(host string, at []time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.get(host)
	s.at = append(s.at, at...)
	s.trim(b.now())
}

// Spent is what to persist, so the counts survive a restart the way the penalty
// box does.
func (b *Budgets) Spent(host string) []time.Time {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.get(host)
	s.trim(b.now())
	out := make([]time.Time, len(s.at))
	copy(out, s.at)
	return out
}

func round(d time.Duration) time.Duration {
	if d > time.Minute {
		return d.Round(time.Minute)
	}
	return d.Round(time.Second)
}
