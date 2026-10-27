package tools

import (
	"fmt"
	"sync"
	"time"
)

// budget is a spend limit for hosts that ban an address for asking too often. A
// gap only bounds the rate and DuckDuckGo blocked us with every gap honoured, so a
// spent ceiling stops the calls and lets the host's limit expire instead of renewing it.
type budget struct {
	gap    time.Duration
	minute int
	hour   int
	day    int
}

var budgets = map[string]budget{
	// Well under the two second gap and thirty a minute the duckduckgo_search
	// library suggests, since DuckDuckGo publishes nothing official.
	SearchHost:                 {gap: 6 * time.Second, minute: 8, hour: 30, day: 200},
	"cdn.espn.com":             {gap: 3 * time.Second, minute: 15, hour: 120, day: 900},
	"query1.finance.yahoo.com": {gap: 3 * time.Second, minute: 15, hour: 120, day: 900},
	"api.coingecko.com":        {gap: 2 * time.Second, minute: 20, hour: 200, day: 1500},
	// The widget hosts. A reader flipping a chart between ranges makes a call
	// each in a few seconds, so the gap is small and the ceiling stops a page left
	// open from becoming a poller.
	"query2.finance.yahoo.com":       {gap: 3 * time.Second, minute: 15, hour: 120, day: 900},
	"api.open-meteo.com":             {gap: 500 * time.Millisecond, minute: 40, hour: 400, day: 3000},
	"air-quality-api.open-meteo.com": {gap: 500 * time.Millisecond, minute: 40, hour: 400, day: 3000},
	// A county's web server, and dash already asks it every three hours.
	"www.co.forsyth.nc.us": {gap: 2 * time.Second, minute: 10, hour: 60, day: 400},
	// Nominatim's usage policy is one request a second at most.
	"nominatim.openstreetmap.org": {gap: 1500 * time.Millisecond, minute: 10, hour: 60, day: 300},
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
// approximation when the job is not to go over.
type spend struct {
	at []time.Time
}

// trim drops what has aged out of the longest window so the slice doesn't grow
// all day.
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

// Budgets holds the limits we set ourselves, kept apart from the penalty box,
// which holds what a host told us.
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

// Take records one call against a host, or refuses naming the spent ceiling and
// when it frees, since "search is off" with no reason reads like a bug. A nil
// Budgets allows everything so a Deps built by hand in a test doesn't panic.
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

// Left feeds the page, so a reader can see the pool draining instead
// of finding out when it's gone.
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

// Restore replays the timestamps the last process wrote, so a deploy doesn't
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

// Spent is persisted so the counts survive a restart the way the penalty box
// does.
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
