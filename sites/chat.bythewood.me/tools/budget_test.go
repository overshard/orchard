package tools

import (
	"strings"
	"testing"
	"time"
)

// A clock the test drives, since the whole thing is about windows and sleeping
// through an hour is not a test.
// A budgeted host, picked for its pools being small enough to spend in a loop.
const espnHost = "cdn.espn.com"

func atTime(b *Budgets, t *time.Time) { b.now = func() time.Time { return *t } }

// A gap alone leaves the total unbounded, so a long turn could spend an
// afternoon's searches on one question and honour every gap doing it.
func TestBudgetStopsAtTheMinutePool(t *testing.T) {
	now := time.Now()
	b := NewBudgets()
	atTime(b, &now)
	want := budgetFor(espnHost).minute

	for i := range want {
		if err := b.Take(espnHost); err != nil {
			t.Fatalf("call %d of %d refused early: %v", i+1, want, err)
		}
	}
	err := b.Take(espnHost)
	if err == nil {
		t.Fatal("the pool did not stop the next call")
	}
	if !strings.Contains(err.Error(), "frees up in") {
		t.Errorf("the refusal does not say when it clears: %v", err)
	}

	// It frees as the window slides, without anything being poked to find out.
	now = now.Add(61 * time.Second)
	if err := b.Take(espnHost); err != nil {
		t.Errorf("the minute pool did not free up: %v", err)
	}
}

func TestBudgetStopsAtTheHourAndDayPools(t *testing.T) {
	now := time.Now()
	b := NewBudgets()
	atTime(b, &now)
	bud := budgetFor(espnHost)

	// Spend the hour, a minute's worth at a time so the tighter pool never bites.
	for i := 0; i < bud.hour; i++ {
		if err := b.Take(espnHost); err != nil {
			t.Fatalf("call %d refused early: %v", i+1, err)
		}
		if (i+1)%bud.minute == 0 {
			now = now.Add(61 * time.Second)
		}
	}
	if err := b.Take(espnHost); err == nil || !strings.Contains(err.Error(), "hourly") {
		t.Fatalf("the hour pool did not bite, got %v", err)
	}

	// An hour on, the hour pool is clear and the day pool is still counting.
	now = now.Add(61 * time.Minute)
	if err := b.Take(espnHost); err != nil {
		t.Errorf("the hour pool did not free up: %v", err)
	}
	if _, _, day, _ := b.Left(espnHost); day >= bud.day {
		t.Errorf("the day pool reset with the hour, %d left of %d", day, bud.day)
	}
}

// A deploy must not hand the model a fresh allowance.
func TestBudgetSurvivesARestart(t *testing.T) {
	now := time.Now()
	b := NewBudgets()
	atTime(b, &now)
	for range 5 {
		_ = b.Take(espnHost)
	}
	spent := b.Spent(espnHost)
	if len(spent) != 5 {
		t.Fatalf("recorded %d calls, want 5", len(spent))
	}

	fresh := NewBudgets()
	atTime(fresh, &now)
	fresh.Restore(espnHost, spent)
	_, _, _, _ = fresh.Left(espnHost)
	if m, _, _, _ := fresh.Left(espnHost); m != budgetFor(espnHost).minute-5 {
		t.Errorf("after a restart the minute pool shows %d left, want %d",
			m, budgetFor(espnHost).minute-5)
	}
}

// Anything a day old stops counting and stops being held.
func TestBudgetForgetsYesterday(t *testing.T) {
	now := time.Now()
	b := NewBudgets()
	atTime(b, &now)
	for range 5 {
		_ = b.Take(espnHost)
	}
	now = now.Add(25 * time.Hour)
	if _, _, day, _ := b.Left(espnHost); day != budgetFor(espnHost).day {
		t.Errorf("yesterday's calls are still counted, %d left", day)
	}
	if got := len(b.Spent(espnHost)); got != 0 {
		t.Errorf("yesterday's timestamps are still held, %d of them", got)
	}
}
