package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseWhenRepeats(t *testing.T) {
	now := at("2026-11-19 14:00") // a Thursday
	cases := []struct{ in, cron, say string }{
		{"every weekday at 7:30am", "30 7 * * 1-5", "Monday to Friday at 7:30am"},
		{"every weekday 7:30am", "30 7 * * 1-5", "Monday to Friday at 7:30am"},
		{"weekdays", "0 9 * * 1-5", "Monday to Friday at 9am"},
		{"7:30am every weekday", "30 7 * * 1-5", "Monday to Friday at 7:30am"},
		{"weekends at noon", "0 12 * * 0,6", "Saturday and Sunday at 12pm"},
		{"every weekend 10am", "0 10 * * 0,6", "Saturday and Sunday at 10am"},
		{"sat 8am", "0 8 * * 6", "Saturdays at 8am"},
		{"Saturdays at 8 AM", "0 8 * * 6", "Saturdays at 8am"},
		{"mon and thu at 8pm", "0 20 * * 1,4", "Mondays and Thursdays at 8pm"},
		{"every tues, thurs & sat 6:15pm", "15 18 * * 2,4,6", "Tuesdays, Thursdays and Saturdays at 6:15pm"},
		{"mon tue wed thu fri at 9", "0 9 * * 1-5", "Monday to Friday at 9am"},
		{"every 15 minutes", "*/15 * * * *", "Every 15 minutes, all day"},
		{"every 7 mins", "*/7 * * * *", "Every 7 minutes, all day, counted from the top of each hour"},
		{"every minute", "* * * * *", "Every minute, all day"},
		{"every 4 hours", "0 */4 * * *", "Every 4 hours from midnight"},
		{"every 1 hour", "0 * * * *", "On the hour, every hour"},
		{"hourly", "0 * * * *", "On the hour, every hour"},
		{"every day", "0 9 * * *", "Every day at 9am"},
		{"daily at 6:45pm", "45 18 * * *", "Every day at 6:45pm"},
		{"every evening", "0 18 * * *", "Every day at 6pm"},
		{"every morning", "0 8 * * *", "Every day at 8am"},
		{"every night at 10pm", "0 22 * * *", "Every day at 10pm"},
		{"every day at midnight", "0 0 * * *", "Every day at 12am"},
		{"on the 1st every 3 months at 9am", "0 9 1 */3 *", "9am on the 1st, every 3 months from January"},
		{"on the 15th", "0 9 15 * *", "9am on the 15th of every month"},
		{"the 1st of every month at 8am", "0 8 1 * *", "8am on the 1st of every month"},
		{"monthly on the 31st", "0 9 31 * *", "9am on the 31st of every month, skipping months too short to have one"},
		{"every year on mar 3 at 9am", "0 9 3 3 *", "Every year on Mar 3 at 9am"},
		{"every dec 24", "0 9 24 12 *", "Every year on Dec 24 at 9am"},
		{"0 9 * * 1-5", "0 9 * * 1-5", "Weekdays 9:00am"},
		{"@daily", "@daily", "@daily"},
	}
	for _, c := range cases {
		w, err := ParseWhen(c.in, now, ny)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if w.Cron != c.cron || w.Say != c.say || !w.At.IsZero() {
			t.Errorf("%q = %q %q, want %q %q", c.in, w.Cron, w.Say, c.cron, c.say)
		}
		if _, err := ParseCron(w.Cron); err != nil {
			t.Errorf("%q gave a line that doesn't parse: %v", c.in, err)
		}
	}
}

