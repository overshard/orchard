package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	historyDays = 90
	// The error budget's target, over a rolling 30 days.
	sloTarget = 0.995
)

// DayView is one tick of the 90 day bar.
type DayView struct {
	Day    string    `json:"day"`
	Label  string    `json:"label"`
	Kind   string    `json:"kind"`
	Checks int64     `json:"checks"`
	Failed int64     `json:"failed"`
	Uptime float64   `json:"uptime"`
	P50    *int64    `json:"p50"`
	P95    *int64    `json:"p95"`
	Hours  [][3]any  `json:"hours"`
	Tip    string    `json:"-"`
	start  time.Time `json:"-"`
}

// IncidentView is one outage, with the push results as nil for unknown.
type IncidentView struct {
	Start      time.Time  `json:"start"`
	DownAt     time.Time  `json:"down_at"`
	End        *time.Time `json:"end"`
	Code       int64      `json:"code"`
	Cause      string     `json:"cause"`
	Fails      int64      `json:"fails"`
	PushedDown *bool      `json:"pushed_down"`
	PushedUp   *bool      `json:"pushed_up"`
	Backfilled bool       `json:"backfilled"`
	Length     string     `json:"length"`
	dur        time.Duration
}

// PhaseRow is one line of "where the time goes".
type PhaseRow struct {
	Name  string
	Now   int64
	Base  int64
	Delta int64
	Share float64
}

// History is everything older than the last hundred checks that a page shows.
type History struct {
	Days     []DayView
	Since    string
	HasSince bool

	Up24, Up7, Up30, Up90 *float64
	Covered30             int

	Incidents []IncidentView
	Open      *IncidentView
	Inc30     int
	Down30    string
	MTTR      string

	BudgetLeft *float64
	BudgetKind string
	Burn       float64
	BudgetMins string

	// One point per hour over the last 24, oldest first.
	HourLabels []string
	HourUp     []float64
	HourP50    []float64
	HourP95    []float64
	HourTTFB   []float64
	P50, P95   *int64
	TTFB       *int64

	Phases   []PhaseRow
	BaseDays int
}

// Ticks30 is the last thirty days, for the list's day strip.
func (h *History) Ticks30() []DayView {
	if len(h.Days) < 30 {
		return h.Days
	}
	return h.Days[len(h.Days)-30:]
}

// Bad30 counts the red and amber days of the last thirty.
func (h *History) Bad30() int {
	n := 0
	for _, d := range h.Ticks30() {
		if d.Kind == "bad" || d.Kind == "warn" {
			n++
		}
	}
	return n
}

// DaysJSON is the bar's drill-in data, every day the rollup has.
func (h *History) DaysJSON() []DayView {
	out := []DayView{}
	for _, d := range h.Days {
		if d.Kind != "none" {
			out = append(out, d)
		}
	}
	return out
}

// causeText says what a status code meant to the prober, which isn't always
// what the code means on the wire, since 408, 523 and 526 are its own verdicts.
func causeText(code int64) string {
	switch code {
	case 408:
		return "no answer within 10s"
	case 523:
		return "the edge served a cached copy and the origin didn't answer"
	case 526:
		return "the certificate or TLS handshake failed"
	case 530:
		return "the tunnel couldn't reach the origin"
	case 502:
		return "bad gateway"
	case 503:
		return "service unavailable"
	case 504:
		return "gateway timeout"
	case 500:
		return "server error"
	case 404:
		return "not found"
	}
	return fmt.Sprintf("answered %d", code)
}

func shortDur(d time.Duration) string {
	m := int64(math.Round(d.Minutes()))
	switch {
	case m < 1:
		return "<1m"
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m < 60*48:
		return fmt.Sprintf("%dh%02dm", m/60, m%60)
	}
	return fmt.Sprintf("%dd", m/1440)
}

func dayLabel(t time.Time) string { return t.Format("Mon Jan 2") }

