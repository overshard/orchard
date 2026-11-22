package main

import (
	"fmt"
	"strings"
	"time"
)

// Series is a count an hour, oldest first, over a window ending now.
type Series []float64

func (s Series) Sum() int {
	n := 0.0
	for _, v := range s {
		n += v
	}
	return int(n)
}

func hours(window time.Duration) int { return max(1, int(window/time.Hour)) }

// slot is where an hour lands in a series of n ending at now.
func slot(hour int64, now time.Time, n int) int {
	return n - 1 - int((hourOf(now)-hour)/3600)
}

// Day is the traffic counts over a window, by verdict and by the hour. From
// says where a narrowed one was counted: "" for every request, "rule" for one
// rule's own hits and "events" for the sampled requests.
type Day struct {
	Checked, Passed, Exempt, Blocked, Limited, Challenged, Solved, Banned int
	Watched                                                               int
	ByHour, BlockedByHour, ChallengedByHour                               Series
	BannedByHour                                                          Series
	From                                                                  string
}

// ruleDay is one rule's matches over a window, from its hourly hits.
func (s *Store) ruleDay(r Rule, window time.Duration, now time.Time) Day {
	n := hours(window)
	d := Day{ByHour: make(Series, n), BlockedByHour: make(Series, n), ChallengedByHour: make(Series, n), From: "rule"}
	rows, err := s.db.Query(`SELECT hour, hits, watched FROM hits WHERE rule = ? AND hour > ?`, r.ID, hourOf(now)-int64(n)*3600)
	if err != nil {
		return d
	}
	defer rows.Close()
	for rows.Next() {
		var hour int64
		var h, w int
		if rows.Scan(&hour, &h, &w) != nil {
			continue
		}
		i := slot(hour, now, n)
		if i < 0 || i >= n {
			continue
		}
		d.Checked += h + w
		d.Watched += w
		d.ByHour[i] += float64(h + w)
		switch r.Action {
		case actAllow:
			d.Passed += h
		case actBlock:
			d.Blocked += h
			d.BlockedByHour[i] += float64(h)
		case actLimit:
			d.Limited += h
			d.BlockedByHour[i] += float64(h)
		case actChallenge:
			d.Challenged += h
			d.ChallengedByHour[i] += float64(h)
		}
	}
	return d
}

// eventDay counts the sampled requests that match a filter, which is all
// there is for one address.
func (s *Store) eventDay(f TrafficFilter, now time.Time) Day {
	n := hours(f.Window)
	d := Day{ByHour: make(Series, n), BlockedByHour: make(Series, n), ChallengedByHour: make(Series, n), From: "events"}
	w, args := f.eventWhere(now)
	rows, err := s.db.Query(`SELECT at / 3600000 * 3600, verdict, watch, COUNT(*) FROM events WHERE `+strings.Join(w, " AND ")+
		` GROUP BY 1, verdict, watch`, args...)
	if err != nil {
		return d
	}
	defer rows.Close()
	for rows.Next() {
		var hour int64
		var v string
		var watch, c int
		if rows.Scan(&hour, &v, &watch, &c) != nil {
			continue
		}
		i := slot(hour, now, n)
		if i < 0 || i >= n {
			continue
		}
		d.Checked += c
		d.ByHour[i] += float64(c)
		if watch == 1 {
			d.Watched += c
			continue
		}
		switch v {
		case vBlock:
			d.Blocked += c
			d.BlockedByHour[i] += float64(c)
		case vBanned:
			d.Banned += c
			d.BlockedByHour[i] += float64(c)
		case vLimit:
			d.Limited += c
			d.BlockedByHour[i] += float64(c)
		case vChallenge:
			d.Challenged += c
			d.ChallengedByHour[i] += float64(c)
		}
	}
	return d
}

// Counted is the word for what Checked holds.
func (d Day) Counted() string {
	switch d.From {
	case "rule":
		return "matched"
	case "events":
		return "sampled"
	}
	return "checked"
}

