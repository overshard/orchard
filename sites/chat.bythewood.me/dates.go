package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A weekday beside a date is arithmetic the model does in its head and gets
// wrong: "Monday, October 4, 2026" was a Sunday. The date is usually the part
// it read and the weekday the part it added, so the weekday is what changes.

const monthNames = `January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sept|Sep|Oct|Nov|Dec`
const dayNames = `Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday`

var (
	dayThenDate = regexp.MustCompile(`\b(` + dayNames + `),?\s+(?:(` + monthNames + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?|(\d{1,2})(?:st|nd|rd|th)?\s+(` + monthNames + `))(?:,?\s+(\d{4}))?\b`)
	dateThenDay = regexp.MustCompile(`\b(` + monthNames + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?(?:,?\s+(\d{4}))?\**\s*\((` + dayNames + `)\)`)
)

func fixWeekdays(md string, now time.Time) string {
	md = dayThenDate.ReplaceAllStringFunc(md, func(m string) string {
		p := dayThenDate.FindStringSubmatch(m)
		month, day := p[2], p[3]
		if month == "" {
			month, day = p[5], p[4]
		}
		if right, ok := weekdayOf(month, day, p[6], now); ok && right != p[1] {
			return right + strings.TrimPrefix(m, p[1])
		}
		return m
	})
	return dateThenDay.ReplaceAllStringFunc(md, func(m string) string {
		p := dateThenDay.FindStringSubmatch(m)
		if right, ok := weekdayOf(p[1], p[2], p[3], now); ok && right != p[4] {
			return strings.TrimSuffix(m, "("+p[4]+")") + "(" + right + ")"
		}
		return m
	})
}

// weekdayOf is the real weekday of a date. With no year it is the nearest one
// that is not long past, since "Sunday, October 4" is about the coming one.
func weekdayOf(month, day, year string, now time.Time) (string, bool) {
	mon, ok := monthNumber(month)
	d, err := strconv.Atoi(day)
	if !ok || err != nil || d < 1 || d > 31 {
		return "", false
	}
	y := now.Year()
	if year != "" {
		if y, err = strconv.Atoi(year); err != nil {
			return "", false
		}
	}
	t := time.Date(y, mon, d, 12, 0, 0, 0, time.UTC)
	if t.Day() != d {
		return "", false
	}
	if year == "" && now.Sub(t) > 180*24*time.Hour {
		t = t.AddDate(1, 0, 0)
	}
	return t.Weekday().String(), true
}

func monthNumber(s string) (time.Month, bool) {
	s = strings.ToLower(s)
	for m := time.January; m <= time.December; m++ {
		if strings.HasPrefix(strings.ToLower(m.String()), s) && len(s) >= 3 {
			return m, true
		}
	}
	return 0, false
}
