package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// clearBugs plays a go out. It opens every safe pad, or with sting set
// opens that many taps and then steps on a wasp.
func clearBugs(t *testing.T, s *store, id int64, seed string, sting int) *bugTapped {
	t.Helper()
	g, err := s.bugStart(id)
	if err != nil {
		t.Fatal(err)
	}
	if seed == "" {
		var r bugRow
		if r, err = loadBugs(s.r, id); err != nil {
			t.Fatal(err)
		}
		seed = r.seed
	}
	b := bugDeal(seed, 0)
	open := map[int]bool{}
	taps := 0
	for i := 0; i < bugPads; i++ {
		pad := i
		if b.wasp[i] || open[i] {
			continue
		}
		if sting > 0 && taps == sting {
			for !b.wasp[pad] {
				pad++
			}
		}
		out, err := s.bugTap(id, g.Run, pad)
		if err != nil {
			t.Fatalf("pad %d: %v", pad, err)
		}
		taps++
		for _, o := range out.Open {
			open[o[0]] = true
		}
		if out.State != "play" {
			return out
		}
	}
	t.Fatal("the go never ended")
	return nil
}

func TestBugSweepDealIsTheDays(t *testing.T) {
	s, clock := testStore(t)
	day := dayOf(*clock)
	a, b := bugDeal(s.bugSeed(day), 0), bugDeal(s.bugSeed(day), 0)
	if a.wasp != b.wasp {
		t.Error("the same day dealt two grids")
	}
	if next := bugDeal(s.bugSeed(dayOf(clock.Add(24*time.Hour))), 0); next.wasp == a.wasp {
		t.Error("tomorrow's grid is today's")
	}
	// Two ponds that tap the same pad see the same pads open.
	_, one := newPond(t, s)
	_, two := newPond(t, s)
	var got [2]string
	for i, id := range []int64{one, two} {
		g, err := s.bugStart(id)
		if err != nil || !g.Daily || g.Cols*g.Rows != bugPads {
			t.Fatalf("the day's go: %v %+v", err, g)
		}
		out, err := s.bugTap(id, g.Run, 37)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(out.Open)
		got[i] = string(raw)
	}
	if got[0] != got[1] || got[0] == "[]" {
		t.Errorf("two ponds on the day's grid: %s and %s", got[0], got[1])
	}
}

func TestBugSweepFirstTapIsAlwaysSafe(t *testing.T) {
	s, clock := testStore(t)
	for d := range 40 {
		seed := s.bugSeed(dayOf(clock.Add(time.Duration(d) * 24 * time.Hour)))
		for first := range bugPads {
			b := bugDeal(seed, first)
			wasps := 0
			for _, w := range b.wasp {
				if w {
					wasps++
				}
			}
			if wasps != bugWasps {
				t.Fatalf("day %d from pad %d has %d wasps", d, first, wasps)
			}
			open, err := b.tap(first)
			if err != nil || b.state == bugLost || len(open) < 2 {
				t.Fatalf("day %d from pad %d: %v, state %d, %d pads opened", d, first, err, b.state, len(open))
			}
			if again := bugDeal(seed, first); again.wasp != b.wasp {
				t.Fatalf("day %d from pad %d dealt two ways", d, first)
			}
		}
	}
}