func (s *Store) Day(window time.Duration, now time.Time) Day {
	n := hours(window)
	d := Day{ByHour: make(Series, n), BlockedByHour: make(Series, n), ChallengedByHour: make(Series, n), BannedByHour: make(Series, n)}
	rows, err := s.db.Query(`SELECT hour, verdict, SUM(n) FROM traffic WHERE hour > ? GROUP BY hour, verdict`, hourOf(now)-int64(n)*3600)
	if err != nil {
		return d
	}
	defer rows.Close()
	for rows.Next() {
		var hour int64
		var v string
		var c int
		if rows.Scan(&hour, &v, &c) != nil {
			continue
		}
		i := slot(hour, now, n)
		if i < 0 || i >= n {
			continue
		}
		d.Checked += c
		d.ByHour[i] += float64(c)
		switch v {
		case vPass:
			d.Passed += c
		case vExempt:
			d.Exempt += c
		case vSolved:
			d.Solved += c
		case vBlock:
			d.Blocked += c
			d.BlockedByHour[i] += float64(c)
		case vBanned:
			d.Banned += c
			d.BlockedByHour[i] += float64(c)
			d.BannedByHour[i] += float64(c)
		case vLimit:
			d.Limited += c
			d.BlockedByHour[i] += float64(c)
		case vChallenge:
			d.Challenged += c
			d.ChallengedByHour[i] += float64(c)
		}
	}
	return d
}

// Refused is everything gate answered for the site.
func (d Day) Refused() int { return d.Blocked + d.Banned + d.Limited }

// RuleUse is one rule with what it did over a window.
type RuleUse struct {
	Rule
	Hits, Watched  int
	Matched        int // enforcing matches, Hits plus the ones a rule before it took
	ByHour         Series
	WatchedByHour  Series
	Last           time.Time
	Order          int // where it runs, from 1, 0 for a rule that can't run
	ShadowBy       string
	People, Solved int
}

// Taken is the share of a rule's matches a rule before it decided, as a
// whole percent, or -1 when there's nothing to say. Hours from before matches
// were counted have none, so Matched can trail Hits.
func (u RuleUse) Taken() int {
	if u.Matched == 0 || u.Matched <= u.Hits || u.Action == actAllow {
		return -1
	}
	return 100 * (u.Matched - u.Hits) / u.Matched
}

// SolveRate is the challenge rules' solved answers over people challenged.
func (u RuleUse) SolveRate() string {
	if u.Action != actChallenge || u.People == 0 {
		return "–"
	}
	return fmt.Sprintf("%.1f%%", min(100, 100*float64(u.Solved)/float64(u.People)))
}

func (s *Store) RuleUses(rules []Rule, window time.Duration, now time.Time) []RuleUse {
	n := hours(window)
	by := map[int64]*RuleUse{}
	out := make([]RuleUse, len(rules))
	for i, r := range rules {
		out[i] = RuleUse{Rule: r, ByHour: make(Series, n), WatchedByHour: make(Series, n)}
		by[r.ID] = &out[i]
	}
	rows, err := s.db.Query(`SELECT rule, hour, hits, watched, matched FROM hits WHERE hour > ?`, hourOf(now)-int64(n)*3600)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, hour int64
		var h, w, m int
		if rows.Scan(&id, &hour, &h, &w, &m) != nil {
			continue
		}
		u := by[id]
		i := slot(hour, now, n)
		if u == nil || i < 0 || i >= n {
			continue
		}
		u.Hits += h
		u.Watched += w
		u.Matched += m
		u.ByHour[i] += float64(h + w)
		u.WatchedByHour[i] += float64(w)
		if t := time.Unix(hour, 0); t.After(u.Last) && h+w > 0 {
			u.Last = t
		}
	}
	return out
}

// Group is one network, country or host and what gate did with its requests.
type Group struct {
	Key, Label             string
	Total, Refused, Passed int
	Challenged             int
	Share                  float64
}

// TrafficFilter narrows the traffic page and the agent's reads to one network,
// country, host or verdict.
type TrafficFilter struct {
	ASN     int
	Country string
	Host    string
	IP      string
	Rule    string
	Verdict string
	Window  time.Duration
}

