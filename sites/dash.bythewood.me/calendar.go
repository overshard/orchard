package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"
)

// What the next session has coming, for the markets brief's lean. Each note
// says which way it has tended to push, and the prompt treats them as tilts
// since none is strong alone.

// fomcDecisions is each meeting's second day, when the 2pm statement comes, and
// true marks projections. There's no machine readable calendar, so copy each
// year's from federalreserve.gov/monetarypolicy/fomccalendars.htm.
var fomcDecisions = map[string]bool{
	"2026-01-28": false, "2026-03-18": true, "2026-04-29": false, "2026-06-17": true,
	"2026-07-29": false, "2026-09-16": true, "2026-10-28": false, "2026-12-09": true,
	"2027-01-27": false, "2027-03-17": true, "2027-04-28": false, "2027-06-09": true,
	"2027-07-28": false, "2027-09-15": true, "2027-10-27": false, "2027-12-08": true,
}

// BLS refuses anything without a contact in the User-Agent, browsers included.
const contactAgent = "dash.bythewood.me (isaac@bythewood.me)"

type release struct {
	at    time.Time
	name  string
	short string
}

// The releases that move the whole market. Everything else on these calendars
// is regional, annual or too small to matter the next morning.
var bigReleases = []struct{ prefix, name, short string }{
	{"Consumer Price Index", "CPI inflation", "CPI"},
	{"Producer Price Index", "PPI wholesale inflation", "PPI"},
	{"Employment Situation", "The monthly jobs report", "JOBS"},
	{"Job Openings and Labor Turnover Survey", "JOLTS job openings", "JOLTS"},
	{"Gross Domestic Product,", "GDP", "GDP"},
	{"GDP (", "GDP", "GDP"},
	{"Personal Income and Outlays", "PCE inflation and spending", "PCE"},
}

func fetchReleases(ctx context.Context, g *Guard) []release {
	var out []release
	for _, src := range []struct{ endpoint, url string }{
		{"bls", "https://www.bls.gov/schedule/news_release/bls.ics"},
		{"bea", "https://www.bea.gov/news/schedule/ics/online-calendar-subscription.ics"},
	} {
		rs, err := fetchICS(ctx, g, src.endpoint, src.url)
		if err != nil {
			continue
		}
		out = append(out, rs...)
	}
	return out
}

func fetchICS(ctx context.Context, g *Guard, endpoint, url string) ([]release, error) {
	if err := g.Reserve(ctx, endpoint); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", contactAgent)
	resp, err := client.Do(req)
	if err != nil {
		g.Fail(endpoint, 0, 0)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		g.Fail(endpoint, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")))
		return nil, fmt.Errorf("%s: http %d", endpoint, resp.StatusCode)
	}
	rs := parseICS(io.LimitReader(resp.Body, maxBody))
	if len(rs) == 0 {
		g.Fail(endpoint, resp.StatusCode, 0)
		return nil, fmt.Errorf("%s: no events", endpoint)
	}
	g.Succeed(endpoint)
	return rs, nil
}

// parseICS keeps only the big releases. BLS writes its times in Eastern and
// BEA in UTC, and BEA folds long summaries onto a second line.
func parseICS(r io.Reader) []release {
	var lines []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			if len(lines) > 0 {
				lines[len(lines)-1] += l[1:]
			}
			continue
		}
		lines = append(lines, l)
	}

	var out []release
	var at time.Time
	var summary string
	for _, l := range lines {
		key, val, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch {
		case l == "BEGIN:VEVENT":
			at, summary = time.Time{}, ""
		case strings.HasPrefix(key, "DTSTART"):
			at = parseICSTime(val)
		case key == "SUMMARY":
			summary = strings.ReplaceAll(val, `\,`, ",")
		case l == "END:VEVENT":
			for _, b := range bigReleases {
				if strings.HasPrefix(summary, b.prefix) && !at.IsZero() {
					out = append(out, release{at, b.name, b.short})
					break
				}
			}
		}
	}
	return out
}

func parseICSTime(v string) time.Time {
	if t, err := time.Parse("20060102T150405Z", v); err == nil {
		return t
	}
	if t, err := time.ParseInLocation("20060102T150405", v, easternTime()); err == nil {
		return t
	}
	t, _ := time.ParseInLocation("20060102", v, easternTime())
	return t
}

// sessionNotes is what the calendar and the documented patterns say about one
// session, as of now. Weekdays stand in for trading days since there is no
// holiday list, which puts the turn of the month a day off around a holiday.
func sessionNotes(day time.Time, releases []release, vix float64, now time.Time) []string {
	day = day.In(easternTime())
	date := day.Format("2006-01-02")
	var notes []string

	for _, r := range releases {
		at := r.at.In(easternTime())
		if at.Format("2006-01-02") != date {
			continue
		}
		when := fmt.Sprintf("%s comes out at %s Eastern.", r.name, at.Format("3:04pm"))
		if !at.After(now) {
			when = fmt.Sprintf("%s came out at %s Eastern. The futures already reflect it, and a story since then may say what it showed.", r.name, at.Format("3:04pm"))
		}
		notes = append(notes, when+" Big release days have historically had larger moves in both directions and a higher average return.")
	}

	if sep, ok := fomcDecisions[date]; ok {
		n := "The Fed announces its rate decision at 2pm Eastern"
		if sep {
			n += ", with new projections"
		}
		notes = append(notes, n+". Stocks have historically drifted higher into a Fed decision, though less since 2015, and the afternoon can swing either way.")
	} else if _, ok := fomcDecisions[day.AddDate(0, 0, 1).Format("2006-01-02")]; ok {
		notes = append(notes, "The Fed meets, with its decision the next day. The day before a decision has historically leaned higher.")
	} else if _, ok := fomcDecisions[day.AddDate(0, 0, -1).Format("2006-01-02")]; ok {
		notes = append(notes, "The Fed announced its rate decision the afternoon before, and stories since then say what it decided.")
	}

	if turnOfMonth(day) {
		notes = append(notes, "It falls in the turn of the month, the last trading day and first three of a month, which has historically carried most of the month's gains. A mild push higher.")
	}

	if vix > 0 {
		notes = append(notes, fmt.Sprintf("A VIX of %.1f prices a typical daily move of about %.1f%% either way.", vix, vix/math.Sqrt(252)))
	}
	return notes
}