// filterBy adds a property filter to a query when one is wanted.
func filterBy(query string, id *uuid.UUID, args ...any) (string, []any) {
	if id == nil {
		return strings.Replace(query, "{and}", "", 1), args
	}
	return strings.Replace(query, "{and}", "AND property_id = ?", 1), append(args, id[:])
}

type dayRow struct {
	day                        string
	checks, failed             int64
	p50, p95, dns, tcp, tls, f *int64
	hours                      string
}

// loadHistories builds a History for every property in props. With one
// property it filters every query to it, the list reads them all at once.
func loadHistories(ctx context.Context, db *sql.DB, props []*Property, now time.Time) (map[uuid.UUID]*History, error) {
	var only *uuid.UUID
	if len(props) == 1 {
		only = &props[0].ID
	}
	today := now.UTC().Truncate(24 * time.Hour)
	first := today.AddDate(0, 0, -(historyDays - 1))

	days := map[uuid.UUID][]dayRow{}
	q, args := filterBy(`SELECT property_id, day, checks, failed, p50_ms, p95_ms, dns_ms, tcp_ms, tls_ms, ttfb_ms, hours
		FROM days WHERE day >= ? {and} ORDER BY day`, only, first.Format(dayLayout))
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw []byte
		var r dayRow
		if err := rows.Scan(&raw, &r.day, &r.checks, &r.failed, &r.p50, &r.p95, &r.dns, &r.tcp, &r.tls, &r.f, &r.hours); err != nil {
			rows.Close()
			return nil, err
		}
		if id, err := uuid.FromBytes(raw); err == nil {
			days[id] = append(days[id], r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var since sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT MIN(day) FROM days").Scan(&since); err != nil {
		return nil, err
	}

	incidents := map[uuid.UUID][]IncidentView{}
	q, args = filterBy(`SELECT property_id, started_at, down_at, ended_at, first_code, failed_checks,
		pushed_down, pushed_up, backfilled FROM incidents
		WHERE (ended_at IS NULL OR ended_at >= ?) {and} ORDER BY started_at DESC`, only, first.UnixMilli())
	rows, err = db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			raw             []byte
			start, down     int64
			end             *int64
			code, fails     int64
			pdown, pup      *int64
			backfilled      int64
			inc             IncidentView
			boolOf          = func(v *int64) *bool { return nilOr(v) }
			incidentEndTime time.Time
		)
		if err := rows.Scan(&raw, &start, &down, &end, &code, &fails, &pdown, &pup, &backfilled); err != nil {
			rows.Close()
			return nil, err
		}
		inc = IncidentView{
			Start: time.UnixMilli(start).UTC(), DownAt: time.UnixMilli(down).UTC(),
			Code: code, Cause: causeText(code), Fails: fails,
			PushedDown: boolOf(pdown), PushedUp: boolOf(pup), Backfilled: backfilled != 0,
		}
		incidentEndTime = now
		if end != nil {
			t := time.UnixMilli(*end).UTC()
			inc.End, incidentEndTime = &t, t
		}
		inc.dur = incidentEndTime.Sub(inc.Start)
		inc.Length = shortDur(inc.dur)
		if id, err := uuid.FromBytes(raw); err == nil {
			incidents[id] = append(incidents[id], inc)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	hourly, last100, err := loadRecent(ctx, db, only, now)
	if err != nil {
		return nil, err
	}

	out := make(map[uuid.UUID]*History, len(props))
	for _, p := range props {
		h := &History{Incidents: incidents[p.ID]}
		if since.Valid {
			if t, err := time.Parse(dayLayout, since.String); err == nil {
				h.Since, h.HasSince = t.Format("Jan 2"), true
			}
		}
		h.fillDays(days[p.ID], first, today, now, time.UnixMilli(p.CreatedAt).UTC())
		h.fillIncidents(now)
		h.fillBudget()
		h.fillHourly(hourly[p.ID], now)
		h.fillPhases(last100[p.ID], days[p.ID])
		out[p.ID] = h
	}
	return out, nil
}

func nilOr(v *int64) *bool {
	if v == nil {
		return nil
	}
	b := *v != 0
	return &b
}

func (h *History) fillDays(rows []dayRow, first, today, now, created time.Time) {
	byDay := map[string]dayRow{}
	for _, r := range rows {
		byDay[r.day] = r
	}
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		key := d.Format(dayLayout)
		v := DayView{Day: key, Label: dayLabel(d), start: d}
		r, ok := byDay[key]
		if !ok || r.checks == 0 {
			v.Kind = "none"
			switch {
			case d.Before(created.Truncate(24 * time.Hour)):
				v.Tip = v.Label + "\nnot watched yet"
			case h.HasSince:
				v.Tip = v.Label + "\nno data, the daily rollup started " + h.Since
			default:
				v.Tip = v.Label + "\nno data, the daily rollup hasn't run yet"
			}
			h.Days = append(h.Days, v)
			continue
		}
		v.Checks, v.Failed, v.P50, v.P95 = r.checks, r.failed, r.p50, r.p95
		v.Uptime = float64(r.checks-r.failed) / float64(r.checks) * 100
		_ = json.Unmarshal([]byte(r.hours), &v.Hours)
		h.Days = append(h.Days, v)
	}

	// Red is an outage over five minutes that touched the day, amber is a day
	// under the target without one.
	for i := range h.Days {
		v := &h.Days[i]
		if v.Kind == "none" {
			continue
		}
		end := v.start.Add(24 * time.Hour)
		var notes []string
		for _, inc := range h.Incidents {
			stop := now
			if inc.End != nil {
				stop = *inc.End
			}
			if inc.Start.Before(end) && stop.After(v.start) && inc.dur > 5*time.Minute {
				v.Kind = "bad"
				notes = append(notes, fmt.Sprintf("%d from %s for %s", inc.Code, inc.Start.Format("15:04"), inc.Length))
			}
		}
		if v.Kind == "" && v.Uptime < sloTarget*100 {
			v.Kind = "warn"
		}
		tip := fmt.Sprintf("%s\n%.2f%% up\n%d of %s failed", v.Label, v.Uptime, v.Failed, formatNum(v.Checks))
		if v.P95 != nil {
			tip += fmt.Sprintf("\np95 %dms", *v.P95)
		}
		if len(notes) > 0 {
			tip += "\n" + strings.Join(notes, "\n")
		}
		v.Tip = tip
	}

	window := func(n int) (*float64, int) {
		var c, f int64
		covered := 0
		for _, d := range h.Days[max(0, len(h.Days)-n):] {
			if d.Kind != "none" {
				c += d.Checks
				f += d.Failed
				covered++
			}
		}
		if c == 0 {
			return nil, 0
		}
		v := float64(c-f) / float64(c) * 100
		return &v, covered
	}
	h.Up7, _ = window(7)
	h.Up30, h.Covered30 = window(30)
	h.Up90, _ = window(90)
}

