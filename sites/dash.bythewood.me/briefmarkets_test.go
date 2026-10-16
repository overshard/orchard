package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMarketLines(t *testing.T) {
	et := easternTime()
	bar := func(d int, close float64) (int64, float64) {
		return time.Date(2026, 10, d, 9, 30, 0, 0, et).Unix(), close
	}
	h := &history{}
	for _, b := range [][2]float64{{1, 100}, {2, 101}, {5, 100}, {6, 102}, {7, 101}, {8, 101.5}} {
		ts, c := bar(int(b[0]), b[1])
		h.times, h.closes = append(h.times, ts), append(h.closes, c)
	}
	type want struct{ label, lean, move string }
	check := func(name string, got []marketLine, w []want) {
		t.Helper()
		if len(got) != len(w) {
			t.Fatalf("%s: %d lines, want %d", name, len(got), len(w))
		}
		for i := range w {
			if got[i].label != w[i].label || got[i].dir != w[i].lean || got[i].move != w[i].move {
				t.Errorf("%s line %d: %s %s %s, want %v", name, i, got[i].label, got[i].dir, got[i].move, w[i])
			}
		}
	}

	// Thursday before the open has no bar for today yet as far as it cares.
	check("thursday morning", marketLines(h, "pre", time.Date(2026, 10, 8, 7, 0, 0, 0, et)), []want{
		{"YESTERDAY", "down", "0.98%"}, {"TODAY", "", ""}, {"TOMORROW", "", ""},
	})
	// The 9am brief is still before the bell, so it's the same three answers
	// with the 8:30 data out.
	check("thursday pre-open", marketLines(h, "pre", time.Date(2026, 10, 8, 9, 0, 0, 0, et)), []want{
		{"YESTERDAY", "down", "0.98%"}, {"TODAY", "", ""}, {"TOMORROW", "", ""},
	})
	check("thursday midday", marketLines(h, "regular", time.Date(2026, 10, 8, 11, 0, 0, 0, et)), []want{
		{"YESTERDAY", "down", "0.98%"}, {"TODAY", "up", "0.50% SO FAR"}, {"TOMORROW", "", ""},
	})
	check("thursday close", marketLines(h, "post", time.Date(2026, 10, 8, 16, 5, 0, 0, et)), []want{
		{"YESTERDAY", "down", "0.98%"}, {"TODAY", "up", "0.50%"}, {"TOMORROW", "", ""},
	})
	// The week is Thursday's close against the Friday before, with no bar for
	// this Friday, and the week ahead carries the lean.
	check("saturday", marketLines(h, "closed", time.Date(2026, 10, 10, 9, 0, 0, 0, et)), []want{
		{"LAST WEEK", "up", "0.50%"}, {"WEEK AHEAD", "", ""},
	})
	h.times, h.closes = h.times[:3], h.closes[:3]
	check("monday morning", marketLines(h, "pre", time.Date(2026, 10, 5, 7, 0, 0, 0, et)), []want{
		{"FRIDAY", "up", "1.00%"}, {"TODAY", "", ""}, {"TOMORROW", "", ""},
	})
	if got := marketLines(h, "closed", time.Date(2026, 10, 4, 9, 0, 0, 0, et)); got[len(got)-1].label != "WEEK AHEAD" || !got[len(got)-1].week || got[len(got)-1].day.Day() != 5 {
		t.Errorf("sunday should end on a lean for the week of the 5th, got %+v", got)
	}
}

func TestMarketProblems(t *testing.T) {
	et := easternTime()
	monday := time.Date(2026, 10, 12, 0, 0, 0, 0, et)
	lines := []marketLine{{label: "LAST WEEK"}, {label: weekAhead, forward: true, week: true, day: monday}}
	releases := []release{{at: time.Date(2026, 10, 14, 8, 30, 0, 0, et), short: "CPI"}}
	same := "Retailers trimmed assortments and Apple cut iPhone orders on soft demand."

	bad := marketProblems(lines, map[string]marketAnswer{"line1": {Text: same}, "line2": {Text: same}}, nil, releases)
	if !strings.Contains(bad["line2"], "repeats line1") || bad["line1"] != "" {
		t.Errorf("a copied week ahead: %v", bad)
	}
	bad = marketProblems(lines, map[string]marketAnswer{"line1": {Text: "Stocks rose."}, "line2": {Text: "Apple cut iPhone orders."}}, nil, releases)
	if !strings.Contains(bad["line2"], "CPI") {
		t.Errorf("a week ahead without the CPI: %v", bad)
	}
	if bad = marketProblems(lines, map[string]marketAnswer{"line1": {Text: "Stocks rose."}, "line2": {Text: "CPI on Wednesday weighs."}}, nil, releases); len(bad) != 0 {
		t.Errorf("a good week ahead flagged: %v", bad)
	}
}

