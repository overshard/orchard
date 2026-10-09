package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Pulse is the line over a feed. Its level is worked out from points and age
// on every poll, since a thread takes off inside an hour, and under it sits the
// model's read of the page from the last brief, which says what the numbers
// can't.
type Pulse struct {
	Level string `json:"level"`
	Label string `json:"label"`
	Text  string `json:"text"`
	Read  string `json:"read,omitempty"`
	Links []Link `json:"links,omitempty"`
}

// attachFeedReads copies the feeds brief onto the live pulses, which every poll
// rebuilds from scratch.
func (st *State) attachFeedReads() {
	for _, p := range []struct {
		label string
		pulse *Pulse
	}{{"HN", &st.HNPulse}, {"LOBSTERS", &st.LobstersPulse}} {
		p.pulse.Read, p.pulse.Links = "", nil
		for _, pt := range st.Briefs.Feeds.Points {
			if pt.Label == p.label {
				p.pulse.Read, p.pulse.Links = pt.Text, pt.Links
			}
		}
	}
}

// feedItem is a story with what the pulse needs and the row doesn't show.
type feedItem struct {
	title    string
	points   int
	comments int
	age      time.Duration
}

// pulseScale is what counts as big on one site. Lobsters is about a tenth of
// Hacker News, so the same numbers can't serve both.
type pulseScale struct {
	hugePoints, freshPoints, hugeComments int
	busyPoints, busyComments              int
	fresh                                 time.Duration
}

var (
	hnScale       = pulseScale{1000, 500, 700, 400, 400, 6 * time.Hour}
	lobstersScale = pulseScale{150, 80, 100, 60, 50, 12 * time.Hour}
)

func feedPulse(items []feedItem, sc pulseScale) Pulse {
	if len(items) == 0 {
		return Pulse{}
	}
	// In the order the rows are drawn, so a story can be named by its row and
	// the top row never gets repeated back.
	items = slices.Clone(items)
	slices.SortStableFunc(items, func(a, b feedItem) int { return b.points - a.points })
	row := func(i int) string {
		if i < storiesShown {
			return fmt.Sprintf("No. %d", i+1)
		}
		return short(items[i].title)
	}

	hot := -1
	for i, it := range items {
		if it.age <= sc.fresh && it.points >= sc.freshPoints || it.age <= 24*time.Hour && it.points >= sc.hugePoints {
			hot = i
			break
		}
	}
	argued := 0
	for i, it := range items {
		if it.comments > items[argued].comments {
			argued = i
		}
	}
	big := items[0].points >= sc.hugePoints || items[argued].comments >= sc.hugeComments
	busy := items[0].points >= sc.busyPoints || items[argued].comments >= sc.busyComments

	var p Pulse
	switch {
	case hot >= 0:
		p.Level, p.Label = "hot", "BLOWING UP"
		p.Text = fmt.Sprintf("%s hit %d points in %s.", row(hot), items[hot].points, hours(items[hot].age))
	case big:
		p.Level, p.Label = "big", "BIG THREAD"
	case busy:
		p.Level, p.Label = "busy", "BUSY"
	default:
		p.Level, p.Label = "quiet", "QUIET"
	}
	return p
}

func short(title string) string {
	if r := []rune(title); len(r) > 44 {
		cut := string(r[:44])
		if i := strings.LastIndex(cut, " "); i > 24 {
			cut = cut[:i]
		}
		return "“" + cut + "...”"
	}
	return "“" + title + "”"
}

func hours(d time.Duration) string {
	if d < time.Hour {
		return "under an hour"
	}
	if d < 2*time.Hour {
		return "an hour"
	}
	return fmt.Sprintf("%d hours", int(d.Hours()))
}
