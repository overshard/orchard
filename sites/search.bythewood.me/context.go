package main

import (
	"fmt"
	"strings"
	"time"
)

// Ambient is what the model would know if it were a person sitting here, and
// has no way of knowing otherwise. Without the date it cannot tell what
// "recent" means; without the place it answers a question about the weather or
// what is open as though it were nowhere.
//
// Location is a constant for the same reason it is in dash: it is where Isaac
// is, and a lookup would be a network call to learn something that does not
// change.
const (
	placeName = "Yadkin Valley, North Carolina, United States"
	timeZone  = "America/New_York"
)

// AmbientContext is the block at the top of every system prompt: the facts plus
// the instruction for using them.
func AmbientContext() string {
	return AmbientFacts() + " " + strings.Join([]string{
		"Use this only when the question depends on it, such as anything asking what is recent, current, nearby, in season, or open.",
		"Never state it as a fact from a source.",
	}, " ")
}

// AmbientFacts is the same block without the instructions, for showing a person
// what the model has been told.
func AmbientFacts() string {
	now := localNow()
	parts := []string{
		fmt.Sprintf("Today is %s.", now.Format("Monday, 2 January 2006")),
		fmt.Sprintf("The local time is %s.", now.Format("3:04 PM MST")),
		fmt.Sprintf("The user is in %s.", placeName),
		fmt.Sprintf("It is %s.", season(now)),
	}
	if h := holiday(now); h != "" {
		parts = append(parts, fmt.Sprintf("Today is %s.", h))
	} else if h, days := nextHoliday(now); h != "" && days <= 14 {
		parts = append(parts, fmt.Sprintf("%s is in %d days.", h, days))
	}
	return strings.Join(parts, " ")
}

func localNow() time.Time {
	loc, err := time.LoadLocation(timeZone)
	if err != nil {
		return time.Now()
	}
	return time.Now().In(loc)
}

func season(t time.Time) time.Month { return t.Month() }

// holiday names the day if it is one worth knowing about. US holidays plus the
// handful of others a question might turn on.
func holiday(t time.Time) string {
	y := t.Year()
	for name, day := range holidays(y) {
		if sameDay(t, day) {
			return name
		}
	}
	return ""
}

func nextHoliday(t time.Time) (string, int) {
	best, bestDays := "", 1<<30
	for _, y := range []int{t.Year(), t.Year() + 1} {
		for name, day := range holidays(y) {
			d := int(day.Sub(truncDay(t)).Hours() / 24)
			if d > 0 && d < bestDays {
				best, bestDays = name, d
			}
		}
	}
	if best == "" {
		return "", 0
	}
	return best, bestDays
}

func holidays(y int) map[string]time.Time {
	loc := localNow().Location()
	d := func(m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, loc) }
	return map[string]time.Time{
		"New Year's Day":   d(time.January, 1),
		"Valentine's Day":  d(time.February, 14),
		"St Patrick's Day": d(time.March, 17),
		"Easter Sunday":    easter(y, loc),
		"Independence Day": d(time.July, 4),
		"Halloween":        d(time.October, 31),
		"Thanksgiving":     nthWeekday(y, time.November, time.Thursday, 4, loc),
		"Christmas Eve":    d(time.December, 24),
		"Christmas Day":    d(time.December, 25),
		"New Year's Eve":   d(time.December, 31),
		"Memorial Day":     lastWeekday(y, time.May, time.Monday, loc),
		"Labor Day":        nthWeekday(y, time.September, time.Monday, 1, loc),
		"Mother's Day":     nthWeekday(y, time.May, time.Sunday, 2, loc),
		"Father's Day":     nthWeekday(y, time.June, time.Sunday, 3, loc),
	}
}

// easter is the anonymous Gregorian computus. It is here because several
// holidays hang off it and it is fifteen lines.
func easter(y int, loc *time.Location) time.Time {
	a := y % 19
	b := y / 100
	c := y % 100
	dd := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - dd - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(y, time.Month(month), day, 0, 0, 0, 0, loc)
}

func nthWeekday(y int, m time.Month, wd time.Weekday, n int, loc *time.Location) time.Time {
	t := time.Date(y, m, 1, 0, 0, 0, 0, loc)
	for t.Weekday() != wd {
		t = t.AddDate(0, 0, 1)
	}
	return t.AddDate(0, 0, 7*(n-1))
}

func lastWeekday(y int, m time.Month, wd time.Weekday, loc *time.Location) time.Time {
	t := time.Date(y, m+1, 1, 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	for t.Weekday() != wd {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

func truncDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}
