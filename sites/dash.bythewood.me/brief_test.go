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
	compiled []string
}

func (f *fakeGateway) Structured(context.Context, string, string, map[string]any, int, any) error {
	return nil
}

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

// A slot that comes due runs every desk once, and nothing runs again until the
// next one.
func TestBriefRunsEveryDeskOncePerSlot(t *testing.T) {
	f := &fakeGateway{}
	b := testBriefer(t, f, false)
	now := inNY(t, "2026-10-07 07:00")
	b.tick(context.Background(), now)
	if strings.Join(f.compiled, ",") != "markets,news,feeds,forecast" {
		t.Fatalf("compiled %v", f.compiled)
	}
	b.tick(context.Background(), now.Add(119*time.Minute))
	if len(f.compiled) != len(briefDesks) {
		t.Errorf("ran again with nothing owed: %v", f.compiled)
	}
	b.tick(context.Background(), now.Add(2*time.Hour))
	if len(f.compiled) != 2*len(briefDesks) {
		t.Errorf("the 9:00 slot didn't run: %v", f.compiled)
	}
}

// A failed run tries again in a quarter hour and not every minute.
func TestBriefRetriesAfterAFailure(t *testing.T) {
	f := &fakeGateway{}
	b := testBriefer(t, f, true)
	now := inNY(t, "2026-10-07 16:05")
	b.tick(context.Background(), now)
	b.tick(context.Background(), now.Add(14*time.Minute))
	if len(f.compiled) != len(briefDesks) {
		t.Errorf("compiled %v before the retry was due", f.compiled)
	}
	b.tick(context.Background(), now.Add(15*time.Minute))
	if len(f.compiled) != 2*len(briefDesks) {
		t.Errorf("compiled %v, want a retry at 15 minutes", f.compiled)
	}
}
