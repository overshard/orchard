package tools

import (
	"testing"
	"time"
)

type fakeStore struct {
	saved   map[string]int
	cleared []string
}

func (f *fakeStore) SavePenalty(host string, till time.Time, trips int) {
	if f.saved == nil {
		f.saved = map[string]int{}
	}
	f.saved[host] = trips
}
func (f *fakeStore) ClearPenalty(host string) { f.cleared = append(f.cleared, host) }

// Asking again the moment a ten minute box empties is what renews a ban rather
// than letting it expire, so each repeat doubles the wait.
func TestGuardBacksOffOnRepeatTrips(t *testing.T) {
	g := NewGuard(10 * time.Minute)
	var last time.Duration
	for i := 1; i <= 4; i++ {
		g.Trip("example.com")
		_, left := g.Blocked("example.com")
		if left <= last {
			t.Fatalf("trip %d waited %s, no longer than the %s before it", i, left, last)
		}
		last = left
	}
	// And it stops somewhere, rather than doubling into next week.
	for i := 0; i < 20; i++ {
		g.Trip("example.com")
	}
	if _, left := g.Blocked("example.com"); left > maxCool {
		t.Errorf("backoff ran to %s, past the %s ceiling", left, maxCool)
	}
}

// One bad afternoon must not leave a host on a four hour backoff for the rest
// of the process, so a call that worked resets the streak.
func TestGuardOKResetsTheStreak(t *testing.T) {
	store := &fakeStore{}
	g := NewGuard(10 * time.Minute)
	g.Restore(store, nil)

	g.Trip("example.com")
	g.Trip("example.com")
	g.OK("example.com")
	if len(store.cleared) != 1 || store.cleared[0] != "example.com" {
		t.Errorf("a host that answered was not cleared: %v", store.cleared)
	}

	// Back to the first step rather than continuing to double.
	g.Trip("example.com")
	_, left := g.Blocked("example.com")
	if left > 10*time.Minute+time.Second {
		t.Errorf("after a good call the next trip waited %s, want the base cool", left)
	}
}

// A deploy used to empty the penalty box, so the next turn asked a host that
// was still refusing. That is the surest way to keep a rate limit alive.
func TestGuardSurvivesARestart(t *testing.T) {
	store := &fakeStore{}
	g := NewGuard(10 * time.Minute)
	g.Restore(store, nil)
	g.Trip(SearchHost)
	if store.saved[SearchHost] != 1 {
		t.Fatalf("the trip was not persisted: %v", store.saved)
	}

	// A new process, handed what the last one wrote.
	fresh := NewGuard(10 * time.Minute)
	fresh.Restore(store, map[string][2]int64{
		SearchHost: {time.Now().Add(30 * time.Minute).UnixMilli(), 3},
	})
	blocked, left := fresh.Blocked(SearchHost)
	if !blocked {
		t.Fatal("a restart cleared the penalty box")
	}
	if left < 25*time.Minute {
		t.Errorf("restored box has %s left, want about 30 minutes", left)
	}
	// The streak came back too, so the next trip keeps escalating.
	fresh.Trip(SearchHost)
	if _, l := fresh.Blocked(SearchHost); l < time.Hour {
		t.Errorf("the trip count did not survive, next wait was only %s", l)
	}
}

// Down is what the page reads to say search is unavailable.
func TestGuardDownListsOnlyLiveBoxes(t *testing.T) {
	g := NewGuard(10 * time.Minute)
	g.Trip(SearchHost)
	down := g.Down()
	if _, ok := down[SearchHost]; !ok {
		t.Errorf("a tripped host is not reported down: %v", down)
	}
	if _, ok := down["never-called.example"]; ok {
		t.Error("a host that was never tripped is reported down")
	}
}
