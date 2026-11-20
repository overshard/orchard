package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// When is a schedule read from plain words, a cron line when it repeats or a
// time when it fires once, with a sentence saying back what was understood.
type When struct {
	Cron string
	At   time.Time
	Say  string
}

func (w When) Once() bool { return w.Cron == "" }

var errWhen = errors.New(`couldn't read that, try a day and a time like "fri 6pm", a repeat like "every 2 hours" or a date like "dec 24 at 9am"`)

var (
	// A time needs an at, an am or pm, or a colon, so the 24 in "dec 24" or
	// the 15 in "every 15 minutes" is never read as one.
	timeRe = regexp.MustCompile(`(?:^|\s)(?:at\s+|@\s*)?(\d{1,2}(?::\d{2})?\s*(?:am|pm|a|p)|\d{1,2}:\d{2}|noon|midnight)(?:\s|$)|(?:^|\s)(?:at|@)\s*(\d{1,2})(?:\s|$)`)

	everyMinRe   = regexp.MustCompile(`^every (\d+) (?:m|min|mins|minute|minutes)$`)
	everyHourRe  = regexp.MustCompile(`^every (\d+) (?:h|hr|hrs|hour|hours)$`)
	dailyRe      = regexp.MustCompile(`^(?:every |each )?(day|daily|morning|evening|night)$`)
	monthDayRe   = regexp.MustCompile(`^(?:on )?(?:the )?(\d{1,2})(?:st|nd|rd|th)?(?: of)?(?: every (\d+) months?| every month| of the month| of every month| monthly| each month)?$`)
	inRe         = regexp.MustCompile(`^in (\d+|a|an|one) (m|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|day|days|w|wk|wks|week|weeks)$`)
	dateRe       = regexp.MustCompile(`^(?:on )?([a-z]{3,9})\.? (\d{1,2})(?:st|nd|rd|th)?(?:,? (\d{4}))?$`)
	dateRevRe    = regexp.MustCompile(`^(?:on )?(?:the )?(\d{1,2})(?:st|nd|rd|th)? (?:of )?([a-z]{3,9})(?:,? (\d{4}))?$`)
	slashDateRe  = regexp.MustCompile(`^(?:on )?(\d{1,2})/(\d{1,2})(?:/(\d{2}|\d{4}))?$`)
	nextDayRe    = regexp.MustCompile(`^(next|this|coming) ([a-z]+)$`)
	yearlyPrefix = regexp.MustCompile(`^(?:every year on |yearly on |annually on |every year |yearly |annually |every )`)
)

var fullDays = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

var fullMonths = []string{"january", "february", "march", "april", "may", "june", "july",
	"august", "september", "october", "november", "december"}

