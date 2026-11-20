package main

import (
	"fmt"
	"slices"
	"time"
)

// tick is one run in a job's strip, its class in o-ticks and its tooltip.
type tick struct{ Class, Tip string }

const tickCount = 14

var resultClass = map[string]string{"pushed": "", "quiet": "q", "failed": "bad", "missed": "warn"}

// ticks is a job's last runs, oldest first and padded on the left so every
// strip on the page is the same width.
func (s *site) ticks(runs []Run) []tick {
	out := make([]tick, 0, tickCount)
	for range tickCount - len(runs) {
		out = append(out, tick{Class: "none", Tip: "no run"})
	}
	for _, r := range runs {
		tip := s.eastern(r.At) + " · " + r.Result()
		switch {
		case r.Err != "":
			tip += "\n" + clip(r.Err, 80)
		case r.Pushed:
			tip += "\n" + clip(r.Title, 60)
		}
		out = append(out, tick{Class: resultClass[r.Result()], Tip: tip})
	}
	return out
}

// jobRow is a job as the jobs table shows it.
type jobRow struct {
	Job
	Ticks []tick
	Last  string
	// Snoozed is when a snooze pressed on this job's push will land, and on
	// a snooze's own row Of is the job it came from.
	Snoozed time.Time
	Of      string
}

// snoozes maps each job to the soonest pending snooze made from its push.
// One made before snoozes kept their job is matched by name instead.
func snoozes(jobs []Job) map[int64]time.Time {
	byName := map[string]int64{}
	for _, j := range jobs {
		if j.CreatedBy != "snooze" {
			byName[j.Name] = j.ID
		}
	}
	out := map[int64]time.Time{}
	for _, j := range jobs {
		if j.CreatedBy != "snooze" || !j.Enabled || j.NextRun.IsZero() {
			continue
		}
		of := j.SnoozeOf
		if of == 0 {
			of = byName[j.Name]
		}
		if of == 0 {
			continue
		}
		if t, ok := out[of]; !ok || j.NextRun.Before(t) {
			out[of] = j.NextRun
		}
	}
	return out
}

func (s *site) rows(jobs []Job) []jobRow {
	last, _ := s.store.LastRuns(tickCount)
	snz := snoozes(jobs)
	names := map[int64]string{}
	for _, j := range jobs {
		names[j.ID] = j.Name
	}
	out := make([]jobRow, len(jobs))
	for i, j := range jobs {
		r := jobRow{Job: j, Ticks: s.ticks(last[j.ID]), Snoozed: snz[j.ID]}
		if runs := last[j.ID]; len(runs) > 0 {
			r.Last = runs[len(runs)-1].Result()
		}
		if j.CreatedBy == "snooze" {
			r.Of = names[j.SnoozeOf]
			if r.Of == "" {
				r.Of = j.Name
			}
		}
		out[i] = r
	}
	return out
}

// weekItem is one line in a day of the week agenda. A job that fires more
// than a few times a day is one line with a count instead of a line a fire.
type weekItem struct {
	JobID   int64
	Name    string
	Kind    string
	At      time.Time
	Once    bool
	Snooze  bool
	Failing bool
	Count   int
}

type weekDay struct {
	Day   time.Time
	Today bool
	Items []weekItem
}

type week struct {
	Days                    []weekDay
	Fires, Reminders, Check int
}

const perDay = 3

func (s *site) week(jobs []Job, now time.Time) week {
	today := midnight(now.In(s.loc))
	end := today.AddDate(0, 0, 7)
	w := week{}
	for i := range 7 {
		w.Days = append(w.Days, weekDay{Day: today.AddDate(0, 0, i), Today: i == 0})
	}
	for _, j := range jobs {
		if !j.Enabled || j.NextRun.IsZero() {
			continue
		}
		perDayFires := make([][]time.Time, 7)
		t := j.NextRun
		for range 7 * 24 * 60 {
			if !t.Before(end) {
				break
			}
			if !t.Before(today) {
				perDayFires[dayIndexFrom(today, t.In(s.loc))] = append(perDayFires[dayIndexFrom(today, t.In(s.loc))], t)
			}
			if j.Once() {
				break
			}
			next, err := s.sched.NextRun(j, t)
			if err != nil || !next.After(t) {
				break
			}
			t = next
		}
		for d, fires := range perDayFires {
			if len(fires) == 0 {
				continue
			}
			w.Fires += len(fires)
			if j.Kind == "prompt" {
				w.Check += len(fires)
			} else {
				w.Reminders += len(fires)
			}
			it := weekItem{JobID: j.ID, Name: j.Name, Kind: j.Kind, Once: j.Once(), Snooze: j.CreatedBy == "snooze", Failing: j.LastErr != ""}
			if len(fires) > perDay {
				it.At, it.Count = fires[0], len(fires)
				w.Days[d].Items = append(w.Days[d].Items, it)
				continue
			}
			for _, f := range fires {
				it.At = f
				w.Days[d].Items = append(w.Days[d].Items, it)
			}
		}
	}
	for i := range w.Days {
		slices.SortStableFunc(w.Days[i].Items, func(a, b weekItem) int { return a.At.Compare(b.At) })
	}
	return w
}

// FireTicks is how many fires land on each day, for the this week tile.
func (w week) FireTicks() []hourTick {
	out := make([]hourTick, len(w.Days))
	top := 1
	for i, d := range w.Days {
		for _, it := range d.Items {
			out[i].N += max(it.Count, 1)
		}
		out[i].Label = d.Day.Format("Mon Jan 2")
		top = max(top, out[i].N)
	}
	for i := range out {
		out[i].Pct = 20 + 80*out[i].N/top
	}
	return out
}

// short is a time of day the way the week agenda prints it, 6:00p.
func (s *site) short(t time.Time) string {
	t = t.In(s.loc)
	ap := "a"
	if t.Hour() >= 12 {
		ap = "p"
	}
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	return fmt.Sprintf("%d:%02d%s", h, t.Minute(), ap)
}
