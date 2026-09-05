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

const monthNames = `(january|february|march|april|may|june|july|august|september|october|november|december)`

var (
	isoDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	// "12 September 2026" and "12 September".
	dayFirst = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?\s+` + monthNames + `\b(?:,?\s+(\d{4})\b)?`)
	// "September 12, 2026" and "September 12".
	monthFirst = regexp.MustCompile(`(?i)\b` + monthNames + `\s+(\d{1,2})(?:st|nd|rd|th)?\b(?:,?\s+(\d{4})\b)?`)
)

// latestDate is the furthest ahead date named anywhere in the text.
func latestDate(text string, now time.Time) (time.Time, bool) {
	var best time.Time
	// Every date lands on midnight where the reader is, so a page's UTC
	// timestamp and a fixture written as a bare day compare as the same kind
	// of thing.
	keep := func(t time.Time, ok bool) {
		if !ok {
			return
		}
		if d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location()); d.After(best) {
			best = d
		}
	}
	for _, m := range isoDate.FindAllString(text, -1) {
		keep(parseDate(m))
	}
	for _, m := range dayFirst.FindAllStringSubmatch(text, -1) {
		keep(dateAt(m[1], m[2], m[3], now))
	}
	for _, m := range monthFirst.FindAllStringSubmatch(text, -1) {
		keep(dateAt(m[2], m[1], m[3], now))
	}
	// A bare month and year is coarse but datable, and it only counts when
	// nothing more precise was found, since "September 2026" sitting beside
	// "12 September 2026" is the same date written twice.
	if best.IsZero() {
		for _, m := range monthYear.FindAllStringSubmatch(text, -1) {
			keep(monthStart(m[1], m[2]))
		}
	}
	return best, !best.IsZero()
}

// dateAt normalises the pieces as they were written and hands them to the page
// date parser, which already knows the formats.
func dateAt(day, month, year string, now time.Time) (time.Time, bool) {
	assumed := year == ""
	if assumed {
		year = strconv.Itoa(now.Year())
	}
	month = strings.ToUpper(month[:1]) + strings.ToLower(month[1:])
	t, ok := parseDate(day + " " + month + " " + year)
	if !ok {
		return time.Time{}, false
	}
	// A date written without its year in late December means January, and
	// nothing names a fixture two months behind us as the next one.
	if assumed && t.Before(now.AddDate(0, -2, 0)) {
		t = t.AddDate(1, 0, 0)
	}
	return t, true
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