func TestParseWhenOnce(t *testing.T) {
	now := at("2026-11-19 14:00")
	cases := []struct{ in, want string }{
		{"tomorrow at 3pm", "2026-11-20 15:00"},
		{"tomorrow", "2026-11-20 09:00"},
		{"tomorrow morning", "2026-11-20 08:00"},
		{"today at 5pm", "2026-11-19 17:00"},
		{"tonight", "2026-11-19 21:00"},
		{"in 30 minutes", "2026-11-19 14:30"},
		{"in an hour", "2026-11-19 15:00"},
		{"in 3 days", "2026-11-22 14:00"},
		{"in 2 days at 7am", "2026-11-21 07:00"},
		{"in 1 week", "2026-11-26 14:00"},
		{"dec 24 at 9am", "2026-12-24 09:00"},
		{"December 24th, 2027 6pm", "2027-12-24 18:00"},
		{"24th of december", "2026-12-24 09:00"},
		{"nov 19 at 1pm", "2027-11-19 13:00"},
		{"nov 19 at 3pm", "2026-11-19 15:00"},
		{"12/25 at 8am", "2026-12-25 08:00"},
		{"feb 29", "2028-02-29 09:00"},
		{"next friday 6pm", "2026-11-20 18:00"},
		{"next thursday", "2026-11-26 09:00"},
		{"this thursday at 8pm", "2026-11-19 20:00"},
		{"this thursday at 8am", "2026-11-26 08:00"},
		{"2026-11-22T08:00", "2026-11-22 08:00"},
	}
	for _, c := range cases {
		w, err := ParseWhen(c.in, now, ny)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if w.Cron != "" || !w.At.Equal(at(c.want)) || !strings.HasPrefix(w.Say, "Once, ") {
			t.Errorf("%q = %s %q %q, want %s", c.in, w.At.In(ny), w.Cron, w.Say, c.want)
		}
	}
}

// The clocks change on March 14 and November 7 2027 in New York.
func TestParseWhenDST(t *testing.T) {
	w, err := ParseWhen("in 1 day", at("2027-03-13 09:00"), ny)
	if err != nil || !w.At.Equal(at("2027-03-14 09:00")) {
		t.Errorf("a day across the spring change is the same wall time, got %s %v", w.At.In(ny), err)
	}
	if got := w.At.Sub(at("2027-03-13 09:00")); got != 23*time.Hour {
		t.Errorf("that day was %s long", got)
	}
	w, _ = ParseWhen("in 3 hours", at("2027-11-07 00:30"), ny)
	if got := w.At.In(ny).Format("15:04 MST"); got != "02:30 EST" {
		t.Errorf("three real hours across the fall change landed at %s", got)
	}
	w, _ = ParseWhen("tomorrow at 2:30am", at("2027-03-13 12:00"), ny)
	if got := w.At.In(ny).Format("15:04 MST"); got != "03:30 EDT" {
		t.Errorf("2:30 on the spring change day landed at %s", got)
	}
	w, _ = ParseWhen("every day at 2:30am", at("2027-03-13 12:00"), ny)
	e, _ := ParseCron(w.Cron)
	next, _ := e.Next(at("2027-03-13 12:00"), ny)
	if !next.Equal(at("2027-03-15 02:30")) {
		t.Errorf("a daily 2:30am skips the spring change day, got %s", next.In(ny))
	}
	w, _ = ParseWhen("nov 7 2027 at 1:30am", at("2027-11-01 12:00"), ny)
	if w.At.In(ny).Format("MST") != "EDT" {
		t.Errorf("the repeated hour should be its first pass, got %s", w.At.In(ny))
	}
}

func TestParseWhenRefuses(t *testing.T) {
	now := at("2026-11-19 14:00")
	for _, in := range []string{
		"", "whenever", "every 15 minutes at 9am", "every 0 minutes", "every 60 minutes", "every 24 hours",
		"today at 8am", "feb 30", "nov 31", "on the 32nd", "every other friday", "13/40",
		"2020-01-01T09:00", "dec 24 2025", "25:00", "at 9am at 10am", "0 0 31 2 *", "in 30 minutes at 5pm",
	} {
		if w, err := ParseWhen(in, now, ny); err == nil {
			t.Errorf("%q parsed as %+v", in, w)
		}
	}
}