// ParseWhen reads a schedule the way it would be said, on loc's wall clock and
// counted from now. A cron line or a plain timestamp is taken as well, so the
// one box covers everything the advanced fields do.
func ParseWhen(raw string, now time.Time, loc *time.Location) (When, error) {
	s := normalize(raw)
	if s == "" {
		return When{}, errors.New("say when, like every weekday at 7:30am")
	}
	now = now.In(loc)

	if e, err := ParseCron(s); err == nil {
		if _, ok := e.Next(now, loc); !ok {
			return When{}, errors.New("that cron line never matches")
		}
		line := strings.Join(strings.Fields(s), " ")
		return When{Cron: line, Say: capital(humanCron(line))}, nil
	}
	if t, ok := parseStamp(raw, loc); ok {
		return once(t, now)
	}

	body, hm, hasTime, err := splitTime(s)
	if err != nil {
		return When{}, err
	}
	at := func(def clock) clock {
		if hasTime {
			return hm
		}
		return def
	}

	switch {
	case body == "every minute":
		return noTime(hasTime, When{Cron: "* * * * *", Say: "Every minute, all day"})
	case body == "hourly" || body == "every hour":
		return noTime(hasTime, When{Cron: "0 * * * *", Say: "On the hour, every hour"})
	}
	if m := everyMinRe.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n < 1 || n > 59 {
			return When{}, errors.New("every so many minutes goes from 1 to 59, use hours past that")
		}
		w := When{Cron: fmt.Sprintf("*/%d * * * *", n), Say: fmt.Sprintf("Every %d minutes, all day", n)}
		if n == 1 {
			w = When{Cron: "* * * * *", Say: "Every minute, all day"}
		} else if 60%n != 0 {
			w.Say += ", counted from the top of each hour"
		}
		return noTime(hasTime, w)
	}
	if m := everyHourRe.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n < 1 || n > 23 {
			return When{}, errors.New("every so many hours goes from 1 to 23")
		}
		if n == 1 {
			return noTime(hasTime, When{Cron: "0 * * * *", Say: "On the hour, every hour"})
		}
		w := When{Cron: fmt.Sprintf("0 */%d * * *", n), Say: fmt.Sprintf("Every %d hours from midnight", n)}
		if 24%n != 0 {
			w.Say += ", starting over each day"
		}
		return noTime(hasTime, w)
	}
	if body == "every day" || body == "each day" {
		body = "day"
	}
	if m := dailyRe.FindStringSubmatch(body); m != nil {
		def, ok := map[string]clock{"morning": {8, 0}, "evening": {18, 0}, "night": {21, 0}}[m[1]]
		if !ok {
			def = clock{9, 0}
		}
		t := at(def)
		return When{Cron: t.cron("* * *"), Say: "Every day at " + t.String()}, nil
	}
	switch strings.TrimPrefix(strings.TrimPrefix(body, "every "), "on ") {
	case "weekday", "weekdays":
		t := at(clock{9, 0})
		return When{Cron: t.cron("* * 1-5"), Say: "Monday to Friday at " + t.String()}, nil
	case "weekend", "weekends":
		t := at(clock{9, 0})
		return When{Cron: t.cron("* * 0,6"), Say: "Saturday and Sunday at " + t.String()}, nil
	}

	if w, ok, err := monthly(body, at(clock{9, 0})); ok {
		return w, err
	}

	switch body {
	case "today", "tonight", "tomorrow", "tomorrow morning", "tomorrow evening", "tomorrow night", "this morning", "this evening":
		def := clock{9, 0}
		switch {
		case strings.HasSuffix(body, "evening"):
			def = clock{18, 0}
		case strings.HasSuffix(body, "night"):
			def = clock{21, 0}
		case strings.HasSuffix(body, "morning"):
			def = clock{8, 0}
		}
		day := now
		if strings.HasPrefix(body, "tomorrow") {
			day = now.AddDate(0, 0, 1)
		}
		return once(at(def).on(day, loc), now)
	}

	if m := inRe.FindStringSubmatch(body); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			n = 1
		}
		if n < 1 || n > 3650 {
			return When{}, errors.New("that's too far out")
		}
		var t time.Time
		switch m[2][0] {
		case 'm':
			if hasTime {
				return When{}, errors.New("in so many minutes doesn't take a time as well")
			}
			t = now.Add(time.Duration(n) * time.Minute)
		case 'h':
			if hasTime {
				return When{}, errors.New("in so many hours doesn't take a time as well")
			}
			t = now.Add(time.Duration(n) * time.Hour)
		case 'd', 'w':
			days := n
			if m[2][0] == 'w' {
				days *= 7
			}
			t = now.AddDate(0, 0, days)
			if hasTime {
				t = hm.on(t, loc)
			}
		}
		return once(t.Truncate(time.Minute), now)
	}

	if w, ok, err := date(body, at(clock{9, 0}), now, loc); ok {
		return w, err
	}

	if m := nextDayRe.FindStringSubmatch(body); m != nil {
		if d := dayIndex(m[2]); d >= 0 {
			t := at(clock{9, 0})
			ahead := (d - int(now.Weekday()) + 7) % 7
			cand := t.on(now.AddDate(0, 0, ahead), loc)
			if ahead == 0 && (m[1] == "next" || !cand.After(now)) {
				cand = t.on(now.AddDate(0, 0, 7), loc)
			}
			return once(cand, now)
		}
	}

	if days, ok := plainDays(body); ok {
		t := at(clock{9, 0})
		return When{Cron: t.cron("* * " + days.field()), Say: days.say() + " at " + t.String()}, nil
	}

	if strings.Contains(body, "every other") || strings.Contains(body, "fortnight") {
		return When{}, errors.New("every other week can't be said as a cron line, set two jobs or use a date")
	}
	return When{}, errWhen
}

func normalize(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.NewReplacer("a.m.", "am", "p.m.", "pm", "\t", " ").Replace(s)
	s = strings.TrimRight(s, ".!")
	return strings.Join(strings.Fields(s), " ")
}

// clock is a time of day on the wall clock.
type clock struct{ h, m int }

func (c clock) String() string {
	h := c.h % 12
	if h == 0 {
		h = 12
	}
	ap := "am"
	if c.h >= 12 {
		ap = "pm"
	}
	if c.m == 0 {
		return strconv.Itoa(h) + ap
	}
	return fmt.Sprintf("%d:%02d%s", h, c.m, ap)
}

func (c clock) cron(rest string) string { return fmt.Sprintf("%d %d %s", c.m, c.h, rest) }

