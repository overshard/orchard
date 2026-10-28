package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// What the archive says about a TODAY point: whether it's new since the last
// slot, what was said about it on the days before, and how big each day was.

const (
	// A story still running after this many days is a different story.
	timelineDays = 5
	timelineMax  = 4

	intensityDays = 30
)

// since is every brief a desk wrote from from on, oldest first.
func (a briefArchive) since(desk string, from time.Time) []Brief {
	f, err := os.Open(a.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Brief
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var line archived
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.Desk == desk && line.Brief.Slot >= from.Unix() {
			out = append(out, line.Brief)
		}
	}
	slices.SortStableFunc(out, func(a, b Brief) int { return cmp.Compare(a.Slot, b.Slot) })
	return out
}

// samePoint is two points about one story, by a shared link or three words of
// substance in common between what each said and its first outlet's title.
func samePoint(a, b Point) bool {
	for _, l := range a.Links {
		if slices.ContainsFunc(b.Links, func(m Link) bool { return m.URL == l.URL }) {
			return true
		}
	}
	return sameEvent(pointWords(a), pointWords(b))
}

func pointWords(p Point) []string {
	text := p.Text
	if len(p.Links) > 0 {
		text += " " + p.Links[0].Title
	}
	return significant(text)
}

// mark is NEW for a story the last brief didn't have and UPDATED for one it
// had that an outlet has reported on since.
func mark(prev Brief, pt Point, cs []cluster) string {
	if len(prev.Points) == 0 {
		return ""
	}
	i := slices.IndexFunc(prev.Points, func(p Point) bool { return samePoint(p, pt) })
	if i < 0 {
		return "NEW"
	}
	since := time.Unix(prev.Slot, 0)
	for _, c := range cs {
		for _, s := range c.stories {
			known := slices.ContainsFunc(prev.Points[i].Links, func(l Link) bool { return l.URL == s.url })
			if s.at.After(since) && !known {
				return "UPDATED"
			}
		}
	}
	return ""
}

// Step is what TODAY said about a story on one earlier day.
type Step struct {
	Day  string `json:"day"`
	Text string `json:"text"`
}

// timeline is the last thing said about the point on each earlier day it was
// on TODAY, oldest first, so a story running for days shows how it got here.
func timeline(past []Brief, pt Point, at time.Time) []Step {
	today := at.In(easternTime()).Format("2006-01-02")
	var days []string
	byDay := map[string]Step{}
	for _, b := range past {
		day := time.Unix(b.Slot, 0).In(easternTime())
		if day.Format("2006-01-02") >= today {
			continue
		}
		for _, p := range b.Points {
			if p.Text != "" && samePoint(p, pt) {
				key := day.Format("2006-01-02")
				if _, ok := byDay[key]; !ok {
					days = append(days, key)
				}
				byDay[key] = Step{Day: strings.ToUpper(day.Format("Mon Jan 2")), Text: p.Text}
				break
			}
		}
	}
	var out []Step
	for _, d := range days[max(0, len(days)-timelineMax):] {
		out = append(out, byDay[d])
	}
	return out
}

// DayShare is one day on the intensity strip, the biggest share of outlets
// any one story had at any slot that day.
type DayShare struct {
	Day   string `json:"day"`
	Share int    `json:"share"`
	Level string `json:"level,omitempty"`
}

// intensity is the last thirty days of how big the news was, off the read each
// brief kept, with a day nothing was read on left at zero.
func intensity(past []Brief, at time.Time) []DayShare {
	et := easternTime()
	end := at.In(et)
	best := map[string]DayShare{}
	for _, b := range past {
		if b.Read == nil {
			continue
		}
		var top, outlets int
		if _, err := fmt.Sscanf(b.Read.Spread, "TOP STORY ON %d OF %d OUTLETS", &top, &outlets); err != nil || outlets == 0 {
			continue
		}
		key := time.Unix(b.Slot, 0).In(et).Format("2006-01-02")
		if share := top * 100 / outlets; share > best[key].Share {
			level, _ := pulse(top, outlets)
			best[key] = DayShare{Share: share, Level: level}
		}
	}
	out := make([]DayShare, intensityDays)
	for i := range out {
		d := end.AddDate(0, 0, i-intensityDays+1)
		s := best[d.Format("2006-01-02")]
		s.Day = strings.ToUpper(d.Format("Mon Jan 2"))
		out[i] = s
	}
	return out
}

// weekReview is the week's closed sessions as the markets desk explained them,
// one event a session, so Saturday's look back still has the reasons after the
// feeds have moved on.
func weekReview(past []Brief, monday time.Time) []cluster {
	et := easternTime()
	byDay := map[string]cluster{}
	var days []string
	for _, b := range past {
		slot := time.Unix(b.Slot, 0).In(et)
		for _, p := range b.Points {
			if p.Move == "" || strings.HasSuffix(p.Move, " SO FAR") || p.Label == lastWeek || len(p.Links) == 0 || p.Text == "" {
				continue
			}
			day := slot
			if p.Label != "TODAY" {
				day = prevWeekday(slot)
			}
			day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, et)
			if day.Before(monday) || weekend(day) {
				continue
			}
			c := cluster{session: day, newest: slot}
			for i, l := range p.Links {
				s := story{title: l.Title, url: l.URL, source: l.Source, lean: outletLean(l.Source), at: slot, words: significant(l.Title)}
				if i == 0 {
					s.desc = fmt.Sprintf("The S&P 500 was %s %s, explained then as: %s", p.Lean, p.Move, p.Text)
				}
				c.stories = append(c.stories, s)
			}
			key := day.Format("2006-01-02")
			if _, ok := byDay[key]; !ok {
				days = append(days, key)
			}
			byDay[key] = c
		}
	}
	slices.Sort(days)
	var out []cluster
	for _, d := range days {
		out = append(out, byDay[d])
	}
	return out
}

func prevWeekday(t time.Time) time.Time {
	t = t.AddDate(0, 0, -1)
	for weekend(t) {
		t = t.AddDate(0, 0, -1)
	}
	return t
}

// outletLean is a market outlet's AllSides lean by its name, center when it
// isn't one of the market feeds.
func outletLean(name string) string {
	for _, f := range marketFeeds {
		if f.name == name {
			return f.lean
		}
	}
	return "C"
}
