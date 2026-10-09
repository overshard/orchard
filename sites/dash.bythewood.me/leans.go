package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// leanCall is one forward line's lean, kept until the session it was about has
// closed so it can be checked against what the S&P 500 actually did. The page
// only ever holds the latest brief, so without this a lean is gone by the next
// slot and nobody can say how often they were right.
type leanCall struct {
	Day   string `json:"day"`
	Slot  int64  `json:"slot"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Lean  string `json:"lean"`
	Text  string `json:"text"`

	// Filled once the day has closed, up, down or flat, or "closed" for a
	// holiday the calendar didn't know about.
	Dir  string `json:"dir,omitempty"`
	Move string `json:"move,omitempty"`
}

// A year of three slots a day with two forward lines each, with room over.
const leanKeep = 2500

type leanBook struct {
	path  string
	calls []leanCall
}

func openLeanBook(dataDir string) *leanBook {
	lb := &leanBook{path: filepath.Join(dataDir, "leans.json")}
	if raw, err := os.ReadFile(lb.path); err == nil {
		if err := json.Unmarshal(raw, &lb.calls); err != nil {
			slog.Warn("leans file unreadable", slog.String("component", "brief"), slog.Any("err", err))
		}
	}
	return lb
}

// leanDay is the session a forward line is about. Only TODAY and the next
// weekday ever carry a lean, so the label is enough to tell which.
func leanDay(label string, slot time.Time) string {
	slot = slot.In(easternTime())
	if label == "TODAY" {
		return slot.Format("2006-01-02")
	}
	return nextWeekday(slot).Format("2006-01-02")
}

// add records the forward leans in a markets brief, once per slot and label, so
// reading the saved brief back on a restart doesn't count it twice.
func (lb *leanBook) add(b Brief) bool {
	// A zero slot is a brief cleared to force a rerun, and has no day.
	if b.Slot == 0 {
		return false
	}
	added := false
	for _, p := range b.Points {
		if p.Lean != "higher" && p.Lean != "lower" && p.Lean != "mixed" {
			continue
		}
		dup := false
		for _, c := range lb.calls {
			if c.Slot == b.Slot && c.Label == p.Label {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		lb.calls = append(lb.calls, leanCall{
			Day: leanDay(p.Label, time.Unix(b.Slot, 0)), Slot: b.Slot, Kind: b.Kind,
			Label: p.Label, Lean: p.Lean, Text: p.Text,
		})
		added = true
	}
	if n := len(lb.calls); n > leanKeep {
		lb.calls = lb.calls[n-leanKeep:]
	}
	return added
}

// score checks every lean whose day closed before today against the daily
// closes and logs each one, with the running record, so logging can answer how
// often the leans were right. Today's close isn't trusted until tomorrow, since
// Yahoo's daily bar can still move a little just after the bell.
func (lb *leanBook) score(h *history, now time.Time) bool {
	if h == nil {
		return false
	}
	bars := dailyBars(h, easternTime())
	if len(bars) < 2 {
		return false
	}
	today := now.In(easternTime()).Format("2006-01-02")
	idx := map[string]int{}
	for i, b := range bars {
		idx[b.date] = i
	}

	changed := false
	for i := range lb.calls {
		c := &lb.calls[i]
		if c.Dir != "" || c.Day >= today || c.Day > bars[len(bars)-1].date {
			continue
		}
		j, ok := idx[c.Day]
		switch {
		case !ok:
			c.Dir = "closed"
		case j == 0:
			continue
		default:
			l := closedLine("", bars[j].close, bars[j-1].close, "")
			c.Dir, c.Move = l.dir, l.move
		}
		changed = true
		hits, calls := lb.record()
		slog.Info("brief lean scored", slog.String("component", "brief"),
			slog.String("day", c.Day), slog.String("kind", c.Kind), slog.String("label", c.Label),
			slog.String("lean", c.Lean), slog.String("dir", c.Dir), slog.String("move", c.Move),
			slog.Bool("hit", leanHit(c.Lean, c.Dir)), slog.Int("hits", hits), slog.Int("calls", calls))
	}
	return changed
}

// leanHit is higher on an up day, lower on a down day, and mixed on a flat one.
func leanHit(lean, dir string) bool {
	return lean == "higher" && dir == "up" || lean == "lower" && dir == "down" || lean == "mixed" && dir == "flat"
}

// record is how many scored leans were right, out of how many were scored.
func (lb *leanBook) record() (hits, calls int) {
	for _, c := range lb.calls {
		if c.Dir == "" || c.Dir == "closed" {
			continue
		}
		calls++
		if leanHit(c.Lean, c.Dir) {
			hits++
		}
	}
	return hits, calls
}

func (lb *leanBook) save() {
	raw, err := json.MarshalIndent(lb.calls, "", " ")
	if err != nil {
		return
	}
	tmp := lb.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err == nil {
		err = os.Rename(tmp, lb.path)
	}
	if err != nil {
		slog.Warn("leans not saved", slog.String("component", "brief"), slog.Any("err", err))
	}
}