func TestTradingDay(t *testing.T) {
	cases := []struct{ at, day string }{
		{"2026-10-12 15:30", "2026-10-12"},
		{"2026-10-12 18:00", "2026-10-13"},
		{"2026-10-13 06:00", "2026-10-13"},
		{"2026-10-09 19:00", "2026-10-12"},
		{"2026-10-11 12:00", "2026-10-12"},
	}
	for _, c := range cases {
		if got := tradingDay(inNY(t, c.at)).Format("2006-01-02"); got != c.day {
			t.Errorf("%s: got %s, want %s", c.at, got, c.day)
		}
	}
}

// Reuters runs the same headline after Monday's close and Tuesday's, and one
// cluster cited for both would have YESTERDAY and TODAY saying the same thing.
func TestMarketsKeepSessionsApart(t *testing.T) {
	monday := story{title: "S&P 500 flat as chipmakers fall", url: "https://reuters.test/1", source: "REUTERS", at: inNY(t, "2026-10-12 16:10")}
	tuesday := story{title: "S&P 500 flat as chipmakers fall and bank earnings roll in", url: "https://reuters.test/2", source: "REUTERS", at: inNY(t, "2026-10-13 16:10")}
	monday.words, tuesday.words = significant(monday.title), significant(tuesday.title)
	events := clusterBy([]story{tuesday, monday}, 10, func(s story) time.Time { return tradingDay(s.at) })
	if len(events) != 2 {
		t.Fatalf("want one event per session, got %d", len(events))
	}
	if list := clusterList(events, inNY(t, "2026-10-13 16:30")); !strings.Contains(list, "Monday's session") || !strings.Contains(list, "Tuesday's session") {
		t.Errorf("events should say their session:\n%s", list)
	}

	yesterday := closedLine("YESTERDAY", 100, 100, "why").between(inNY(t, "2026-10-09 09:30"), inNY(t, "2026-10-12 09:30"))
	today := closedLine("TODAY", 100, 100, "why").between(inNY(t, "2026-10-12 09:30"), inNY(t, "2026-10-13 16:05"))
	if !strings.Contains(yesterday.ask, "only stories marked Monday's session") {
		t.Errorf("yesterday's ask: %s", yesterday.ask)
	}
	tues := slices.IndexFunc(events, func(c cluster) bool { return c.session.Weekday() == time.Tuesday }) + 1
	mon := 3 - tues
	lines := []marketLine{yesterday, today}

	bad := marketProblems(lines, map[string]marketAnswer{
		"line1": {Text: "Chipmakers fell as bank earnings rolled in.", Sources: []int{tues}},
		"line2": {Text: "Banks reported earnings.", Sources: []int{tues}},
	}, events, nil)
	if !strings.Contains(bad["line1"], "Tuesday's session") || bad["line2"] != "" {
		t.Errorf("yesterday cited today's story: %v", bad)
	}
	bad = marketProblems(lines, map[string]marketAnswer{
		"line1": {Text: "Chipmakers fell, leaving the S&P 500 flat Monday.", Sources: []int{mon}},
		"line2": {Text: "Chipmakers fell, leaving the S&P 500 flat Tuesday.", Sources: []int{tues}},
	}, events, nil)
	if !strings.Contains(bad["line2"], "repeats line1") {
		t.Errorf("today copied yesterday: %v", bad)
	}
	if bad = marketProblems(lines, map[string]marketAnswer{
		"line1": {Text: "Chipmakers fell.", Sources: []int{mon}},
		"line2": {Text: "Bank earnings rolled in.", Sources: []int{tues}},
	}, events, nil); len(bad) != 0 {
		t.Errorf("a good pair flagged: %v", bad)
	}
}

// A holiday Monday's news counts for Tuesday, the next session, and a week
// covers every weekday in it.
func TestClosedLineSessions(t *testing.T) {
	l := closedLine("TODAY", 1, 1, "why").between(inNY(t, "2026-10-09 09:30"), inNY(t, "2026-10-13 16:05"))
	if !l.covers(inNY(t, "2026-10-12 00:00")) || !strings.Contains(l.ask, "Monday or Tuesday's session") {
		t.Errorf("holiday: %s", l.ask)
	}
	week := closedLine("LAST WEEK", 1, 1, "why").between(inNY(t, "2026-10-02 09:30"), inNY(t, "2026-10-09 09:30"))
	if !strings.Contains(week.ask, "Monday, Tuesday, Wednesday, Thursday or Friday's session") || week.covers(inNY(t, "2026-10-02 00:00")) {
		t.Errorf("week: %s", week.ask)
	}
}