func (f TrafficFilter) where(now time.Time) (string, []any) {
	w := []string{"hour > ?"}
	args := []any{hourOf(now) - int64(hours(f.Window))*3600}
	if f.ASN != 0 {
		w, args = append(w, "asn = ?"), append(args, f.ASN)
	}
	if f.Country != "" {
		w, args = append(w, "country = ?"), append(args, f.Country)
	}
	if f.Host != "" {
		w, args = append(w, "host = ?"), append(args, f.Host)
	}
	if f.Verdict != "" {
		w, args = append(w, "verdict = ?"), append(args, f.Verdict)
	}
	return strings.Join(w, " AND "), args
}

// Groups ranks one column of the traffic table, the most refused first when
// by is "refused", else the busiest.
func (s *Store) Groups(col string, f TrafficFilter, by string, limit int, now time.Time) []Group {
	switch col {
	case "asn", "country", "host":
	default:
		return nil
	}
	orgs := map[string]string{}
	if col == "asn" {
		orgs = s.Orgs()
	}
	where, args := f.where(now)
	order := "total"
	if by == "refused" {
		order = "refused"
	}
	q := `SELECT ` + col + `, SUM(n) AS total,
		SUM(CASE WHEN verdict IN ('block','banned','limit') THEN n ELSE 0 END) AS refused,
		SUM(CASE WHEN verdict IN ('pass','solved') THEN n ELSE 0 END),
		SUM(CASE WHEN verdict = 'challenge' THEN n ELSE 0 END)
		FROM traffic WHERE ` + where + ` GROUP BY ` + col + ` HAVING ` + order + ` > 0 ORDER BY ` + order + ` DESC LIMIT ?`
	rows, err := s.db.Query(q, append(args, limit)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Group
	top := 0
	for rows.Next() {
		var g Group
		var key any
		if rows.Scan(&key, &g.Total, &g.Refused, &g.Passed, &g.Challenged) != nil {
			continue
		}
		switch k := key.(type) {
		case int64:
			if k == 0 {
				g.Key = "0"
			} else {
				g.Key = itoa(int(k))
			}
		case string:
			g.Key = k
		}
		g.Label = g.Key
		v := g.Total
		if by == "refused" {
			v = g.Refused
		}
		top = max(top, v)
		out = append(out, g)
	}
	if col == "asn" {
		for i := range out {
			if out[i].Key == "0" {
				out[i].Label = "unknown network"
			} else if o := orgs[out[i].Key]; o != "" {
				out[i].Label = "AS" + out[i].Key + " " + o
			} else {
				out[i].Label = "AS" + out[i].Key
			}
		}
	}
	if col == "country" {
		for i := range out {
			if out[i].Key == "" {
				out[i].Label = "unknown"
			}
		}
	}
	for i := range out {
		v := out[i].Total
		if by == "refused" {
			v = out[i].Refused
		}
		if top > 0 {
			out[i].Share = 100 * float64(v) / float64(top)
		}
	}
	return out
}

func (s *Store) Orgs() map[string]string {
	out := map[string]string{}
	rows, err := s.db.Query(`SELECT asn, org FROM orgs`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a int
		var o string
		if rows.Scan(&a, &o) == nil {
			out[itoa(a)] = o
		}
	}
	return out
}

func (f TrafficFilter) eventWhere(now time.Time) ([]string, []any) {
	w := []string{"at > ?"}
	args := []any{now.Add(-f.Window).UnixMilli()}
	if f.ASN != 0 {
		w, args = append(w, "asn = ?"), append(args, f.ASN)
	}
	if f.Country != "" {
		w, args = append(w, "country = ?"), append(args, f.Country)
	}
	if f.Host != "" {
		w, args = append(w, "host = ?"), append(args, f.Host)
	}
	if f.IP != "" {
		w, args = append(w, "ip = ?"), append(args, f.IP)
	}
	if f.Rule != "" {
		w, args = append(w, "rule = ?"), append(args, f.Rule)
	}
	if f.Verdict != "" {
		w, args = append(w, "verdict = ?"), append(args, f.Verdict)
	}
	return w, args
}

func (s *Store) Events(f TrafficFilter, watch string, limit int, now time.Time) []Event {
	w, args := f.eventWhere(now)
	switch watch {
	case "yes":
		w = append(w, "watch = 1")
	case "no":
		w = append(w, "watch = 0")
	}
	orgs := s.Orgs()
	rows, err := s.db.Query(`SELECT at, ip, asn, country, host, method, path, ua, rule, verdict, watch, line, matched, also FROM events WHERE `+
		strings.Join(w, " AND ")+` ORDER BY at DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var at int64
		var watched int
		if rows.Scan(&at, &e.IP, &e.ASN, &e.Country, &e.Host, &e.Method, &e.Path, &e.UA, &e.Rule, &e.Verdict, &watched, &e.Line, &e.Matched, &e.Also) != nil {
			continue
		}
		e.At, e.Watch = time.UnixMilli(at).UTC(), watched == 1
		e.Org = orgs[itoa(e.ASN)]
		out = append(out, e)
	}
	return out
}

// TopPaths is the paths a filter's refused or watched requests asked for most,
// from the sampled events.
func (s *Store) TopPaths(f TrafficFilter, limit int, now time.Time) []Group {
	w := []string{"at > ?"}
	args := []any{now.Add(-f.Window).UnixMilli()}
	if f.ASN != 0 {
		w, args = append(w, "asn = ?"), append(args, f.ASN)
	}
	if f.Host != "" {
		w, args = append(w, "host = ?"), append(args, f.Host)
	}
	if f.Rule != "" {
		w, args = append(w, "rule = ?"), append(args, f.Rule)
	}
	if f.IP != "" {
		w, args = append(w, "ip = ?"), append(args, f.IP)
	}
	if f.Verdict == "refused" {
		w = append(w, "watch = 0 AND verdict IN ('block','limit','banned')")
	}
	rows, err := s.db.Query(`SELECT host || path, COUNT(*) AS c FROM events WHERE `+strings.Join(w, " AND ")+
		` GROUP BY host, path ORDER BY c DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Group
	top := 0
	for rows.Next() {
		var g Group
		if rows.Scan(&g.Key, &g.Total) == nil {
			g.Label = g.Key
			top = max(top, g.Total)
			out = append(out, g)
		}
	}
	for i := range out {
		out[i].Share = 100 * float64(out[i].Total) / float64(max(1, top))
	}
	return out
}

// TopIPs is the addresses with the most refused or watched requests.
func (s *Store) TopIPs(f TrafficFilter, limit int, now time.Time) []Group {
	w := []string{"at > ?"}
	args := []any{now.Add(-f.Window).UnixMilli()}
	if f.ASN != 0 {
		w, args = append(w, "asn = ?"), append(args, f.ASN)
	}
	if f.Host != "" {
		w, args = append(w, "host = ?"), append(args, f.Host)
	}
	if f.Rule != "" {
		w, args = append(w, "rule = ?"), append(args, f.Rule)
	}
	if f.Country != "" {
		w, args = append(w, "country = ?"), append(args, f.Country)
	}
	orgs := s.Orgs()
	rows, err := s.db.Query(`SELECT ip, COUNT(*) AS c, MAX(asn), MAX(country) FROM events WHERE `+strings.Join(w, " AND ")+
		` GROUP BY ip ORDER BY c DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Group
	top := 0
	for rows.Next() {
		var g Group
		var asn int
		var country string
		if rows.Scan(&g.Key, &g.Total, &asn, &country) == nil {
			g.Label = g.Key + " · " + country
			if o := orgs[itoa(asn)]; o != "" {
				g.Label += " · " + o
			}
			top = max(top, g.Total)
			out = append(out, g)
		}
	}
	for i := range out {
		out[i].Share = 100 * float64(out[i].Total) / float64(max(1, top))
	}
	return out
}