func (h *History) fillIncidents(now time.Time) {
	var total time.Duration
	var closed int
	var closedSum time.Duration
	cutoff := now.AddDate(0, 0, -30)
	for i := range h.Incidents {
		inc := &h.Incidents[i]
		if inc.End == nil && h.Open == nil {
			h.Open = inc
		}
		if inc.Start.Before(cutoff) {
			continue
		}
		h.Inc30++
		total += inc.dur
		if inc.End != nil {
			closed++
			closedSum += inc.dur
		}
	}
	h.Down30 = shortDur(total)
	if total == 0 {
		h.Down30 = "0m"
	}
	h.MTTR = "none"
	if closed > 0 {
		h.MTTR = shortDur(closedSum / time.Duration(closed))
	}
}

// fillBudget spends 99.5% of thirty days, about 3.6 hours, at one check
// interval per failed check, so a fresh rollup starts with the budget whole.
func (h *History) fillBudget() {
	var c, f int64
	for _, d := range h.Days[max(0, len(h.Days)-30):] {
		c += d.Checks
		f += d.Failed
	}
	if c == 0 {
		return
	}
	allowed := 30 * 24 * time.Hour.Minutes() * (1 - sloTarget)
	spent := float64(f) * checkInterval.Minutes()
	left := math.Max(0, 1-spent/allowed) * 100
	h.BudgetLeft = &left
	h.BudgetMins = shortDur(time.Duration(allowed * float64(time.Minute)))
	switch {
	case left <= 0:
		h.BudgetKind = "bad"
	case left < 50:
		h.BudgetKind = "warn"
	default:
		h.BudgetKind = "ok"
	}
}

