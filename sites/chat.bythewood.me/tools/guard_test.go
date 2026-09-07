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

// A host that refuses is left alone for hours and not for ten minutes, because
// a short box means asking again six times an hour and every one of those is a
// request to something that already said no.
func TestGuardLeavesARefusingHostAlone(t *testing.T) {
	g := NewGuard(10 * time.Minute)
	g.Trip("example.com")
	blocked, left := g.Blocked("example.com")
	if !blocked {
		t.Fatal("a host that refused is not boxed")
	}
	if left < refusalCool-time.Minute {
		t.Errorf("boxed for only %s, want about %s", left, refusalCool)
	}
	// Flat rather than escalating, so nothing has to be poked to find the step.
	g.Trip("example.com")
	if _, again := g.Blocked("example.com"); again > refusalCool+time.Minute {
		t.Errorf("a second refusal stretched the box to %s", again)
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
