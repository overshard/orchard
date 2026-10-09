package main

import (
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
