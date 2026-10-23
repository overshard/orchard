package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLeanBookScoresClosedDaysOnly(t *testing.T) {
	et := easternTime()
	h := &history{}
	for _, b := range [][2]float64{{5, 100}, {6, 101}, {7, 100.5}, {8, 100}} {
		h.times = append(h.times, time.Date(2026, 10, int(b[0]), 9, 30, 0, 0, et).Unix())
		h.closes = append(h.closes, b[1])
	}

	lb := openLeanBook(t.TempDir())
	friday := Brief{Kind: "morning", Slot: time.Date(2026, 10, 9, 7, 0, 0, 0, et).Unix(), Points: []Point{
		{Label: "YESTERDAY", Lean: "down", Move: "0.50%"},
		{Label: "TODAY", Lean: "higher"},
		{Label: "MONDAY", Lean: "mixed"},
	}}
	thursday := Brief{Kind: "close", Slot: time.Date(2026, 10, 7, 16, 5, 0, 0, et).Unix(), Points: []Point{
		{Label: "TOMORROW", Lean: "lower"},
	}}
	if !lb.add(friday) || !lb.add(thursday) || lb.add(friday) {
		t.Fatal("add should record new slots once")
	}
	if len(lb.calls) != 3 {
		t.Fatalf("%d calls, want the three forward lines", len(lb.calls))
	}
	if lb.calls[1].Day != "2026-10-12" || lb.calls[2].Day != "2026-10-08" {
		t.Errorf("days %s and %s", lb.calls[1].Day, lb.calls[2].Day)
	}

	// Friday morning, so only Thursday's lean has a close to check against.
	if !lb.score(h, time.Date(2026, 10, 9, 8, 0, 0, 0, et)) {
		t.Fatal("nothing scored")
	}
	if c := lb.calls[2]; c.Dir != "down" || c.Move != "0.50%" {
		t.Errorf("thursday scored %s %s", c.Dir, c.Move)
	}
	if lb.calls[0].Dir != "" || lb.calls[1].Dir != "" {
		t.Error("scored a session that hasn't closed")
	}
	if r := lb.record(); r.hits != 1 || r.calls != 1 {
		t.Errorf("record %d of %d", r.hits, r.calls)
	}
	if lb.score(h, time.Date(2026, 10, 9, 8, 1, 0, 0, et)) {
		t.Error("scored the same lean twice")
	}

	// Monday comes and goes with no bar for Friday, which reads as a holiday.
	h.times = append(h.times, time.Date(2026, 10, 12, 9, 30, 0, 0, et).Unix())
	h.closes = append(h.closes, 100.05)
	lb.score(h, time.Date(2026, 10, 13, 7, 0, 0, 0, et))
	if lb.calls[0].Dir != "closed" || lb.calls[1].Dir != "flat" {
		t.Errorf("friday %q, monday %q", lb.calls[0].Dir, lb.calls[1].Dir)
	}
	if r := lb.record(); r.hits != 1 || r.calls != 1 || r.passes != 1 || r.upCalls != 0 {
		t.Errorf("record %+v, want the mixed monday a pass and not a call", r)
	}

	lb.save()
	if got := openLeanBook(lb.path[:len(lb.path)-len("/leans.json")]); len(got.calls) != 3 || got.calls[1].Dir != "flat" {
		t.Errorf("read back %+v", got.calls)
	}
}

