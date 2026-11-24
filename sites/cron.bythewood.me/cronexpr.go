package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Expr is a five field cron line, minute hour day-of-month month day-of-week,
// read in the zone it is asked about.
type Expr struct {
	min, hour, dom, month, dow uint64
	// Classic cron runs a line naming both days when either matches, and only
	// when one of them is a star does the other decide alone.
	domStar, dowStar bool
	// nth is per weekday, bits 1 to 5 for the nth one in the month and bit 6
	// for the last, from a weekday written like thu#4 or mon#l.
	nth [7]uint8
}

var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

var dayNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

var shortcuts = map[string]string{
	"@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *", "@monthly": "0 0 1 * *",
	"@weekly": "0 0 * * 0", "@daily": "0 0 * * *", "@hourly": "0 * * * *",
}

func ParseCron(line string) (Expr, error) {
	line = strings.TrimSpace(strings.ToLower(line))
	if s, ok := shortcuts[line]; ok {
		line = s
	}
	f := strings.Fields(line)
	if len(f) != 5 {
		return Expr{}, errors.New("a cron line is five fields: minute hour day month weekday")
	}
	var e Expr
	var err error
	if e.min, err = field(f[0], 0, 59, nil); err != nil {
		return Expr{}, fmt.Errorf("minute: %w", err)
	}
	if e.hour, err = field(f[1], 0, 23, nil); err != nil {
		return Expr{}, fmt.Errorf("hour: %w", err)
	}
	if e.dom, err = field(f[2], 1, 31, nil); err != nil {
		return Expr{}, fmt.Errorf("day of month: %w", err)
	}
	if e.month, err = field(f[3], 1, 12, monthNames); err != nil {
		return Expr{}, fmt.Errorf("month: %w", err)
	}
	// 7 is Sunday too, since half the cron lines written by hand use it.
	if err = e.weekdays(f[4]); err != nil {
		return Expr{}, fmt.Errorf("weekday: %w", err)
	}
	if e.dow&(1<<7) != 0 {
		e.dow |= 1
	}
	e.domStar, e.dowStar = f[2] == "*", f[4] == "*"
	return e, nil
}

const lastBit = 6

// weekdays reads the weekday field, which takes thu#4 for the 4th Thursday of
// the month and mon#l for the last Monday on top of what the others take.
func (e *Expr) weekdays(s string) error {
	var plain []string
	for _, part := range strings.Split(s, ",") {
		name, n, ok := strings.Cut(part, "#")
		if !ok {
			plain = append(plain, part)
			continue
		}
		d, err := value(name, dayNames)
		if err != nil {
			return err
		}
		if d < 0 || d > 7 {
			return fmt.Errorf("%q is outside 0 to 7", name)
		}
		bit := lastBit
		if n != "l" {
			if bit, err = strconv.Atoi(n); err != nil || bit < 1 || bit > 5 {
				return fmt.Errorf("%q takes 1 to 5 or l for last after the #", part)
			}
		}
		e.nth[d%7] |= 1 << bit
	}
	if len(plain) == 0 {
		return nil
	}
	var err error
	e.dow, err = field(strings.Join(plain, ","), 0, 7, dayNames)
	return err
}

func field(s string, lo, hi int, names map[string]int) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(s, ",") {
		step := 1
		if i := strings.IndexByte(part, '/'); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n < 1 {
				return 0, fmt.Errorf("bad step in %q", part)
			}
			step, part = n, part[:i]
		}
		a, b := lo, hi
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			x, y, _ := strings.Cut(part, "-")
			var err error
			if a, err = value(x, names); err != nil {
				return 0, err
			}
			if b, err = value(y, names); err != nil {
				return 0, err
			}
		default:
			v, err := value(part, names)
			if err != nil {
				return 0, err
			}
			a, b = v, v
			if step > 1 {
				b = hi
			}
		}
		if a < lo || b > hi || a > b {
			return 0, fmt.Errorf("%q is outside %d to %d", part, lo, hi)
		}
		for v := a; v <= b; v += step {
			bits |= 1 << v
		}
	}
	return bits, nil
}

func value(s string, names map[string]int) (int, error) {
	if v, ok := names[s]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	return v, nil
}

func (e Expr) dayMatches(t time.Time) bool {
	dom := e.dom&(1<<t.Day()) != 0
	dow := e.dow&(1<<int(t.Weekday())) != 0
	if n := e.nth[t.Weekday()]; n != 0 {
		last := t.AddDate(0, 0, 7).Month() != t.Month()
		dow = dow || n&(1<<((t.Day()-1)/7+1)) != 0 || last && n&(1<<lastBit) != 0
	}
	switch {
	case e.domStar && e.dowStar:
		return true
	case e.domStar:
		return dow
	case e.dowStar:
		return dom
	}
	return dom || dow
}

// Next is the first time after `after` that the line matches, on the wall clock
// of loc. A time the spring change skips never matches, and an hour the fall
// change repeats matches once.
func (e Expr) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	after = after.In(loc)
	day := time.Date(after.Year(), after.Month(), after.Day(), 0, 0, 0, 0, loc)
	// Five years covers a February 29th line and anything else that can match.
	for i := 0; i < 366*5; i++ {
		d := day.AddDate(0, 0, i)
		if e.month&(1<<int(d.Month())) == 0 || !e.dayMatches(d) {
			continue
		}
		for h := 0; h < 24; h++ {
			if e.hour&(1<<h) == 0 {
				continue
			}
			for m := 0; m < 60; m++ {
				if e.min&(1<<m) == 0 {
					continue
				}
				t := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
				if t.Hour() != h || t.Minute() != m || t.Day() != d.Day() {
					continue
				}
				if t.After(after) {
					return t, true
				}
			}
		}
	}
	return time.Time{}, false
}
