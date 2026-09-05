package skills

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Time answers what the clock and the calendar say. It never leaves the
// process, so it is the cheapest skill here and the one most certain to be
// right, which is the opposite of how the web handles the same question: a
// search for the time in another city returns a page that has to be rendered
// and read, and a search for how many days until a date returns a countdown
// widget the model cannot see.
type Time struct{}

func (Time) Card() Card {
	return Card{
		Name: "time",
		Does: "gives the current time in a named city or zone, or counts the days between today and a date.",
		Fires: []string{
			"what time is it in tokyo",
			"how many days until christmas",
			"what is the date today",
			"what time is it in london right now",
			"how many days until new year",
		},
		NotFor: []string{
			"what time does the game start",
			"why do we have time zones",
			"when was the declaration of independence signed",
			"what time does the shop close",
			"how long does it take to fly to tokyo",
		},
		Keywords: []string{"what time is it", "days until", "what is the date", "todays date", "what day is it"},
	}
}

// zones are the cities worth naming. A full tz database lookup would take any
// string and get "what time is it in the morning" wrong, so this is a list.
var zones = map[string]string{
	"utc": "UTC", "gmt": "UTC",
	"london": "Europe/London", "uk": "Europe/London", "england": "Europe/London",
	"paris": "Europe/Paris", "berlin": "Europe/Berlin", "madrid": "Europe/Madrid",
	"rome": "Europe/Rome", "amsterdam": "Europe/Amsterdam", "dublin": "Europe/Dublin",
	"lisbon": "Europe/Lisbon", "moscow": "Europe/Moscow", "istanbul": "Europe/Istanbul",
	"stockholm": "Europe/Stockholm", "oslo": "Europe/Oslo", "zurich": "Europe/Zurich",
	"new york": "America/New_York", "nyc": "America/New_York", "boston": "America/New_York",
	"chicago": "America/Chicago", "denver": "America/Denver", "phoenix": "America/Phoenix",
	"los angeles": "America/Los_Angeles", "la": "America/Los_Angeles",
	"san francisco": "America/Los_Angeles", "seattle": "America/Los_Angeles",
	"toronto": "America/Toronto", "vancouver": "America/Vancouver",
	"mexico city": "America/Mexico_City", "sao paulo": "America/Sao_Paulo",
	"tokyo": "Asia/Tokyo", "japan": "Asia/Tokyo", "seoul": "Asia/Seoul",
	"beijing": "Asia/Shanghai", "shanghai": "Asia/Shanghai", "china": "Asia/Shanghai",
	"hong kong": "Asia/Hong_Kong", "singapore": "Asia/Singapore",
	"mumbai": "Asia/Kolkata", "delhi": "Asia/Kolkata", "india": "Asia/Kolkata",
	"dubai": "Asia/Dubai", "tel aviv": "Asia/Jerusalem",
	"sydney": "Australia/Sydney", "melbourne": "Australia/Melbourne",
	"auckland": "Pacific/Auckland", "perth": "Australia/Perth",
	"cairo": "Africa/Cairo", "lagos": "Africa/Lagos", "nairobi": "Africa/Nairobi",
	"johannesburg": "Africa/Johannesburg",
}

func (Time) Run(ctx context.Context, question string, d Deps) (*Result, error) {
	start := d.now()
	l := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(question), "?"))

	if text, ok := daysUntil(l, d); ok {
		return &Result{Skill: "time", Shape: "factual", Text: text,
			Elapsed: d.now().Sub(start).Round(time.Millisecond).String()}, nil
	}
	if text, ok := clockIn(l, d); ok {
		return &Result{Skill: "time", Shape: "factual", Text: text,
			Elapsed: d.now().Sub(start).Round(time.Millisecond).String()}, nil
	}
	if containsAny(l, "what is the date", "what's the date", "todays date", "today's date",
		"what day is it", "what is today", "what time is it") {
		now := d.now()
		text := fmt.Sprintf("**%s**\n\n- **%s** locally\n- **%s** UTC",
			now.Format("Monday, 2 January 2006"),
			now.Format("3:04 PM MST"), now.UTC().Format("15:04"))
		return &Result{Skill: "time", Shape: "factual", Text: text,
			Elapsed: d.now().Sub(start).Round(time.Millisecond).String()}, nil
	}
	return nil, nil
}