func TestBugSweepRefusesABadTap(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	g, err := s.bugStart(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.bugTap(id, "nope", 0); !errors.Is(err, errBadRun) {
		t.Errorf("a go that isn't the one in play: %v", err)
	}
	if _, err := s.bugTap(id, g.Run, bugPads); !errors.Is(err, errBadTap) {
		t.Errorf("a pad off the grid: %v", err)
	}
	out, err := s.bugTap(id, g.Run, 0)
	if err != nil || out.State != "play" {
		t.Fatalf("a first tap: %v %+v", err, out)
	}
	if _, err := s.bugTap(id, g.Run, out.Open[0][0]); !errors.Is(err, errBadTap) {
		t.Errorf("a tap on an open pad: %v", err)
	}
	// Coming back to it finds the same go with the same pads open.
	back, err := s.bugStart(id)
	if err != nil || back.Run != g.Run || len(back.Open) != len(out.Open) {
		t.Errorf("coming back to a go: %v %+v", err, back)
	}
	r, _ := loadBugs(s.r, id)
	b := bugDeal(r.seed, 0)
	wasp := 0
	for !b.wasp[wasp] {
		wasp++
	}
	if out, err = s.bugTap(id, g.Run, wasp); err != nil || out.State != "lost" || out.Hit != wasp || len(out.Wasps) != bugWasps {
		t.Fatalf("a tap on a wasp: %v %+v", err, out)
	}
	if _, err := s.bugTap(id, g.Run, bugPads-1); !errors.Is(err, errBadRun) {
		t.Errorf("a tap after a wasp: %v", err)
	}
	if _, err := s.arcadeStart(id, "sweep"); !errors.Is(err, errNotHeld) {
		t.Errorf("bug sweep started as a plain run: %v", err)
	}
	if _, err := s.arcadeFinish(id, "sweep", "", bugSafe, nil, nil); !errors.Is(err, errNotHeld) {
		t.Errorf("bug sweep took a score from the page: %v", err)
	}
}

func TestBugSweepPaysOnceADay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	g, _ := arcadeOf("sweep")
	// A game played first, so the frog isn't well rested for the prize.
	play(t, s, clock, id, "flick", 0)
	before, _ := s.view(id)
	out := clearBugs(t, s, id, "", 0)
	// The day's goals are met on the same go and pay inside the cap.
	day := min(g.cap, bugClear+len(out.Aims)*aimPebbles)
	if out.State != "won" || !out.Daily || out.Paid != day || out.Pads != bugSafe || out.Pond.Pebbles != before.Pebbles+day {
		t.Fatalf("clearing the day's grid: %+v", out)
	}
	if n := out.Pond.Gazette.Sweep; n == nil || n.State != "won" || n.Taps != out.Taps {
		t.Errorf("the paper after a clear: %+v", n)
	}
	for range 3 {
		if out = clearBugs(t, s, id, "", 0); out.Daily || out.Paid != 0 || out.Pond.Pebbles != before.Pebbles+day {
			t.Fatalf("a free go: %+v", out)
		}
	}
	for _, a := range out.Pond.Arcade {
		if a.Key == "sweep" && (a.Runs != 1 || a.Full != 1 || a.Paid != day || a.Best != bugSafe || !a.Daily || a.Day) {
			t.Errorf("bug sweep on the view: %+v", a)
		}
	}
	// A wasp on the next day's grid pays for the pads opened before it.
	*clock = clock.Add(24 * time.Hour)
	play(t, s, clock, id, "flick", 0)
	before, _ = s.view(id)
	out = clearBugs(t, s, id, "", 3)
	want := bugPart * out.Pads / bugSafe
	if out.State != "lost" || !out.Daily || out.Paid != want || want >= bugPart || out.Pond.Pebbles != before.Pebbles+want {
		t.Fatalf("a wasp on the day's grid: %+v, want %d", out, want)
	}
	if out = clearBugs(t, s, id, "", 0); out.Daily || out.Paid != len(out.Aims)*aimPebbles {
		t.Errorf("the go after a wasp pays only its goals: %+v", out)
	}
	// Well rested, the clear pays a quarter more and that's the cap.
	*clock = clock.Add(24 * time.Hour)
	if out = clearBugs(t, s, id, "", 0); out.Paid != g.cap || out.Rested != g.cap-min(g.cap, bugClear+len(out.Aims)*aimPebbles) {
		t.Errorf("a well rested clear paid %d with %d for the rest", out.Paid, out.Rested)
	}
}

// Nothing the server says before a go is over gives a wasp away.
func TestBugSweepKeepsTheWaspsToItself(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	g, err := s.bugStart(id)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := loadBugs(s.r, id)
	said := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if got := said(g); strings.Contains(got, "wasps\":[") || strings.Contains(got, r.seed) {
		t.Errorf("the start gives the grid away: %s", got)
	}
	b := bugDeal(r.seed, 0)
	open := map[int]bool{}
	for i := range bugPads {
		if b.wasp[i] || open[i] {
			continue
		}
		out, err := s.bugTap(id, g.Run, i)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range out.Open {
			open[o[0]] = true
			if b.wasp[o[0]] {
				t.Fatalf("pad %d opened with a wasp under it", o[0])
			}
		}
		if out.State != "play" {
			break
		}
		if out.Wasps != nil || out.Pond != nil || out.Hit != -1 || strings.Contains(said(out), "wasps") {
			t.Fatalf("a tap mid go said too much: %s", said(out))
		}
	}
	v, _ := s.view(id)
	if got := said(v); strings.Contains(got, r.seed) || strings.Contains(got, s.bugSeed(v.Today)) {
		t.Error("the pond's view carries the seed")
	}
}
