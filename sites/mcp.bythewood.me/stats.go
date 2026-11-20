package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// toolRow is one tool on the tools page, with its last fortnight by day.
type toolRow struct {
	tool
	Calls, Failed int
	P50           int64
	LastUsed      time.Time
	LastErr       string
	LastErrAt     time.Time
	Week          []float64
	WeekFailed    []float64
	WeekLabels    []string
	Days          []dayCell
}

type dayCell struct{ Kind, Tip string }

// Spark is red when anything failed this week, so a failing tool shows in the strip.
func (t toolRow) Spark() string {
	if t.Failed > 0 {
		return "bad"
	}
	if t.Calls == 0 {
		return "flat"
	}
	return "ok"
}

const statDays = 14

// toolRows is every tool in tools.go with its use over the last two weeks,
// in UTC days, failing tools first and never called ones last.
func (s *Store) toolRows(now time.Time) []toolRow {
	today := now.UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -(statDays - 1))
	weekStart := today.AddDate(0, 0, -6)
	type day struct{ calls, failed int }
	days := map[string][]day{}
	ms := map[string][]int64{}
	rows, err := s.db.Query(`SELECT tool, at, ms, ok FROM calls WHERE at >= ? AND tool != ''`, start.UnixMilli())
	if err == nil {
		for rows.Next() {
			var name string
			var at, took int64
			var ok bool
			if rows.Scan(&name, &at, &took, &ok) != nil {
				continue
			}
			i := int(time.UnixMilli(at).UTC().Sub(start) / (24 * time.Hour))
			if i < 0 || i >= statDays {
				continue
			}
			if days[name] == nil {
				days[name] = make([]day, statDays)
			}
			days[name][i].calls++
			if !ok {
				days[name][i].failed++
			}
			if at >= weekStart.UnixMilli() {
				ms[name] = append(ms[name], took)
			}
		}
		rows.Close()
	}
	last := map[string]time.Time{}
	if rows, err := s.db.Query(`SELECT tool, MAX(at) FROM calls WHERE tool != '' GROUP BY tool`); err == nil {
		for rows.Next() {
			var name string
			var at int64
			if rows.Scan(&name, &at) == nil {
				last[name] = time.UnixMilli(at)
			}
		}
		rows.Close()
	}
	type lastErr struct {
		err string
		at  time.Time
	}
	errs := map[string]lastErr{}
	// SQLite fills a bare column from the row MAX picked, so err is the newest one.
	if rows, err := s.db.Query(`SELECT tool, err, MAX(at) FROM calls WHERE tool != '' AND ok = 0 GROUP BY tool`); err == nil {
		for rows.Next() {
			var name, e string
			var at int64
			if rows.Scan(&name, &e, &at) == nil {
				errs[name] = lastErr{e, time.UnixMilli(at)}
			}
		}
		rows.Close()
	}

	out := make([]toolRow, 0, len(tools))
	for _, t := range tools {
		r := toolRow{tool: t, LastUsed: last[t.Name], LastErr: errs[t.Name].err, LastErrAt: errs[t.Name].at}
		d := days[t.Name]
		if d == nil {
			d = make([]day, statDays)
		}
		for i, x := range d {
			when := start.AddDate(0, 0, i)
			c := dayCell{Tip: fmt.Sprintf("%s · %s", when.Format("Jan 2"), count(x.calls, "call", "calls"))}
			switch {
			case x.calls == 0:
				c.Kind = "n"
			case 2*x.failed >= x.calls:
				c.Kind = "b"
			case x.failed > 0:
				c.Kind = "w"
			}
			if x.failed > 0 {
				c.Tip += fmt.Sprintf(", %d failed", x.failed)
			}
			r.Days = append(r.Days, c)
			if i >= statDays-7 {
				r.Calls += x.calls
				r.Failed += x.failed
				r.Week = append(r.Week, float64(x.calls))
				r.WeekFailed = append(r.WeekFailed, float64(x.failed))
				r.WeekLabels = append(r.WeekLabels, when.Format("Mon Jan 2"))
			}
		}
		if m := ms[t.Name]; len(m) > 0 {
			slices.Sort(m)
			r.P50 = m[len(m)/2]
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Failed > 0) != (b.Failed > 0) {
			return a.Failed > 0
		}
		if a.Failed != b.Failed {
			return a.Failed > b.Failed
		}
		if a.LastUsed.IsZero() != b.LastUsed.IsZero() {
			return b.LastUsed.IsZero()
		}
		return a.Calls > b.Calls
	})
	return out
}

// cause folds an error into the kind of thing that went wrong, so seven
// timeouts read as one problem and not seven.
func cause(err string) string {
	e := strings.ToLower(err)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(e, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("timed out", "timeout", "deadline exceeded"):
		return "timeout"
	case has("session revoked", "session is gone"):
		return "signed out"
	case has("didn't answer"):
		return "tool unreachable"
	case has("is required", "isn't an argument", "must be"):
		return "bad argument"
	case has("no such tool", "no tool named", "no such method"):
		return "unknown tool"
	case has("matches more than one"):
		return "ambiguous name"
	case has("has nothing at", "has no readme", "matches"):
		return "not found"
	case has("gate refused"):
		return "refused by gate"
	case has("didn't parse", " answered "):
		return "tool errored"
	}
	return "other"
}

type causeRow struct {
	Cause string
	N     int
	Share float64
	Last  string
}

// Causes is every failed call since a time grouped by cause, the biggest first.
func (s *Store) Causes(since time.Time) []causeRow {
	rows, err := s.db.Query(`SELECT err FROM calls WHERE at >= ? AND ok = 0 ORDER BY id DESC`, since.UnixMilli())
	if err != nil {
		return nil
	}
	defer rows.Close()
	by := map[string]*causeRow{}
	for rows.Next() {
		var e string
		if rows.Scan(&e) != nil {
			continue
		}
		c := cause(e)
		if by[c] == nil {
			by[c] = &causeRow{Cause: c, Last: e}
		}
		by[c].N++
	}
	out := make([]causeRow, 0, len(by))
	top := 0
	for _, c := range by {
		top = max(top, c.N)
	}
	for _, c := range by {
		c.Share = 100 * float64(c.N) / float64(top)
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Cause < out[j].Cause
	})
	return out
}

type argRow struct {
	Arg   string
	N     int
	Share float64
}

// TopArgs is the argument values asked for most since a time, as key=value.
func (s *Store) TopArgs(since time.Time, n int) []argRow {
	rows, err := s.db.Query(`SELECT args FROM calls WHERE at >= ? AND tool != '' AND args != ''`, since.UnixMilli())
	if err != nil {
		return nil
	}
	defer rows.Close()
	by := map[string]int{}
	for rows.Next() {
		var a string
		if rows.Scan(&a) != nil {
			continue
		}
		for _, kv := range argPairs(a) {
			by[kv[0]+"="+kv[1]]++
		}
	}
	out := make([]argRow, 0, len(by))
	top := 0
	for k, v := range by {
		out = append(out, argRow{Arg: k, N: v})
		top = max(top, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Arg < out[j].Arg
	})
	if len(out) > n {
		out = out[:n]
	}
	for i := range out {
		out[i].Share = 100 * float64(out[i].N) / float64(top)
	}
	return out
}