// weekNotes is the calendar for a week ahead line, day by day from Monday, with
// the VIX given once as a weekly move instead of on every day.
func weekNotes(monday time.Time, releases []release, vix float64, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s, the week of %s:\n", weekAhead, monday.Format("Monday January 2"))
	for d := monday; !weekend(d); d = d.AddDate(0, 0, 1) {
		for _, n := range sessionNotes(d, releases, 0, now) {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	if vix > 0 {
		fmt.Fprintf(&b, "- A VIX of %.1f prices a typical weekly move of about %.1f%% either way.\n", vix, vix/math.Sqrt(52))
	} else if !strings.Contains(b.String(), "\n- ") {
		b.WriteString("- Nothing scheduled and no calendar pattern.\n")
	}
	return b.String()
}

func turnOfMonth(day time.Time) bool {
	if weekend(day) {
		return false
	}
	if nextWeekday(day).Month() != day.Month() {
		return true
	}
	n := 0
	for d := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location()); d.Day() <= day.Day() && d.Month() == day.Month(); d = d.AddDate(0, 0, 1) {
		if !weekend(d) {
			n++
		}
	}
	return n <= 3
}

// calendarClause is a forward line's calendar as the page shows it beside the
// line, like FED 2PM · GDP 8:30AM, or each day's for the week ahead.
func calendarClause(l marketLine, releases []release, now time.Time) string {
	var parts []string
	day := l.day.In(easternTime())
	for d := day; ; d = d.AddDate(0, 0, 1) {
		prefix := ""
		if l.week {
			prefix = strings.ToUpper(d.Format("Mon")) + " "
		}
		parts = append(parts, dayEvents(d, releases, now, prefix)...)
		if !l.week || d.Weekday() == time.Friday {
			break
		}
	}
	if len(parts) == 0 {
		return "NOTHING MAJOR SCHEDULED"
	}
	return strings.Join(parts, " · ")
}

func dayEvents(day time.Time, releases []release, now time.Time, prefix string) []string {
	date := day.Format("2006-01-02")
	events := slices.Clone(releases)
	if _, ok := fomcDecisions[date]; ok {
		events = append(events, release{at: time.Date(day.Year(), day.Month(), day.Day(), 14, 0, 0, 0, easternTime()), short: "FED"})
	}
	slices.SortStableFunc(events, func(a, b release) int { return a.at.Compare(b.at) })
	var out, seen []string
	for _, e := range events {
		at := e.at.In(easternTime())
		if at.Format("2006-01-02") != date || slices.Contains(seen, e.short) {
			continue
		}
		seen = append(seen, e.short)
		when := clock(at)
		if !at.After(now) {
			when = "OUT"
		}
		out = append(out, prefix+e.short+" "+when)
	}
	return out
}

// clock is 8:30AM or 2PM.
func clock(t time.Time) string {
	if t.Minute() == 0 {
		return strings.ToUpper(t.Format("3PM"))
	}
	return strings.ToUpper(t.Format("3:04PM"))
}

// impliedMove is the typical move the VIX prices for a session, or a week,
// which is a fact beside the lean where the lean is a read.
func impliedMove(vix float64, week bool) string {
	if vix <= 0 {
		return ""
	}
	days := 252.0
	if week {
		days = 52
	}
	return fmt.Sprintf("±%.2f%%", vix/math.Sqrt(days))
}

// Upcoming is one entry on the footer's AHEAD line.
type Upcoming struct {
	Label string `json:"label"`
	When  string `json:"when"`
}

// ahead is the next few market moving dates, the releases and the Fed
// decisions together, within three weeks.
func ahead(releases []release, now time.Time, limit int) []Upcoming {
	et := easternTime()
	now = now.In(et)
	events := slices.Clone(releases)
	for date := range fomcDecisions {
		d, err := time.ParseInLocation("2006-01-02", date, et)
		if err == nil {
			events = append(events, release{at: d.Add(14 * time.Hour), short: "FED"})
		}
	}
	slices.SortFunc(events, func(a, b release) int { return a.at.Compare(b.at) })

	var out []Upcoming
	for _, e := range events {
		if !e.at.After(now) || e.at.After(now.AddDate(0, 0, 21)) {
			continue
		}
		at := e.at.In(et)
		when := strings.ToUpper(at.Format("Mon Jan 2"))
		switch at.Format("2006-01-02") {
		case now.Format("2006-01-02"):
			when = "TODAY " + strings.ToUpper(at.Format("3:04pm"))
		case now.AddDate(0, 0, 1).Format("2006-01-02"):
			when = "TOMORROW " + strings.ToUpper(at.Format("3:04pm"))
		}
		out = append(out, Upcoming{e.short, when})
		if len(out) == limit {
			break
		}
	}
	return out
}
