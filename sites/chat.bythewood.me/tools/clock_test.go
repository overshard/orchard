package tools

import (
	"testing"
	"time"
)

func TestUntilIsWorkedOutRatherThanGuessed(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	morning := time.Date(2026, 9, 30, 8, 27, 0, 0, ny)
	evening := time.Date(2026, 9, 30, 21, 0, 0, 0, ny)
	for _, c := range []struct {
		at   time.Time
		ask  string
		want string
	}{
		{morning, "10am", "1 hour 33 minutes"},
		{morning, "10", "1 hour 33 minutes"},
		{evening, "10", "1 hour"},
		{morning, "noon", "3 hours 33 minutes"},
		{morning, "5:30 pm", "9 hours 3 minutes"},
		{morning, "8am", "23 hours 33 minutes"},
		{morning, "friday 5pm", "2 days 8 hours 33 minutes"},
		{morning, "October 4", "3 days 15 hours 33 minutes"},
		{morning, "2026-10-01", "15 hours 33 minutes"},
		{morning, "christmas", "85 days 16 hours 33 minutes"}, // the clocks go back in between
		{morning, "tomorrow at 9am", "1 day 33 minutes"},
		{morning, "midnight", "15 hours 33 minutes"},
	} {
		at, ok := ParseUntil(c.ask, c.at)
		if !ok {
			t.Errorf("%q did not parse", c.ask)
			continue
		}
		if got := Remaining(c.at, at); got != c.want {
			t.Errorf("until %q from %s = %s (%s), want %s", c.ask, c.at.Format(time.Kitchen), got, at, c.want)
		}
	}
	for _, bad := range []string{"", "the weekend is over", "25pm"} {
		if _, ok := ParseUntil(bad, morning); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}