func clockIn(l string, d Deps) (string, bool) {
	if !containsAny(l, "what time", "time is it", "current time", "local time") {
		return "", false
	}
	name, tz := matchZone(l)
	if tz == "" {
		return "", false
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", false
	}
	now := d.now()
	there := now.In(loc)
	// The day difference is the thing people actually want and the thing a
	// bare clock reading hides.
	rel := ""
	switch dayDiff := there.YearDay() - now.YearDay(); {
	case there.Year() > now.Year() || dayDiff == 1:
		rel = ", which is tomorrow"
	case there.Year() < now.Year() || dayDiff == -1:
		rel = ", which is yesterday"
	}
	text := fmt.Sprintf("**%s in %s**%s\n\n- **%s** there\n- **%s** where you are\n- **%s** UTC",
		there.Format("3:04 PM"), title(name), rel,
		there.Format("Monday, 2 January"), now.Format("3:04 PM MST"), now.UTC().Format("15:04"))
	return text, true
}

func matchZone(l string) (string, string) {
	best, bestTZ := "", ""
	for name, tz := range zones {
		if !strings.Contains(l, name) {
			continue
		}
		// Longest name wins, so "new york" beats the "la" inside it elsewhere.
		if len(name) > len(best) {
			best, bestTZ = name, tz
		}
	}
	return best, bestTZ
}

func daysUntil(l string, d Deps) (string, bool) {
	if !containsAny(l, "days until", "days till", "days to ", "how long until") {
		return "", false
	}
	now := d.now()
	target, label, ok := namedDate(l, now)
	if !ok {
		return "", false
	}
	days := int(truncateDay(target).Sub(truncateDay(now)).Hours() / 24)
	switch {
	case days == 0:
		return fmt.Sprintf("**Today.**\n\n- **%s** is **%s**", label, target.Format("Monday, 2 January 2006")), true
	case days < 0:
		return fmt.Sprintf("**%d days ago.**\n\n- **%s** was **%s**",
			-days, label, target.Format("Monday, 2 January 2006")), true
	}
	weeks := ""
	if days >= 14 {
		weeks = fmt.Sprintf("\n- about **%d weeks**", days/7)
	}
	return fmt.Sprintf("**%d days.**\n\n- **%s** falls on **%s**%s",
		days, label, target.Format("Monday, 2 January 2006"), weeks), true
}

// namedDate handles the handful of dates people count down to. Anything else
// falls through to the web, which is right, because a made up date is worse
// than a slow answer.
func namedDate(l string, now time.Time) (time.Time, string, bool) {
	y := now.Year()
	fixed := []struct {
		words []string
		label string
		m     time.Month
		d     int
	}{
		{[]string{"christmas"}, "Christmas Day", time.December, 25},
		{[]string{"christmas eve"}, "Christmas Eve", time.December, 24},
		{[]string{"new year", "new years", "new year's"}, "New Year's Day", time.January, 1},
		{[]string{"halloween"}, "Halloween", time.October, 31},
		{[]string{"valentine"}, "Valentine's Day", time.February, 14},
		{[]string{"independence day", "4th of july", "fourth of july"}, "Independence Day", time.July, 4},
		{[]string{"new years eve", "new year's eve"}, "New Year's Eve", time.December, 31},
	}
	best := -1
	for i, f := range fixed {
		for _, w := range f.words {
			if strings.Contains(l, w) && (best < 0 || len(w) > len(fixed[best].words[0])) {
				best = i
			}
		}
	}
	if best < 0 {
		return time.Time{}, "", false
	}
	f := fixed[best]
	t := time.Date(y, f.m, f.d, 0, 0, 0, 0, now.Location())
	if t.Before(truncateDay(now)) {
		t = t.AddDate(1, 0, 0)
	}
	return t, f.label, true
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func title(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		if len(p) <= 3 && (p == "uk" || p == "la" || p == "nyc" || p == "utc" || p == "gmt") {
			parts[i] = strings.ToUpper(p)
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
