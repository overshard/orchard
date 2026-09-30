package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Time arithmetic is done here rather than by the model, which read 8:27 and
// said 10am was 33 minutes away.

var (
	clockTime = regexp.MustCompile(`\b(\d{1,2})(?::(\d{2}))?\s*(am|pm|a\.m\.|p\.m\.)?(?:\s|$)`)
	isoDate   = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	monthDay  = regexp.MustCompile(`\b(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	dayMonth  = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)?(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\b`)
)

var months = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April,
	"may": time.May, "jun": time.June, "jul": time.July, "aug": time.August,
	"sep": time.September, "sept": time.September, "oct": time.October,
	"nov": time.November, "dec": time.December,
}

// ParseUntil reads the moment a "how long until" question is about, relative to
// now and in now's location. A time with no date is the next one to come round,
// and a date with no time is the start of that day.
func ParseUntil(s string, now time.Time) (time.Time, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, " ?.!")
	s = strings.NewReplacer("o'clock", "", "oclock", "", " at ", " ", "the ", "").Replace(" " + s + " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	loc := now.Location()
	y, m, d := now.Date()
	date := time.Date(y, m, d, 0, 0, 0, 0, loc)
	dated, fixedYear := false, false

	switch {
	case isoDate.MatchString(s):
		p := isoDate.FindStringSubmatch(s)
		yy, _ := strconv.Atoi(p[1])
		mm, _ := strconv.Atoi(p[2])
		dd, _ := strconv.Atoi(p[3])
		date, dated, fixedYear = time.Date(yy, time.Month(mm), dd, 0, 0, 0, 0, loc), true, true
		s = isoDate.ReplaceAllString(s, " ")
	case monthDay.MatchString(s):
		p := monthDay.FindStringSubmatch(s)
		dd, _ := strconv.Atoi(p[2])
		date, dated = time.Date(y, months[p[1]], dd, 0, 0, 0, 0, loc), true
		s = monthDay.ReplaceAllString(s, " ")
	case dayMonth.MatchString(s):
		p := dayMonth.FindStringSubmatch(s)
		dd, _ := strconv.Atoi(p[1])
		date, dated = time.Date(y, months[p[2]], dd, 0, 0, 0, 0, loc), true
		s = dayMonth.ReplaceAllString(s, " ")
	}
	if !dated {
		for name, md := range holidays {
			if strings.Contains(s, name) {
				date, dated = md(y, loc), true
				s = strings.ReplaceAll(s, name, " ")
				break
			}
		}
	}
	if !dated {
		for i := 0; i < 7; i++ {
			wd := time.Weekday(i)
			name := strings.ToLower(wd.String())
			if strings.Contains(s, name) || strings.Contains(s, name[:3]+" ") || strings.HasSuffix(s, name[:3]) {
				ahead := (int(wd) - int(now.Weekday()) + 7) % 7
				date, dated = date.AddDate(0, 0, ahead), true
				s = strings.NewReplacer(name, " ").Replace(s)
				break
			}
		}
	}
	if !dated {
		switch {
		case strings.Contains(s, "tomorrow"):
			date, dated = date.AddDate(0, 0, 1), true
			s = strings.ReplaceAll(s, "tomorrow", " ")
		case strings.Contains(s, "tonight"), strings.Contains(s, "today"):
			s = strings.NewReplacer("tonight", " pm", "today", " ").Replace(s)
		}
	}

	hour, min, timed := 0, 0, false
	switch {
	case strings.Contains(s, "noon"):
		hour, timed = 12, true
	case strings.Contains(s, "midnight"):
		date, timed = date.AddDate(0, 0, 1), true
	default:
		if p := clockTime.FindStringSubmatch(s + " "); p != nil {
			h, _ := strconv.Atoi(p[1])
			mm := 0
			if p[2] != "" {
				mm, _ = strconv.Atoi(p[2])
			}
			ampm := strings.ReplaceAll(p[3], ".", "")
			if ampm == "" && strings.Contains(s, " pm") {
				ampm = "pm"
			}
			if h > 23 || mm > 59 || (ampm != "" && (h < 1 || h > 12)) {
				return time.Time{}, false
			}
			if ampm == "pm" && h < 12 {
				h += 12
			}
			if ampm == "am" && h == 12 {
				h = 0
			}
			hour, min, timed = h, mm, true
			// "till 10" at half eight means ten in the morning, and at nine at
			// night means ten at night.
			if ampm == "" && h <= 12 && !dated {
				at := time.Date(date.Year(), date.Month(), date.Day(), h, mm, 0, 0, loc)
				if !at.After(now) && at.Add(12*time.Hour).After(now) && h < 12 {
					hour += 12
				}
			}
		}
	}
	if !dated && !timed {
		return time.Time{}, false
	}
	at := time.Date(date.Year(), date.Month(), date.Day(), hour, min, 0, 0, loc)
	if !at.After(now) {
		switch {
		case !dated:
			at = at.AddDate(0, 0, 1)
		case !fixedYear && at.Before(now.AddDate(0, 0, -1)):
			// "October 4" in December is next year's.
			at = at.AddDate(1, 0, 0)
		}
	}
	return at, true
}

// holidays with a date that moves need working out, the rest are fixed.
var holidays = map[string]func(int, *time.Location) time.Time{
	"christmas eve": func(y int, l *time.Location) time.Time { return time.Date(y, 12, 24, 0, 0, 0, 0, l) },
	"christmas":     func(y int, l *time.Location) time.Time { return time.Date(y, 12, 25, 0, 0, 0, 0, l) },
	"new year":      func(y int, l *time.Location) time.Time { return time.Date(y+1, 1, 1, 0, 0, 0, 0, l) },
	"halloween":     func(y int, l *time.Location) time.Time { return time.Date(y, 10, 31, 0, 0, 0, 0, l) },
	"thanksgiving": func(y int, l *time.Location) time.Time {
		d := time.Date(y, 11, 1, 0, 0, 0, 0, l)
		for d.Weekday() != time.Thursday {
			d = d.AddDate(0, 0, 1)
		}
		return d.AddDate(0, 0, 21)
	},
}

// Remaining says how far off a moment is in days, hours and minutes.
func Remaining(from, to time.Time) string {
	d := to.Sub(from).Round(time.Minute)
	if d < 0 {
		return "already past"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	var parts []string
	unit := func(n int, one string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %ss", n, one))
		}
	}
	unit(days, "day")
	unit(hours, "hour")
	unit(mins, "minute")
	if len(parts) == 0 {
		return "less than a minute"
	}
	return strings.Join(parts, " ")
}
