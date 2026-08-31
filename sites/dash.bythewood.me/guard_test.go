package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGuardSpendsItsBudgetAndThenRefuses(t *testing.T) {
	g := NewGuard(t.TempDir())

	// yahoo is paced, so the pace has to be stepped over to reach the budget.
	budget := budgets["uptime"].perHour
	for i := 0; i < budget; i++ {
		if err := g.Allow("uptime"); err != nil {
			t.Fatalf("refused at call %d of %d: %v", i+1, budget, err)
		}
	}
	if err := g.Allow("uptime"); err == nil {
		t.Error("the call past the hourly budget was allowed")
	}
}

func TestGuardPacesConsecutiveCalls(t *testing.T) {
	g := NewGuard(t.TempDir())

	if err := g.Allow("yahoo"); err != nil {
		t.Fatalf("first call refused: %v", err)
	}
	if err := g.Allow("yahoo"); err == nil {
		t.Error("a second call inside the pace window was allowed")
	}
}

// 429 and 503 are the endpoint saying stop, so they open the breaker at once
// rather than after a failure streak.
func TestGuardOpensImmediatelyOnRateLimit(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		g := NewGuard(t.TempDir())
		if err := g.Allow("yahoo"); err != nil {
			t.Fatalf("status %d: first call refused: %v", status, err)
		}
		g.Fail("yahoo", status, 0)

		if err := g.Allow("yahoo"); err == nil {
			t.Errorf("status %d did not open the breaker", status)
		}
		if open := g.Status(); len(open) != 1 || open[0] != "yahoo" {
			t.Errorf("status %d reports %v open, want [yahoo]", status, open)
		}
	}
}

func TestGuardToleratesASingleTransportFailure(t *testing.T) {
	g := NewGuard(t.TempDir())
	g.Fail("uptime", 0, 0)

	if err := g.Allow("uptime"); err != nil {
		t.Errorf("one timeout closed the breaker: %v", err)
	}
	for i := 1; i < failsToTrip; i++ {
		g.Fail("uptime", 0, 0)
	}
	if err := g.Allow("uptime"); err == nil {
		t.Errorf("%d consecutive failures did not open the breaker", failsToTrip)
	}
}

func TestGuardTakesTheLongerOfBackoffAndRetryAfter(t *testing.T) {
	g := NewGuard(t.TempDir())
	g.Fail("yahoo", http.StatusTooManyRequests, 20*time.Minute)

	g.mu.Lock()
	open := g.entries["yahoo"].OpenUntil
	g.mu.Unlock()

	if wait := time.Until(open); wait < 19*time.Minute {
		t.Errorf("breaker opens for %s, want the 20 minute Retry-After", wait)
	}
}

// A breaker that forgets on restart is not a breaker, and a restart loop
// against an endpoint that just said 429 is the case it exists for.
func TestGuardStateSurvivesARestart(t *testing.T) {
	dir := t.TempDir()

	g := NewGuard(dir)
	g.Fail("yahoo", http.StatusTooManyRequests, 0)
	g.Flush()

	if _, err := os.Stat(filepath.Join(dir, "guard.json")); err != nil {
		t.Fatalf("nothing was written: %v", err)
	}

	if err := NewGuard(dir).Allow("yahoo"); err == nil {
		t.Error("a fresh guard reading the state on disk allowed the call anyway")
	}
}

func TestGuardIgnoresUnparseableState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guard.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Starting closed is the safe direction: it costs one request to find out
	// the endpoint is still angry, where starting open would refuse forever.
	if err := NewGuard(dir).Allow("yahoo"); err != nil {
		t.Errorf("a corrupt state file blocked every call: %v", err)
	}
}

func TestGuardRefusesAnEndpointWithNoBudget(t *testing.T) {
	if err := NewGuard(t.TempDir()).Allow("nobody"); err == nil {
		t.Error("an endpoint with no budget was allowed")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("120"); got != 2*time.Minute {
		t.Errorf("seconds form gave %s, want 2m", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("empty gave %s, want 0", got)
	}
	if got := parseRetryAfter("later"); got != 0 {
		t.Errorf("unparseable gave %s, want 0", got)
	}
	if got := parseRetryAfter("-5"); got != 0 {
		t.Errorf("negative gave %s, want 0", got)
	}

	// The HTTP date form, which RFC 9110 allows alongside a delay.
	future := time.Now().Add(10 * time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got < 9*time.Minute {
		t.Errorf("date form gave %s, want about 10m", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("a date in the past gave %s, want 0", got)
	}
}

// Pacing is a timing concern, not a breaker, so a poller waits it out. Three
// pollers share the Yahoo endpoint and fire together at boot; refusing two of
// them left their panels empty until their next tick, an hour and six hours
// later.
func TestGuardReserveWaitsOutThePace(t *testing.T) {
	g := NewGuard(t.TempDir())

	if err := g.Reserve(t.Context(), "yahoo"); err != nil {
		t.Fatalf("first reserve refused: %v", err)
	}

	start := time.Now()
	if err := g.Reserve(t.Context(), "yahoo"); err != nil {
		t.Fatalf("second reserve refused instead of waiting: %v", err)
	}
	if waited := time.Since(start); waited < budgets["yahoo"].pace/2 {
		t.Errorf("second reserve returned after %s, so it did not wait for the pace", waited)
	}
}

// Waiting does not help an open breaker or a spent budget, and a caller that
// queued on either would pile up behind a dead endpoint.
func TestGuardReserveStillRefusesTheBreaker(t *testing.T) {
	g := NewGuard(t.TempDir())
	g.Fail("yahoo", http.StatusTooManyRequests, 0)

	start := time.Now()
	if err := g.Reserve(t.Context(), "yahoo"); err == nil {
		t.Error("reserve waited on an open breaker instead of refusing")
	}
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("reserve took %s to refuse an open breaker", waited)
	}
}

func TestGuardReserveStillRefusesASpentBudget(t *testing.T) {
	g := NewGuard(t.TempDir())

	for i := 0; i < budgets["uptime"].perHour; i++ {
		if err := g.Reserve(t.Context(), "uptime"); err != nil {
			t.Fatalf("refused at call %d: %v", i+1, err)
		}
	}
	if err := g.Reserve(t.Context(), "uptime"); err == nil {
		t.Error("reserve waited on a spent budget instead of refusing")
	}
}

func TestGuardReserveHonoursContextCancellation(t *testing.T) {
	g := NewGuard(t.TempDir())
	if err := g.Reserve(t.Context(), "yahoo"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := g.Reserve(ctx, "yahoo"); err == nil {
		t.Error("reserve ignored a cancelled context and waited anyway")
	}
}