// on puts the clock on day's date. A time the spring change skips lands an
// hour later, the way the clock on the wall would read it.
func (c clock) on(day time.Time, loc *time.Location) time.Time {
	day = day.In(loc)
	t := time.Date(day.Year(), day.Month(), day.Day(), c.h, c.m, 0, 0, loc)
	if t.Hour() != c.h || t.Minute() != c.m {
		// A skipped time is counted on from midnight, which steps it past the gap.
		t = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc).Add(time.Duration(c.h)*time.Hour + time.Duration(c.m)*time.Minute)
	}
	return t
}

func parseClock(s string) (clock, bool) {
	switch s {
	case "noon":
		return clock{12, 0}, true
	case "midnight":
		return clock{0, 0}, true
	}
	s = strings.ReplaceAll(s, " ", "")
	ap := ""
	for _, suf := range []string{"am", "pm", "a", "p"} {
		if strings.HasSuffix(s, suf) {
			ap, s = suf[:1], strings.TrimSuffix(s, suf)
			break
		}
	}
	hs, ms, _ := strings.Cut(s, ":")
	h, err := strconv.Atoi(hs)
	if err != nil {
		return clock{}, false
	}
	m := 0
	if ms != "" {
		if m, err = strconv.Atoi(ms); err != nil || len(ms) != 2 {
			return clock{}, false
		}
	}
	if h > 23 || m > 59 || ap != "" && (h > 12 || h == 0) {
		return clock{}, false
	}
	switch {
	case ap == "p" && h < 12:
		h += 12
	case ap == "a" && h == 12:
		h = 0
	}
	return clock{h, m}, true
}

// splitTime takes the time of day out of s, wherever it was said.
func splitTime(s string) (body string, c clock, ok bool, err error) {
	loc := timeRe.FindStringSubmatchIndex(s)
	if loc == nil {
		return s, clock{}, false, nil
	}
	tok := ""
	if loc[2] >= 0 {
		tok = s[loc[2]:loc[3]]
	} else {
		tok = s[loc[4]:loc[5]]
	}
	c, ok = parseClock(tok)
	if !ok {
		return "", clock{}, false, fmt.Errorf("%q isn't a time of day", tok)
	}
	body = strings.Join(strings.Fields(s[:loc[0]]+" "+s[loc[1]:]), " ")
	body = strings.TrimSuffix(strings.TrimSuffix(body, " at"), " on")
	if timeRe.MatchString(body) {
		return "", clock{}, false, errors.New("that has two times in it")
	}
	return body, c, true, nil
}

func noTime(hasTime bool, w When) (When, error) {
	if hasTime {
		return When{}, errors.New("a repeat every so many minutes or hours runs all day, so leave the time off")
	}
	return w, nil
}

func once(t time.Time, now time.Time) (When, error) {
	if t.Before(now.Add(-time.Minute)) {
		return When{}, fmt.Errorf("%s has already passed", t.Format("Mon Jan 2 3:04pm"))
	}
	say := "Once, " + t.Format("Mon Jan 2") + " at " + clock{t.Hour(), t.Minute()}.String()
	if t.Year() != now.Year() {
		say = "Once, " + t.Format("Mon Jan 2, 2006") + " at " + clock{t.Hour(), t.Minute()}.String()
	}
	return When{At: t, Say: say}, nil
}

