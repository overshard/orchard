package main

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// Solves is challenges over a window, people being addresses counted once an
// hour each, and answers being posts to /__gate/solve.
type Solves struct {
	People, Solved, Failed   int
	PeopleByHour, RateByHour Series
	ByRule                   map[int64][3]int
}

// Rate is solved answers over people challenged, as a percentage.
func (x Solves) Rate() float64 {
	if x.People == 0 {
		return 0
	}
	return min(100, 100*float64(x.Solved)/float64(x.People))
}

func (s *Store) Solves(window time.Duration, now time.Time) Solves {
	n := hours(window)
	x := Solves{PeopleByHour: make(Series, n), RateByHour: make(Series, n), ByRule: map[int64][3]int{}}
	solved := make(Series, n)
	rows, err := s.db.Query(`SELECT hour, rule, SUM(people), SUM(solved), SUM(failed) FROM solves WHERE hour > ? GROUP BY hour, rule`, hourOf(now)-int64(n)*3600)
	if err != nil {
		return x
	}
	defer rows.Close()
	for rows.Next() {
		var hour, rule int64
		var p, ok, bad int
		if rows.Scan(&hour, &rule, &p, &ok, &bad) != nil {
			continue
		}
		i := slot(hour, now, n)
		if i < 0 || i >= n {
			continue
		}
		x.People, x.Solved, x.Failed = x.People+p, x.Solved+ok, x.Failed+bad
		x.PeopleByHour[i] += float64(p)
		solved[i] += float64(ok)
		r := x.ByRule[rule]
		x.ByRule[rule] = [3]int{r[0] + p, r[1] + ok, r[2] + bad}
	}
	for i := range x.RateByHour {
		if x.PeopleByHour[i] > 0 {
			x.RateByHour[i] = min(100, 100*solved[i]/x.PeopleByHour[i])
		}
	}
	return x
}

