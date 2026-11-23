package main

import (
	"math/bits"
	"net/http"
	"slices"
	"time"
)

// monthItem is one job on one day of the month calendar, its first fire that
// day and how many fires it has that day.
type monthItem struct {
	JobID   int64
	Name    string
	Kind    string
	At      time.Time
	Count   int
	Once    bool
	Snooze  bool
	Failing bool
}

type monthDay struct {
	Day   time.Time
	In    bool
	Today bool
	Past  bool
	Items []monthItem
}

type month struct {
	Start, Prev, Next time.Time
	Weeks             [][]monthDay
	Fires, Days       int
}

// month lays out the weeks that hold first, Sunday first, with every enabled
// job on the days it fires. A repeating job is drawn from its cron line across
// the whole month, past days too, so the pattern reads at a glance.
func (s *site) month(jobs []Job, first, now time.Time) month {
	now = now.In(s.loc)
	today := midnight(now)
	first = time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, s.loc)
	end := first.AddDate(0, 1, 0)
	m := month{Start: first, Prev: first.AddDate(0, -1, 0), Next: end}

	byDay := map[int][]monthItem{}
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		it := monthItem{JobID: j.ID, Name: j.Name, Kind: j.Kind, Once: j.Once(), Snooze: j.CreatedBy == "snooze", Failing: j.LastErr != ""}
		if j.Once() {
			if at := j.At.In(s.loc); !j.At.IsZero() && !at.Before(first) && at.Before(end) {
				it.At, it.Count = at, 1
				byDay[at.Day()] = append(byDay[at.Day()], it)
			}
			continue
		}
		e, err := ParseCron(j.Cron)
		if err != nil {
			continue
		}
		perDay := bits.OnesCount64(e.hour) * bits.OnesCount64(e.min)
		for d := first; d.Before(end); d = d.AddDate(0, 0, 1) {
			t, ok := e.Next(d.Add(-time.Second), s.loc)
			if !ok || !sameDay(t, d) {
				continue
			}
			it.At, it.Count = t, perDay
			byDay[d.Day()] = append(byDay[d.Day()], it)
		}
	}

	start := first.AddDate(0, 0, -int(first.Weekday()))
	for d := start; d.Before(end) || d.Weekday() != time.Sunday; d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Sunday {
			m.Weeks = append(m.Weeks, nil)
		}
		md := monthDay{Day: d, In: d.Month() == first.Month(), Today: sameDay(d, today), Past: d.Before(today)}
		if md.In {
			md.Items = byDay[d.Day()]
			slices.SortStableFunc(md.Items, func(a, b monthItem) int { return a.At.Compare(b.At) })
			for _, it := range md.Items {
				m.Fires += it.Count
			}
			if len(md.Items) > 0 {
				m.Days++
			}
		}
		m.Weeks[len(m.Weeks)-1] = append(m.Weeks[len(m.Weeks)-1], md)
	}
	return m
}

// List is the days of the month that have anything, for a phone, where the
// grid only has room for dots.
func (m month) List() []monthDay {
	var out []monthDay
	for _, w := range m.Weeks {
		for _, d := range w {
			if d.In && len(d.Items) > 0 {
				out = append(out, d)
			}
		}
	}
	return out
}

func (s *site) calendarPage(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.Jobs()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	now := time.Now().In(s.loc)
	first := now
	if t, err := time.ParseInLocation("2006-01", r.URL.Query().Get("m"), s.loc); err == nil {
		first = t
	}
	m := s.month(jobs, first, now)
	s.render(w, 200, "calendar.html", map[string]any{"Title": "Calendar", "Month": m,
		"ThisMonth": m.Start.Year() == now.Year() && m.Start.Month() == now.Month(), "Suite": s.chrome("calendar", jobs)})
}