func parseStamp(raw string, loc *time.Location) (time.Time, bool) {
	v := strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	v = strings.ReplaceAll(strings.ReplaceAll(v, "PM", "pm"), "AM", "am")
	for _, l := range atLayouts {
		if t, err := time.ParseInLocation(l, v, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func monthly(body string, t clock) (When, bool, error) {
	b := strings.TrimPrefix(strings.TrimPrefix(body, "monthly "), "every month ")
	b = strings.TrimPrefix(b, "on ")
	if !strings.HasPrefix(b, "the ") && b == body {
		return When{}, false, nil
	}
	m := monthDayRe.FindStringSubmatch(b)
	if m == nil {
		return When{}, false, nil
	}
	d, _ := strconv.Atoi(m[1])
	if d < 1 || d > 31 {
		return When{}, true, errors.New("a day of the month goes from 1 to 31")
	}
	every := 1
	if m[2] != "" {
		every, _ = strconv.Atoi(m[2])
		if every < 1 || every > 12 {
			return When{}, true, errors.New("every so many months goes from 1 to 12")
		}
	}
	w := When{Cron: t.cron(strconv.Itoa(d) + " * *"), Say: t.String() + " on the " + ordinal(d) + " of every month"}
	if every > 1 {
		w.Cron = t.cron(fmt.Sprintf("%d */%d *", d, every))
		w.Say = fmt.Sprintf("%s on the %s, every %d months from January", t.String(), ordinal(d), every)
	}
	if d > 28 {
		w.Say += ", skipping months too short to have one"
	}
	return w, true, nil
}

// date reads a month and day, once or every year. A date without a year that
// has passed is next year's.
func date(body string, t clock, now time.Time, loc *time.Location) (When, bool, error) {
	yearly := false
	if p := yearlyPrefix.FindString(body); p != "" {
		yearly, body = true, strings.TrimPrefix(body, p)
	}
	var mon, day, year int
	switch {
	case dateRe.MatchString(body):
		m := dateRe.FindStringSubmatch(body)
		if mon = monthIndex(m[1]); mon < 0 {
			return When{}, false, nil
		}
		day, _ = strconv.Atoi(m[2])
		year, _ = strconv.Atoi(m[3])
	case dateRevRe.MatchString(body):
		m := dateRevRe.FindStringSubmatch(body)
		if mon = monthIndex(m[2]); mon < 0 {
			return When{}, false, nil
		}
		day, _ = strconv.Atoi(m[1])
		year, _ = strconv.Atoi(m[3])
	case slashDateRe.MatchString(body):
		m := slashDateRe.FindStringSubmatch(body)
		mon, _ = strconv.Atoi(m[1])
		mon--
		day, _ = strconv.Atoi(m[2])
		year, _ = strconv.Atoi(m[3])
		if year > 0 && year < 100 {
			year += 2000
		}
		if mon < 0 || mon > 11 {
			return When{}, true, errors.New("that month is outside 1 to 12")
		}
	default:
		return When{}, false, nil
	}
	month := time.Month(mon + 1)
	if day < 1 || day > 31 || day > daysIn(month, 2024) {
		return When{}, true, fmt.Errorf("%s doesn't have a %s", month, ordinal(day))
	}
	if yearly {
		if year != 0 {
			return When{}, true, errors.New("a yearly date doesn't take a year")
		}
		w := When{Cron: t.cron(fmt.Sprintf("%d %d *", day, mon+1)), Say: "Every year on " + month.String()[:3] + " " + strconv.Itoa(day) + " at " + t.String()}
		if month == time.February && day == 29 {
			w.Say += ", so only in leap years"
		}
		return w, true, nil
	}
	if year == 0 {
		year = now.Year()
		if !t.on(time.Date(year, month, 1, 0, 0, 0, 0, loc), loc).AddDate(0, 0, day-1).After(now) {
			year++
		}
		for day > daysIn(month, year) {
			year++
		}
	}
	if day > daysIn(month, year) {
		return When{}, true, fmt.Errorf("%s %d has no %s", month, year, ordinal(day))
	}
	w, err := once(t.on(time.Date(year, month, day, 12, 0, 0, 0, loc), loc), now)
	return w, true, err
}

func daysIn(m time.Month, year int) int {
	return time.Date(year, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func monthIndex(s string) int {
	for i, m := range fullMonths {
		if len(s) >= 3 && strings.HasPrefix(m, s) || s == "sept" && i == 8 {
			return i
		}
	}
	return -1
}

// dayIndex reads a day's name from its first three letters on, plural or not.
func dayIndex(s string) int {
	s = strings.TrimSuffix(s, "s")
	switch s {
	case "tue", "tues":
		return 2
	case "thu", "thur", "thurs":
		return 4
	}
	for i, d := range fullDays {
		if len(s) >= 3 && strings.HasPrefix(d, s) {
			return i
		}
	}
	return -1
}

type days []int

func plainDays(body string) (days, bool) {
	b := strings.TrimPrefix(strings.TrimPrefix(body, "every "), "on ")
	words := strings.FieldsFunc(strings.NewReplacer(" and ", ",", "&", ",", "/", ",").Replace(b), func(r rune) bool {
		return r == ',' || r == ' '
	})
	var out days
	for _, w := range words {
		if w == "every" || w == "on" {
			continue
		}
		d := dayIndex(w)
		if d < 0 {
			return nil, false
		}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	return out, len(out) > 0
}

func (d days) field() string {
	switch {
	case slices.Equal(d, days{1, 2, 3, 4, 5}):
		return "1-5"
	case slices.Equal(d, days{0, 1, 2, 3, 4, 5, 6}):
		return "*"
	}
	parts := make([]string, len(d))
	for i, n := range d {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func (d days) say() string {
	switch d.field() {
	case "1-5":
		return "Monday to Friday"
	case "0,6":
		return "Saturday and Sunday"
	case "*":
		return "Every day"
	}
	names := make([]string, len(d))
	for i, n := range d {
		names[i] = capital(fullDays[n]) + "s"
	}
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
