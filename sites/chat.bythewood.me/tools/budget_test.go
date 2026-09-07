package tools

import (
	"strings"
	"testing"
	"time"
)

// A clock the test drives, since the whole thing is about windows and sleeping
// through an hour is not a test.
func atTime(b *Budgets, t *time.Time) { b.now = func() time.Time { return *t } }

// The gap was never what got this address blocked. Six seconds of spacing was
// already in place and the total was unbounded, so a long turn could spend an
// afternoon's searches on one question and honour every gap doing it.
func TestBudgetStopsAtTheMinutePool(t *testing.T) {
	now := time.Now()
	b := NewBudgets()
	atTime(b, &now)
	want := budgetFor(SearchHost).minute

	for i := 0; i < want; i++ {
		if err := b.Take(SearchHost); err != nil {
			t.Fatalf("call %d of %d refused early: %v", i+1, want, err)
		}
	}
	err := b.Take(SearchHost)
	if err == nil {
		t.Fatal("the pool did not stop the next call")
	}
	if !strings.Contains(err.Error(), "frees up in") {
		t.Errorf("the refusal does not say when it clears: %v", err)
	}

	// It frees as the window slides, without anything being poked to find out.
	now = now.Add(61 * time.Second)
	if err := b.Take(SearchHost); err != nil {
		t.Errorf("the minute pool did not free up: %v", err)
	}
}

func TestBudgetStopsAtTheHourAndDayPools(t *testing.T) {
	now := time.Now()
	b := NewBudgets()
	atTime(b, &now)
	bud := budgetFor(SearchHost)

	// Spend the hour, a minute's worth at a time so the tighter pool never bites.
	for i := 0; i < bud.hour; i++ {
		if err := b.Take(SearchHost); err != nil {
			t.Fatalf("call %d refused early: %v", i+1, err)
		}
		if (i+1)%bud.minute == 0 {
			now = now.Add(61 * time.Second)
		}
	}
	if err := b.Take(SearchHost); err == nil || !strings.Contains(err.Error(), "hourly") {
		t.Fatalf("the hour pool did not bite, got %v", err)
	}

	// An hour on, the hour pool is clear and the day pool is still counting.
	now = now.Add(61 * time.Minute)
	if err := b.Take(SearchHost); err != nil {
		t.Errorf("the hour pool did not free up: %v", err)
	}
	if _, _, day, _ := b.Left(SearchHost); day >= bud.day {
		t.Errorf("the day pool reset with the hour, %d left of %d", day, bud.day)
	}
}

// A deploy must not hand the model a fresh allowance, which is the same mistake
// the penalty box made before it was persisted.
func TestBudgetSurvivesARestart(t *testing.T) {
	now := time.Now()
	b := NewBudgets()
	atTime(b, &now)
	for i := 0; i < 5; i++ {
		_ = b.Take(SearchHost)
	}
	spent := b.Spent(SearchHost)
	if len(spent) != 5 {
		t.Fatalf("recorded %d calls, want 5", len(spent))
	}

	fresh := NewBudgets()
	atTime(fresh, &now)
	fresh.Restore(SearchHost, spent)
	_, _, _, _ = fresh.Left(SearchHost)
	if m, _, _, _ := fresh.Left(SearchHost); m != budgetFor(SearchHost).minute-5 {
		t.Errorf("after a restart the minute pool shows %d left, want %d",
			m, budgetFor(SearchHost).minute-5)
	}
}

// Anything a day old stops counting and stops being held.
func TestBudgetForgetsYesterday(t *testing.T) {
	now := time.Now()
	b := NewBudgets()
	atTime(b, &now)
	for i := 0; i < 5; i++ {
		_ = b.Take(SearchHost)
	}
	now = now.Add(25 * time.Hour)
	if _, _, day, _ := b.Left(SearchHost); day != budgetFor(SearchHost).day {
		t.Errorf("yesterday's calls are still counted, %d left", day)
	}
	if got := len(b.Spent(SearchHost)); got != 0 {
		t.Errorf("yesterday's timestamps are still held, %d of them", got)
	}
}

// The search host has to be the strictest thing here, since it is the one that
// has actually banned this address.
func TestSearchIsTheStrictestBudget(t *testing.T) {
	s, d := budgetFor(SearchHost), defaultBudget
	if s.minute >= d.minute || s.hour >= d.hour || s.day >= d.day {
		t.Errorf("search budget %+v is not stricter than the default %+v", s, d)
	}
	// Under the figure people repeat for an address, not at it.
	if s.minute > 30 {
		t.Errorf("minute pool of %d is at or past the 30 a minute people warn about", s.minute)
	}
	// At or above what the duckduckgo_search library asks between calls.
	if s.gap < 2*time.Second {
		t.Errorf("gap of %s is under the 2s that library recommends", s.gap)
	}
}