func TestLeanBookScoresTheWeekAhead(t *testing.T) {
	et := easternTime()
	h := &history{}
	for _, b := range [][2]float64{{2, 100}, {5, 101}, {8, 99}, {9, 98}} {
		h.times = append(h.times, time.Date(2026, 10, int(b[0]), 9, 30, 0, 0, et).Unix())
		h.closes = append(h.closes, b[1])
	}
	lb := openLeanBook(t.TempDir())
	lb.add(Brief{Kind: "midday", Slot: time.Date(2026, 10, 4, 11, 0, 0, 0, et).Unix(), Points: []Point{
		{Label: "LAST WEEK", Lean: "up", Move: "1.00%"},
		{Label: weekAhead, Lean: "lower"},
	}})
	if len(lb.calls) != 1 || lb.calls[0].From != "2026-10-05" || lb.calls[0].Day != "2026-10-09" {
		t.Fatalf("calls %+v", lb.calls)
	}
	if lb.score(h, time.Date(2026, 10, 9, 20, 0, 0, 0, et)) {
		t.Error("scored the week before Friday's close was trusted")
	}
	lb.score(h, time.Date(2026, 10, 10, 9, 0, 0, 0, et))
	if c := lb.calls[0]; c.Dir != "down" || c.Move != "2.00%" {
		t.Errorf("week scored %s %s, want the 2nd to the 9th", c.Dir, c.Move)
	}
}

func TestPulse(t *testing.T) {
	for _, c := range []struct {
		top, outlets int
		want         string
	}{
		{8, 11, "major"}, {7, 11, "steady"}, {5, 11, "steady"}, {4, 11, "quiet"}, {6, 8, "major"}, {1, 0, "steady"},
	} {
		if got, _ := pulse(c.top, c.outlets); got != c.want {
			t.Errorf("%d of %d: %s, want %s", c.top, c.outlets, got, c.want)
		}
	}
}

func TestArchiveKeepsEachSlotOnce(t *testing.T) {
	dir := t.TempDir()
	a := openArchive(dir)
	b := Brief{Slot: 100, Points: []Point{{Text: "x"}}, Waiting: "CARD IN USE"}
	if a.has("news", 100) {
		t.Fatal("empty archive has a slot")
	}
	a.add("news", b)
	a.add("markets", Brief{Slot: 100})
	if !a.has("news", 100) || a.has("markets", 100) || a.has("news", 101) {
		t.Error("has disagrees with what was added")
	}

	// A restart reads the saved briefs back and should not add them again.
	saved, _ := json.Marshal(Briefs{News: b})
	os.WriteFile(filepath.Join(dir, "briefs.json"), saved, 0o644)
	NewBriefer(NewStore(NewHub()), nil, nil, dir)
	raw, _ := os.ReadFile(a.path)
	if n := strings.Count(string(raw), "\n"); n != 1 {
		t.Errorf("%d lines, want 1", n)
	}
	if strings.Contains(string(raw), "CARD IN USE") {
		t.Error("archived the waiting note")
	}
}

func TestMostImpactKeepsAMarketStoryFiledAsAnIncident(t *testing.T) {
	futures := cluster{stories: []story{{source: "REUTERS", title: "US futures fall after explosions in Riyadh"}}}
	poland := cluster{stories: []story{{source: "REUTERS", title: "School attack"}}}
	got := mostImpact([]cluster{futures, poland}, []eventRating{{1, "incident", 4}, {2, "incident", 3}}, 12)
	if len(got) != 1 || got[0].lead().title != futures.lead().title {
		t.Errorf("kept %d", len(got))
	}
}

