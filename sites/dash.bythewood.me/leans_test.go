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
	if hits, calls := lb.record(); hits != 1 || calls != 1 {
		t.Errorf("record %d of %d", hits, calls)
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
	if hits, calls := lb.record(); hits != 2 || calls != 2 {
		t.Errorf("record %d of %d, want the flat monday a mixed hit", hits, calls)
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