type recentRow struct {
	code, ms, at        int64
	dns, tcp, tls, ttfb *int64
}

// loadRecent reads the last 24 hours of checks, which covers both the hourly
// series and the last hundred checks a property has.
func loadRecent(ctx context.Context, db *sql.DB, only *uuid.UUID, now time.Time) (map[uuid.UUID][]recentRow, map[uuid.UUID][]recentRow, error) {
	q, args := filterBy(`SELECT property_id, status_code, response_ms, dns_ms, tcp_ms, tls_ms, ttfb_ms, created_at
		FROM checks WHERE created_at >= ? {and} ORDER BY created_at DESC`, only, now.Add(-24*time.Hour).UnixMilli())
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	day := map[uuid.UUID][]recentRow{}
	for rows.Next() {
		var raw []byte
		var r recentRow
		if err := rows.Scan(&raw, &r.code, &r.ms, &r.dns, &r.tcp, &r.tls, &r.ttfb, &r.at); err != nil {
			return nil, nil, err
		}
		if id, err := uuid.FromBytes(raw); err == nil {
			day[id] = append(day[id], r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	last := map[uuid.UUID][]recentRow{}
	for id, rs := range day {
		last[id] = rs[:min(len(rs), 100)]
	}
	return day, last, nil
}

func (h *History) fillHourly(rows []recentRow, now time.Time) {
	if len(rows) == 0 {
		return
	}
	top := now.UTC().Truncate(time.Hour)
	start := top.Add(-23 * time.Hour)
	type bucket struct {
		n, ok int64
		ms    []int64
		ttfb  []int64
	}
	var b [24]bucket
	var all, allTTFB []int64
	var n, up int64
	for _, r := range rows {
		i := int(time.UnixMilli(r.at).UTC().Sub(start) / time.Hour)
		n++
		if i >= 0 && i < 24 {
			b[i].n++
		}
		if r.code != 200 {
			continue
		}
		up++
		all = append(all, r.ms)
		if r.ttfb != nil {
			allTTFB = append(allTTFB, *r.ttfb)
		}
		if i >= 0 && i < 24 {
			b[i].ok++
			b[i].ms = append(b[i].ms, r.ms)
			if r.ttfb != nil {
				b[i].ttfb = append(b[i].ttfb, *r.ttfb)
			}
		}
	}
	pct := float64(up) / float64(n) * 100
	h.Up24 = &pct
	h.P50, h.P95, h.TTFB = quantile(all, 0.5), quantile(all, 0.95), quantile(allTTFB, 0.5)

	// An hour with no checks repeats the one before, so a gap reads flat.
	var lastUp, last50, last95, lastT float64 = 100, 0, 0, 0
	for i := range b {
		if b[i].n > 0 {
			lastUp = float64(b[i].ok) / float64(b[i].n) * 100
		}
		if v := quantile(b[i].ms, 0.5); v != nil {
			last50 = float64(*v)
		}
		if v := quantile(b[i].ms, 0.95); v != nil {
			last95 = float64(*v)
		}
		if v := quantile(b[i].ttfb, 0.5); v != nil {
			lastT = float64(*v)
		}
		h.HourLabels = append(h.HourLabels, start.Add(time.Duration(i)*time.Hour).Format("15:04"))
		h.HourUp = append(h.HourUp, lastUp)
		h.HourP50 = append(h.HourP50, last50)
		h.HourP95 = append(h.HourP95, last95)
		h.HourTTFB = append(h.HourTTFB, lastT)
	}
}

// fillPhases puts the median of each phase over the last hundred checks beside
// the median of the daily medians, which is as close to a week's median as the
// rollup can get.
func (h *History) fillPhases(rows []recentRow, days []dayRow) {
	base := days[max(0, len(days)-7):]
	h.BaseDays = len(base)
	pick := func(r recentRow, i int) *int64 { return [...]*int64{r.ttfb, r.tls, r.tcp, r.dns}[i] }
	pickDay := func(d dayRow, i int) *int64 { return [...]*int64{d.f, d.tls, d.tcp, d.dns}[i] }
	var total int64
	for i, name := range []string{"first byte", "tls", "tcp", "dns"} {
		var now, then []int64
		for _, r := range rows {
			if v := pick(r, i); r.code == 200 && v != nil {
				now = append(now, *v)
			}
		}
		for _, d := range base {
			if v := pickDay(d, i); v != nil {
				then = append(then, *v)
			}
		}
		n, b := quantile(now, 0.5), quantile(then, 0.5)
		if n == nil {
			continue
		}
		row := PhaseRow{Name: name, Now: *n}
		if b != nil {
			row.Base, row.Delta = *b, *n-*b
		}
		total += *n
		h.Phases = append(h.Phases, row)
	}
	for i := range h.Phases {
		if total > 0 {
			h.Phases[i].Share = float64(h.Phases[i].Now) / float64(total) * 100
		}
	}
	slices.SortStableFunc(h.Phases, func(a, b PhaseRow) int { return int(b.Now - a.Now) })
}

// Tick is one bar of an o-ticks row.
type Tick struct {
	Kind   string
	Height int
	Tip    string
}

// BudgetTicks is the last thirty days by failed checks, the days that spent
// the budget standing tallest.
func (h *History) BudgetTicks() []Tick {
	days := h.Ticks30()
	var top int64 = 1
	for _, d := range days {
		top = max(top, d.Failed)
	}
	out := make([]Tick, 0, len(days))
	for _, d := range days {
		t := Tick{Height: 6, Tip: fmt.Sprintf("%s · %d failed", d.Label, d.Failed)}
		switch {
		case d.Kind == "none":
			t.Kind, t.Tip = "none", d.Label+" · no data"
		case d.Failed > 0:
			t.Kind, t.Height = "warn", max(12, int(d.Failed*100/top))
		}
		out = append(out, t)
	}
	return out
}

// Burn is the last 24 hours' failure rate against what the target allows, so
// 1× spends the budget exactly over the window.
func (h *History) Burn24() string {
	if h.Up24 == nil {
		return ""
	}
	return fmt.Sprintf("%.1f× burn", (100-*h.Up24)/100/(1-sloTarget))
}

// TTFBNote is the first byte median against the rollup's, for the tile.
func (h *History) TTFBNote() string {
	for _, p := range h.Phases {
		if p.Name == "first byte" && h.BaseDays > 0 {
			return fmt.Sprintf("%+d on %dd median", p.Delta, h.BaseDays)
		}
	}
	return "median of 24h"
}

// Drill is what the bar's click-through reads.
func (h *History) Drill() map[string]any {
	inc := h.Incidents
	if inc == nil {
		inc = []IncidentView{}
	}
	return map[string]any{"days": h.DaysJSON(), "incidents": inc}
}

// PhaseWidth scales a phase against the largest, for its bar.
func (h *History) PhaseWidth(p PhaseRow) float64 {
	if len(h.Phases) == 0 || h.Phases[0].Now == 0 {
		return 0
	}
	return float64(p.Now) / float64(h.Phases[0].Now) * 100
}