// NetworksByHour is how many networks had anything refused, an hour at a time,
// and across the whole window.
func (s *Store) NetworksByHour(window time.Duration, now time.Time) (Series, int) {
	n := hours(window)
	out := make(Series, n)
	since := hourOf(now) - int64(n)*3600
	rows, err := s.db.Query(`SELECT hour, COUNT(DISTINCT asn) FROM traffic WHERE hour > ? AND verdict IN ('block','banned','limit') GROUP BY hour`, since)
	if err == nil {
		for rows.Next() {
			var hour int64
			var c int
			if rows.Scan(&hour, &c) == nil {
				if i := slot(hour, now, n); i >= 0 && i < n {
					out[i] = float64(c)
				}
			}
		}
		rows.Close()
	}
	total := 0
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT asn) FROM traffic WHERE hour > ? AND verdict IN ('block','banned','limit')`, since).Scan(&total)
	return out, total
}

// hourLabels names each slot of a series ending now, for the crosshair.
func hourLabels(n int, now time.Time) []string {
	out := make([]string, n)
	top := now.UTC().Truncate(time.Hour)
	for i := range out {
		out[i] = top.Add(-time.Duration(n-1-i) * time.Hour).Format("Jan 2 15:00")
	}
	return out
}

// Share is a group's refused requests as a share of its own.
func (g Group) RefusedPct() string {
	if g.Total == 0 {
		return "0%"
	}
	p := 100 * float64(g.Refused) / float64(g.Total)
	if p < 10 && p > 0 {
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.0f%%", p)
}

// sumSeries adds series of the same length.
func sumSeries(n int, all ...Series) Series {
	out := make(Series, n)
	for _, s := range all {
		for i := range min(n, len(s)) {
			out[i] += s[i]
		}
	}
	return out
}

// ruleRows is every rule with its use over a window, in the order check runs
// them, with the shadowed ones marked and each challenge's solve rate.
func (s *site) ruleRows(window time.Duration, now time.Time) []RuleUse {
	rules, _ := s.store.Rules()
	uses := s.store.RuleUses(rules, window, now)
	order := map[int64]int{}
	for i, r := range *s.engine.rules.Load() {
		order[r.ID] = i + 1
	}
	shadows := s.shadows()
	solves := s.store.Solves(window, now)
	for i := range uses {
		u := &uses[i]
		u.Order = order[u.ID]
		if sh, ok := shadows[u.ID]; ok {
			u.ShadowBy = sh.By
		}
		x := solves.ByRule[u.ID]
		u.People, u.Solved = x[0], x[1]
	}
	slices.SortStableFunc(uses, func(a, b RuleUse) int {
		if (a.Order == 0) != (b.Order == 0) {
			if a.Order == 0 {
				return 1
			}
			return -1
		}
		return a.Order - b.Order
	})
	return uses
}

// WhyLine is one line of the rule that refused an event and whether it held
// for that request, worked out again against the rule as it is now.
type WhyLine struct {
	N           int
	Text, Piece string
	State       string // holds, fails or unknown
	Decided     bool   // the line the event recorded as the one that caught it
}

// EventRow is an event with what its drawer shows.
type EventRow struct {
	Event
	Lines   []WhyLine
	AlsoL   []string
	Refused int      // refused from this address over the last 24h, sampled
	Ticks   []string // the address's last 24 hours, a class an hour
	Build   []string // conditions a rule from this request could start with
}

// Summary is the line that caught it in a few words, like path regex wp-.
func (e EventRow) Summary() string {
	for _, l := range e.Lines {
		if l.Decided {
			if l.Piece != "" {
				return fmt.Sprintf("line %d · %s", l.N, l.Piece)
			}
			return fmt.Sprintf("line %d", l.N)
		}
	}
	return ""
}

func (s *site) eventRows(events []Event, now time.Time) []EventRow {
	rules, _ := s.store.Rules()
	byName := map[string]Rule{}
	for _, r := range rules {
		if r.validate() == nil {
			byName[r.Name] = r
		}
	}
	hist := s.store.ipHours(events, now)
	out := make([]EventRow, len(events))
	for i, e := range events {
		row := EventRow{Event: e, Ticks: make([]string, 24)}
		for j := range row.Ticks {
			row.Ticks[j] = "none"
		}
		for _, h := range hist[e.IP] {
			if j := slot(h.hour, now, 24); j >= 0 && j < 24 {
				row.Ticks[j] = h.kind
				row.Refused += h.refused
			}
		}
		if e.Also != "" {
			row.AlsoL = strings.Split(e.Also, ",")
		}
		if r, ok := byName[e.Rule]; ok {
			q := e.req()
			for _, c := range r.conds {
				l := WhyLine{N: c.line, Text: lineAt(r.When, c.line), Decided: c.line == e.Line}
				v, known := c.value(q)
				switch {
				case !known:
					l.State = "unknown"
				case c.test(q, v) != c.not:
					l.State = "holds"
					if !c.not {
						l.Piece = c.piece(q, v)
					}
				default:
					l.State = "fails"
				}
				if l.Decided && e.Matched != "" {
					l.Piece = e.Matched
				}
				row.Lines = append(row.Lines, l)
			}
		}
		row.Build = buildFrom(e)
		out[i] = row
	}
	return out
}

func lineAt(when string, n int) string {
	ls := strings.Split(when, "\n")
	if n < 1 || n > len(ls) {
		return ""
	}
	return strings.TrimSpace(ls[n-1])
}

// req is what an event still knows of its request. The query, headers and
// crawler check aren't kept, so lines on those read as unknown.
func (e Event) req() *req {
	ip, _ := netip.ParseAddr(e.IP)
	return &req{IP: ip, ASN: e.ASN, Country: e.Country, Host: e.Host, Method: e.Method, Path: e.Path, UA: e.UA}
}

// buildFrom is the lines a new rule can start from, host and path first.
func buildFrom(e Event) []string {
	out := []string{"host is " + e.Host, "path is " + e.Path}
	if seg, _, _ := strings.Cut(strings.TrimPrefix(e.Path, "/"), "/"); seg != "" && "/"+seg != e.Path {
		out = append(out, "path prefix /"+seg+"/")
	}
	if t := uaToken(e.UA); t != "" {
		out = append(out, "ua contains "+t)
	}
	if e.ASN != 0 {
		out = append(out, "asn is "+itoa(e.ASN))
	}
	if e.Country != "" {
		out = append(out, "country is "+e.Country)
	}
	if e.IP != "" {
		out = append(out, "ip is "+e.IP)
	}
	return out
}

// uaToken is the first word of a user agent that names something, skipping
// the Mozilla and compatible every browser sends.
func uaToken(ua string) string {
	for _, w := range strings.FieldsFunc(strings.ToLower(ua), func(r rune) bool { return strings.ContainsRune(" /;()+,", r) }) {
		if len(w) >= 4 && w != "mozilla" && w != "compatible" && (w[0] < '0' || w[0] > '9') {
			return w
		}
	}
	return ""
}

type ipHour struct {
	hour    int64
	kind    string
	refused int
}

// ipHours is each event address's last day by the hour from the sampled
// events, bad for an hour with a refusal and warn for one only watched.
func (s *Store) ipHours(events []Event, now time.Time) map[string][]ipHour {
	out := map[string][]ipHour{}
	var ips []any
	seen := map[string]bool{}
	for _, e := range events {
		if e.IP != "" && !seen[e.IP] {
			seen[e.IP] = true
			ips = append(ips, e.IP)
		}
	}
	if len(ips) == 0 {
		return out
	}
	q := `SELECT ip, at / 3600000 * 3600, COUNT(*), SUM(CASE WHEN watch = 0 AND verdict IN ('block','limit','banned') THEN 1 ELSE 0 END)
		FROM events WHERE at > ? AND ip IN (?` + strings.Repeat(",?", len(ips)-1) + `) GROUP BY 1, 2`
	rows, err := s.db.Query(q, append([]any{now.Add(-24 * time.Hour).UnixMilli()}, ips...)...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var ip string
		var h ipHour
		var n int
		if rows.Scan(&ip, &h.hour, &n, &h.refused) != nil {
			continue
		}
		h.kind = "warn"
		if h.refused > 0 {
			h.kind = "bad"
		}
		out[ip] = append(out[ip], h)
	}
	return out
}
