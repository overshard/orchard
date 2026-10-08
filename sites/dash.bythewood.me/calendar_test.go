package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseICS(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\nDTSTART;TZID=US-Eastern:20261014T083000\r\nSUMMARY:Consumer Price Index\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nDTSTART;TZID=US-Eastern:20261014T100000\r\nSUMMARY:Real Earnings\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:GDP (Advance Estimate)\\, 3rd\r\n  Quarter 2026\r\nDTSTART;VALUE=DATE-TIME:20261029T123000Z\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	got := parseICS(strings.NewReader(ics))
	if len(got) != 2 {
		t.Fatalf("got %d releases, want CPI and GDP: %+v", len(got), got)
	}
	et := easternTime()
	if got[0].name != "CPI inflation" || !got[0].at.Equal(time.Date(2026, 10, 14, 8, 30, 0, 0, et)) {
		t.Errorf("CPI: %+v", got[0])
	}
	if got[1].name != "GDP" || !got[1].at.Equal(time.Date(2026, 10, 29, 8, 30, 0, 0, et)) {
		t.Errorf("GDP: %+v", got[1])
	}
}

func TestTurnOfMonth(t *testing.T) {
	et := easternTime()
	for d, want := range map[int]bool{1: true, 5: true, 6: false, 29: false, 30: true} {
		day := time.Date(2026, 10, d, 9, 0, 0, 0, et)
		if got := turnOfMonth(day); got != want {
			t.Errorf("Oct %d: %v, want %v", d, got, want)
		}
	}
}

func TestSessionNotes(t *testing.T) {
	et := easternTime()
	cpi := release{time.Date(2026, 10, 28, 8, 30, 0, 0, et), "CPI inflation"}
	notes := strings.Join(sessionNotes(time.Date(2026, 10, 28, 9, 0, 0, 0, et), []release{cpi}, 15.9), "\n")
	for _, want := range []string{"CPI inflation comes out at 8:30am", "rate decision at 2pm", "about 1.0% either way"} {
		if !strings.Contains(notes, want) {
			t.Errorf("missing %q in\n%s", want, notes)
		}
	}
	if eve := sessionNotes(time.Date(2026, 10, 27, 9, 0, 0, 0, et), nil, 0); len(eve) != 1 || !strings.Contains(eve[0], "decision the next day") {
		t.Errorf("the day before: %v", eve)
	}
}
