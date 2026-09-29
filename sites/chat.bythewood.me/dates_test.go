package main

import (
	"testing"
	"time"
)

func TestAWrongWeekdayIsCorrected(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"It comes back on Monday, October 4, 2026, after its vacation.": "It comes back on Sunday, October 4, 2026, after its vacation.",
		"Reopens **October 4, 2026** (Monday).":                         "Reopens **October 4, 2026** (Sunday).",
		"The game is Sunday, 4 October.":                                "The game is Sunday, 4 October.",
		"Back on Friday, Jan 8.":                                        "Back on Friday, Jan 8.",
		"Back on Monday, Jan 8.":                                        "Back on Friday, Jan 8.",
		"Tuesday, September 29, 2026 is today.":                         "Tuesday, September 29, 2026 is today.",
		"Wednesday, February 30 does not exist.":                        "Wednesday, February 30 does not exist.",
	} {
		if got := fixWeekdays(in, now); got != want {
			t.Errorf("fixWeekdays(%q) = %q, want %q", in, got, want)
		}
	}
}
