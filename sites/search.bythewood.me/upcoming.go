package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A question about what is next is answered by a date, so whether an answer
// answered it is arithmetic and belongs here rather than in a prompt. The
// contract tells the model which dates can answer and a 4B still writes the
// ones its passages gave it, which for a team that played on Friday is Friday.
//
// The test is deliberately weak. One date anywhere in the answer that is today
// or later leaves it alone, because a fixture list names the last result as
// background and that is fine. Nothing at all dated today or later means the
// answer cannot contain the thing that was asked for, whatever else it holds.

// Abbreviations included, since a fixture list writes "9 Sept 2026" far more
// often than it writes the month out.
const monthNames = `(january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sept|sep|oct|nov|dec)\.?`

var (
	isoDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	// "12 September 2026" and "12 September".
	dayFirst = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?\s+` + monthNames + `\b(?:,?\s+(\d{4})\b)?`)
	// "September 12, 2026" and "September 12".
	monthFirst = regexp.MustCompile(`(?i)\b` + monthNames + `\s+(\d{1,2})(?:st|nd|rd|th)?\b(?:,?\s+(\d{4})\b)?`)

	months = map[string]time.Month{
		"jan": time.January, "feb": time.February, "mar": time.March,
		"apr": time.April, "may": time.May, "jun": time.June,
		"jul": time.July, "aug": time.August, "sep": time.September,
		"oct": time.October, "nov": time.November, "dec": time.December,
	}
)

// datesIn is every date named in the text, each at midnight where the reader
// is, so a page's UTC timestamp and a fixture written as a bare day compare as
// the same kind of thing.
func datesIn(text string, now time.Time) []time.Time {
	var out []time.Time
	add := func(t time.Time, ok bool) {
		if ok {
			out = append(out, time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location()))
		}
	}
	for _, m := range isoDate.FindAllString(text, -1) {
		add(parseDate(m))
	}
	for _, m := range dayFirst.FindAllStringSubmatch(text, -1) {
		add(dateAt(m[1], m[2], m[3], now))
	}
	for _, m := range monthFirst.FindAllStringSubmatch(text, -1) {
		add(dateAt(m[2], m[1], m[3], now))
	}
	// A bare month and year is coarse but datable, and it only counts when
	// nothing more precise was found, since "September 2026" sitting beside
	// "12 September 2026" is the same date written twice.
	if len(out) == 0 {
		for _, m := range monthYear.FindAllStringSubmatch(text, -1) {
			add(monthStart(m[1], m[2]))
		}
	}
	return out
}

// dateAt builds a date from the pieces as they were written.
func dateAt(day, month, year string, now time.Time) (time.Time, bool) {
	m, ok := months[strings.ToLower(month)[:3]]
	if !ok {
		return time.Time{}, false
	}
	d, err := strconv.Atoi(day)
	if err != nil || d < 1 || d > 31 {
		return time.Time{}, false
	}
	y := now.Year()
	assumed := year == ""
	if !assumed {
		if y, err = strconv.Atoi(year); err != nil {
			return time.Time{}, false
		}
	}
	t := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	// 31 February rolls over into March rather than failing, and a date that
	// does not exist is not a date.
	if t.Day() != d {
		return time.Time{}, false
	}
	// A date written without its year in late December means January, and
	// nothing names a fixture two months behind us as the next one.
	if assumed && t.Before(now.AddDate(0, -2, 0)) {
		t = t.AddDate(1, 0, 0)
	}
	return t, true
}

// latestDate is the furthest ahead date named anywhere in the text.
func latestDate(text string, now time.Time) (time.Time, bool) {
	var best time.Time
	for _, d := range datesIn(text, now) {
		if d.After(best) {
			best = d
		}
	}
	return best, !best.IsZero()
}

// earliestFuture is the soonest date today or later, which for this shape is
// the one being asked for.
func earliestFuture(text string, now time.Time) (time.Time, bool) {
	var best time.Time
	for _, d := range datesIn(text, now) {
		if d.Before(truncDay(now)) {
			continue
		}
		if best.IsZero() || d.Before(best) {
			best = d
		}
	}
	return best, !best.IsZero()
}

// A page stamps itself with the day it was last touched, and that date is not a
// fixture. It is the one false positive worth spending a regex on, since a
// schedule page updated this morning would otherwise look like a match today.
var metaLine = regexp.MustCompile(`(?i)(updated|published|posted|written|reviewed|retrieved|copyright|all rights reserved|subscribe)`)

// fixtureDates is every date on a line that is not a page stamping itself.
func fixtureDates(text string, now time.Time) []time.Time {
	var out []time.Time
	for _, line := range strings.Split(text, "\n") {
		if metaLine.MatchString(line) {
			continue
		}
		out = append(out, datesIn(line, now)...)
	}
	return out
}

// sooner catches the answer that took a page at its word. A fixture list
// covering one competition calls its own first match the next match, so a page
// of league fixtures says 12 September while a page carrying the cup says the
// 9th, and picking between them is a date comparison across five documents,
// which is the thing a 4B does worst.
//
// It warns rather than rewrites, because the earlier date can legitimately be
// something else the passage happened to mention.
func sooner(text string, passages []Passage, now time.Time) []string {
	lead, ok := earliestFuture(text, now)
	if !ok {
		return nil
	}
	var best time.Time
	for _, p := range passages {
		for _, d := range fixtureDates(p.Text, now) {
			if d.Before(truncDay(now)) {
				continue
			}
			if best.IsZero() || d.Before(best) {
				best = d
			}
		}
	}
	if best.IsZero() || !best.Before(lead) {
		return nil
	}
	return []string{fmt.Sprintf(
		"a source names %s, which is sooner than the %s this leads with, so check that the earlier one is not a fixture in a competition the other pages leave out",
		best.Format("2 January"), lead.Format("2 January"))}
}

// noFutureDate reports whether nothing in the answer is dated today or later,
// which for this shape means the answer does not contain what was asked for.
func noFutureDate(text string, now time.Time) bool {
	d, ok := latestDate(text, now)
	if !ok {
		return true
	}
	return d.Before(truncDay(now))
}

// pastOnly is the reader-facing half of the same check.
func pastOnly(text string, now time.Time) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	d, ok := latestDate(text, now)
	if !ok {
		return []string{"this names no date, and the question asked when something happens next"}
	}
	if d.Before(truncDay(now)) {
		return []string{fmt.Sprintf(
			"every date here has already passed, the latest is %s, so the pages found cover what has already happened rather than what is next",
			d.Format("2 January 2006"))}
	}
	return nil
}

// scheduleAge warns when the freshest page behind a schedule is old enough
// that the fixture has probably moved. The model was asked to say this itself
// for one round and wrote that its sources were published in late September on
// the fifth of September, which is the same lesson stale.go opens with: a 4B
// does not compare dates in prose, so the comparison happens in Go and the
// answer says nothing about it.
func scheduleAge(sources []Source, now time.Time) []string {
	var newest time.Time
	for _, s := range sources {
		if t, ok := parseDate(s.Published); ok && t.After(newest) {
			newest = t
		}
	}
	if newest.IsZero() {
		return []string{"none of these pages says when it was written, so there is no telling whether this schedule is current"}
	}
	if days := int(truncDay(now).Sub(truncDay(newest)).Hours() / 24); days > 14 {
		return []string{fmt.Sprintf(
			"the newest page behind this was written %d days ago, and a schedule moves, so check it against the official one", days)}
	}
	return nil
}

// upcomingHint is the retry's instruction. The first search found the right
// subject and the wrong half of it, so re-planning has to change the words
// rather than the topic.
func upcomingHint() string {
	return "A previous search found only events that have already happened, and the question asks what is next. " +
		"Write queries for the schedule itself, using words like schedule, fixtures, upcoming or next, " +
		"and drop any word about a result, a score or what happened."
}
