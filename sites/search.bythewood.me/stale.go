package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// A passage describes the future as of the day it was written, and a model
// repeating it says a thing "is scheduled for April 2026" in September. Two
// rounds of telling it not to did not hold, which is the usual result of asking
// a 4B to compare dates in prose, so the check happens here instead.
//
// This warns rather than rewrites. The sentence is the model's and may be a
// fair report of what a stale page said, so the honest thing is to say which
// line is out of date rather than to quietly edit an answer whose whole claim
// is that it was checked.

var (
	// The ways an answer says a thing has not happened yet.
	futureTense = regexp.MustCompile(`(?i)\b(is|are|was|were|remains?|stays?)\s+(scheduled|planned|slated|expected|due|set)\b|` +
		`\b(will|is going to|are going to)\s+\w+|` +
		`\b(is|are)\s+(targeting|upcoming|forthcoming)\b|` +
		`\b(targets?|targeted for|scheduled for|planned for|due in|expected in)\b`)

	// Month and year, which is as precise as these sentences get.
	monthYear = regexp.MustCompile(`(?i)\b(january|february|march|april|may|june|july|august|september|october|november|december)\s+(\d{4})\b`)

	// The other half of the same bug. A live tracker page saying a mission "is
	// currently underway" was right the day it was written, and a model
	// repeating it months later says the mission is still flying.
	presentTense = regexp.MustCompile(`(?i)\b(is|are)\s+(currently|now|presently)\b|` +
		`\b(is|are)\s+(underway|under way|ongoing|active|in progress|in flight|tracking)\b|` +
		`\bcurrently\s+(active|underway|executing|running|flying)\b`)

	// "April 2, 2026" as well as a bare month and year, since a tracker page
	// dates things to the day.
	dayMonthYear = regexp.MustCompile(`(?i)\b(january|february|march|april|may|june|july|august|september|october|november|december)\s+\d{1,2},?\s+(\d{4})\b`)

	// A bare quarter or year on its own is too coarse to call stale, since
	// "early 2028" said in 2026 is fine.
	sentenceSplit = regexp.MustCompile(`(?m)[^.!?\n]+[.!?]?`)
)

// staleNow flags a sentence saying something is happening now while dating it
// to a month that has already finished.
func staleNow(text string, now time.Time) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range sentenceSplit.FindAllString(text, -1) {
		s := strings.TrimSpace(raw)
		if s == "" || !presentTense.MatchString(s) {
			continue
		}
		for _, re := range []*regexp.Regexp{dayMonthYear, monthYear} {
			var hit bool
			for _, m := range re.FindAllStringSubmatch(s, -1) {
				when, ok := monthStart(m[1], m[len(m)-1])
				if !ok {
					continue
				}
				if !when.AddDate(0, 1, 0).Before(truncMonth(now)) {
					continue
				}
				key := m[1] + m[len(m)-1]
				if seen[key] {
					hit = true
					continue
				}
				seen[key] = true
				hit = true
				out = append(out, fmt.Sprintf(
					"one line says this is happening now but dates it to %s %s, so it is describing what a source said at the time rather than today",
					strings.ToUpper(m[1][:1])+strings.ToLower(m[1][1:]), m[len(m)-1]))
			}
			if hit {
				break
			}
		}
	}
	return out
}

// staleFutures returns a warning for each sentence claiming something is still
// ahead on a month that has already finished.
func staleFutures(text string, now time.Time) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range sentenceSplit.FindAllString(text, -1) {
		s := strings.TrimSpace(raw)
		if s == "" || !futureTense.MatchString(s) {
			continue
		}
		for _, m := range monthYear.FindAllStringSubmatch(s, -1) {
			when, ok := monthStart(m[1], m[2])
			if !ok {
				continue
			}
			// The month has to be fully behind us. Something "scheduled for
			// September" on the 5th of September is not wrong.
			if !when.AddDate(0, 1, 0).Before(truncMonth(now)) {
				continue
			}
			key := m[1] + m[2]
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, fmt.Sprintf(
				"one line calls %s %s still upcoming, and it has passed, so the sources are older than the event",
				strings.ToUpper(m[1][:1])+strings.ToLower(m[1][1:]), m[2]))
		}
	}
	return out
}

func monthStart(name, year string) (time.Time, bool) {
	t, err := time.Parse("January 2006", strings.ToUpper(name[:1])+strings.ToLower(name[1:])+" "+year)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func truncMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}
