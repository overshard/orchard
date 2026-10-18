package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func inNY(t *testing.T, v string) time.Time {
	t.Helper()
	at, err := time.ParseInLocation("2006-01-02 15:04", v, easternTime())
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func TestLatestSlot(t *testing.T) {
	cases := []struct{ now, kind, at string }{
		{"2026-10-07 06:59", "close", "2026-10-06 16:05"},
		{"2026-10-07 07:00", "morning", "2026-10-07 07:00"},
		{"2026-10-07 08:59", "morning", "2026-10-07 07:00"},
		{"2026-10-07 09:00", "preopen", "2026-10-07 09:00"},
		{"2026-10-07 10:30", "preopen", "2026-10-07 09:00"},
		{"2026-10-07 11:00", "midday", "2026-10-07 11:00"},
		{"2026-10-07 16:04", "midday", "2026-10-07 11:00"},
		{"2026-10-07 16:05", "close", "2026-10-07 16:05"},
		// Weekends run like any other day.
		{"2026-10-10 08:00", "morning", "2026-10-10 07:00"},
		{"2026-10-10 09:30", "preopen", "2026-10-10 09:00"},
		{"2026-10-12 06:00", "close", "2026-10-11 16:05"},
	}
	for _, c := range cases {
		slot, at := latestSlot(inNY(t, c.now))
		if slot.kind != c.kind || !at.Equal(inNY(t, c.at)) {
			t.Errorf("%s: got %s at %s, want %s at %s", c.now, slot.kind, at.Format("2006-01-02 15:04"), c.kind, c.at)
		}
	}
}

func TestTidy(t *testing.T) {
	got := tidy("Stocks fell [3, 4] after the Fed (2) — citing inflation [12]. ")
	if got != "Stocks fell after the Fed, citing inflation." {
		t.Errorf("got %q", got)
	}
}

type fakeGateway struct {
	gpu      GPUState
	gpuErr   error
	asked    int
	unloads  int
	compiled []string
}

func (f *fakeGateway) Structured(context.Context, string, string, map[string]any, int, any) error {
	return nil
}
func (f *fakeGateway) GPU(context.Context) (GPUState, error) { f.asked++; return f.gpu, f.gpuErr }
func (f *fakeGateway) Unload(context.Context) error          { f.unloads++; return nil }

func testBriefer(t *testing.T, f *fakeGateway, fail bool) *Briefer {
	t.Helper()
	b := NewBriefer(NewStore(NewHub()), nil, nil, t.TempDir())
	b.model = f
	b.compile = func(_ context.Context, desk string, slot briefSlot, at time.Time) (Brief, error) {
		f.compiled = append(f.compiled, desk)
		if fail {
			return Brief{}, errors.New("model fell over")
		}
		return Brief{Slot: at.Unix(), Title: slot.title(), Points: []Point{{Text: desk}}}, nil
	}
	return b
}

// Gaming on a Saturday: the 7am slot is owed, the card is busy, nothing loads,
// and the next ask is an hour later, not every minute.
func TestBriefDefersAnHourWhenTheCardIsBusy(t *testing.T) {
	f := &fakeGateway{gpu: GPUState{Busy: true, Reason: "card at 95%"}}
	b := testBriefer(t, f, false)
	now := inNY(t, "2026-10-10 07:00")

	for m := 0; m < 59; m++ {
		b.tick(context.Background(), now.Add(time.Duration(m)*time.Minute))
	}
	if f.asked != 1 || len(f.compiled) != 0 || f.unloads != 0 {
		t.Fatalf("in the first hour: asked %d, compiled %v, unloaded %d", f.asked, f.compiled, f.unloads)
	}
	if w := b.store.Snapshot().Briefs.News.Waiting; w != "CARD IN USE, NEXT TRY 08:00" {
		t.Errorf("waiting %q", w)
	}

	f.gpu = GPUState{Reason: "card idle"}
	b.tick(context.Background(), now.Add(time.Hour))
	if f.asked != 2 || strings.Join(f.compiled, ",") != "markets,news,feeds,glance" || f.unloads != 1 {
		t.Fatalf("at 8am: asked %d, compiled %v, unloaded %d", f.asked, f.compiled, f.unloads)
	}
	if w := b.store.Snapshot().Briefs.News.Waiting; w != "" {
		t.Errorf("still waiting %q after a run", w)
	}

	// Done for this slot, so nothing more until 9.
	b.tick(context.Background(), now.Add(119*time.Minute))
	if f.asked != 2 {
		t.Errorf("asked again with nothing owed")
	}
}

// A game started partway through a slot: the gateway turns the news desk away,
// nothing after it runs, the short desk isn't written, and the slot waits out
// what the gateway said.
func TestBriefStopsWhenTheGatewayTurnsACallAway(t *testing.T) {
	f := &fakeGateway{gpu: GPUState{Reason: "card idle"}}
	b := testBriefer(t, f, false)
	now := inNY(t, "2026-10-10 16:05")
	b.compile = func(ctx context.Context, desk string, slot briefSlot, at time.Time) (Brief, error) {
		f.compiled = append(f.compiled, desk)
		if desk == "news" {
			busy := CardBusy{Until: now.Add(time.Hour), Reason: "card full"}
			gateOf(ctx).refused(busy)
			return Brief{Slot: at.Unix(), Points: []Point{{Text: "short"}}}, &busy
		}
		return Brief{Slot: at.Unix(), Title: slot.title(), Points: []Point{{Text: desk}}}, nil
	}
	b.tick(context.Background(), now)
	if strings.Join(f.compiled, ",") != "markets,news" || f.unloads != 1 {
		t.Fatalf("compiled %v, unloaded %d", f.compiled, f.unloads)
	}
	briefs := b.store.Snapshot().Briefs
	if len(briefs.News.Points) != 0 || len(briefs.Markets.Points) != 1 {
		t.Errorf("news %v, markets %v", briefs.News.Points, briefs.Markets.Points)
	}
	if briefs.News.Waiting != "CARD IN USE, NEXT TRY 17:05" {
		t.Errorf("waiting %q", briefs.News.Waiting)
	}
	b.tick(context.Background(), now.Add(59*time.Minute))
	if f.asked != 1 {
		t.Errorf("asked again %d times before the hour was up", f.asked-1)
	}
}

// The wait after the gateway starts says when it ends, and the slot is tried
// then instead of an hour on.
func TestBriefWaitsUntilTheGatewaySays(t *testing.T) {
	now := inNY(t, "2026-10-10 07:00")
	f := &fakeGateway{gpu: GPUState{Busy: true, Reason: "the gateway just started", RetryAt: now.Add(10 * time.Minute).Unix()}}
	b := testBriefer(t, f, false)
	b.tick(context.Background(), now)
	if w := b.store.Snapshot().Briefs.News.Waiting; w != "CARD IN USE, NEXT TRY 07:10" {
		t.Errorf("waiting %q", w)
	}
	f.gpu = GPUState{Reason: "card idle"}
	b.tick(context.Background(), now.Add(10*time.Minute))
	if f.asked != 2 || len(f.compiled) != 4 {
		t.Errorf("asked %d, compiled %v", f.asked, f.compiled)
	}
}

// A model already on the card is somebody's chat, and pulling it would kill
// their turn, so the brief uses it and leaves it.
func TestBriefLeavesAResidentModelAlone(t *testing.T) {
	f := &fakeGateway{gpu: GPUState{Loaded: true}}
	b := testBriefer(t, f, false)
	b.tick(context.Background(), inNY(t, "2026-10-07 11:00"))
	if len(f.compiled) != 4 || f.unloads != 0 {
		t.Errorf("compiled %v, unloaded %d", f.compiled, f.unloads)
	}
}

// A failed run still unloads what it loaded, and tries again in a quarter hour.
func TestBriefUnloadsAndRetriesAfterAFailure(t *testing.T) {
	f := &fakeGateway{}
	b := testBriefer(t, f, true)
	now := inNY(t, "2026-10-07 16:05")
	b.tick(context.Background(), now)
	if f.unloads != 1 {
		t.Errorf("unloaded %d after a failed run", f.unloads)
	}
	b.tick(context.Background(), now.Add(14*time.Minute))
	b.tick(context.Background(), now.Add(15*time.Minute))
	if f.asked != 2 {
		t.Errorf("asked %d times, want a retry at 15 minutes", f.asked)
	}
}

// The gateway being down is not the card being busy, and is asked again sooner.
func TestBriefRetriesWhenTheGatewayIsDown(t *testing.T) {
	f := &fakeGateway{gpuErr: errors.New("connection refused")}
	b := testBriefer(t, f, false)
	now := inNY(t, "2026-10-07 07:00")
	b.tick(context.Background(), now)
	b.tick(context.Background(), now.Add(15*time.Minute))
	if f.asked != 2 || len(f.compiled) != 0 {
		t.Errorf("asked %d, compiled %v", f.asked, f.compiled)
	}
}