func TestWhole(t *testing.T) {
	pad := strings.Repeat("word ", 17)
	for _, c := range []struct{ in, want string }{
		{"Fed held rates.", "Fed held rates."},
		{"Fed held rates", "Fed held rates."},
		{pad + "and the U.S.", ""},
		{"short, " + pad + "and some residents fled as wi", ""},
		{pad + "now quitting,", pad + "now quitting."},
		{pad + "rather than uploads, while others compare it to minimal", pad + "rather than uploads."},
		{"U.S. prosecutors charged him. " + pad + "and", "U.S. prosecutors charged him."},
		{pad + "and it rained.", pad + "and it rained."},
	} {
		if got := whole(c.in, readMax); got != c.want {
			t.Errorf("whole(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLeanRecordCountsTheLastCallPerSession(t *testing.T) {
	et := easternTime()
	h := &history{}
	for _, b := range [][2]float64{{8, 100}, {9, 101}} {
		h.times = append(h.times, time.Date(2026, 10, int(b[0]), 9, 30, 0, 0, et).Unix())
		h.closes = append(h.closes, b[1])
	}
	lb := openLeanBook(t.TempDir())
	lb.add(Brief{Kind: "close", Slot: time.Date(2026, 10, 8, 16, 5, 0, 0, et).Unix(), Points: []Point{
		{Label: "TOMORROW", Lean: "lower", Text: "a"},
	}})
	lb.add(Brief{Kind: "morning", Slot: time.Date(2026, 10, 9, 7, 0, 0, 0, et).Unix(), Points: []Point{
		{Label: "TODAY", Lean: "higher", Text: "b"},
	}})
	lb.score(h, time.Date(2026, 10, 10, 9, 0, 0, 0, et))
	if lb.calls[0].Dir != "up" || lb.calls[1].Dir != "up" {
		t.Fatalf("both calls should still be scored, got %+v", lb.calls)
	}
	if r := lb.record(); r.hits != 1 || r.calls != 1 || r.upCalls != 1 {
		t.Errorf("record %+v, want only the morning call", r)
	}
}

func TestLeanBookSkipsAHeldLine(t *testing.T) {
	et := easternTime()
	lb := openLeanBook(t.TempDir())
	held := Point{Label: weekAhead, Lean: "higher", Text: "CPI lands Wednesday."}
	lb.add(Brief{Kind: "morning", Slot: time.Date(2026, 10, 10, 7, 0, 0, 0, et).Unix(), Points: []Point{held}})
	if lb.add(Brief{Kind: "midday", Slot: time.Date(2026, 10, 10, 11, 0, 0, 0, et).Unix(), Points: []Point{held}}) {
		t.Error("recorded the same line again")
	}
}

func TestHeldLineWantsTheSamePricesAndSession(t *testing.T) {
	et := easternTime()
	sat := time.Date(2026, 10, 10, 7, 0, 0, 0, et).Unix()
	sun := time.Date(2026, 10, 11, 16, 5, 0, 0, et).Unix()
	prev := Brief{Slot: sat, Basis: "7,859.75", Points: []Point{
		{Label: "LAST WEEK", Lean: "up", Move: "1.00%"},
		{Label: weekAhead, Lean: "higher", Text: "CPI lands Wednesday."},
	}}

	if pt, ok := heldLine(prev, Brief{Slot: sun, Basis: "7,859.75"}, weekAhead); !ok || pt.Lean != "higher" {
		t.Errorf("weekend with no new prices should hold, got %+v %v", pt, ok)
	}
	if _, ok := heldLine(prev, Brief{Slot: sun, Basis: "7,881.00"}, weekAhead); ok {
		t.Error("held a line after the futures moved")
	}
	friday := Brief{Slot: time.Date(2026, 10, 9, 16, 5, 0, 0, et).Unix(), Basis: "7,859.75",
		Points: []Point{{Label: "MONDAY", Lean: "lower"}}}
	if _, ok := heldLine(friday, Brief{Slot: sat, Basis: "7,859.75"}, weekAhead); ok {
		t.Error("held a line from a different label")
	}
	if _, ok := heldLine(Brief{Slot: sat, Points: prev.Points}, Brief{Slot: sun}, weekAhead); ok {
		t.Error("held with no prices to compare")
	}
}

func TestClosedLineTellsTheModelTheDirection(t *testing.T) {
	if l := closedLine("LAST WEEK", 101, 100, "the reasons"); !strings.HasSuffix(l.ask, "was up 1.00%") {
		t.Errorf("ask %q", l.ask)
	}
	if l := closedLine("", 99, 100, ""); l.ask != "" {
		t.Errorf("scoring line got an ask %q", l.ask)
	}
}
