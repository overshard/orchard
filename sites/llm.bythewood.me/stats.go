package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// dayStats is the last 24 hours of calls by the hour, oldest first, with the
// 24 hours before for the deltas.
type dayStats struct {
	Calls, Errors, PrevCalls int
	P50, P95, PrevP50        int64
	// ErrorKinds is the failures by status, like 2× 529 · 1× 422.
	ErrorKinds string
	Slowest    string
	Labels     []string
	CallsByHour, ErrorsByHour, P50ByHour, P95ByHour,
	SpendByHour, CacheByHour []float64
	Spent float64
	// CacheHit is the share of input tokens read from the cache, and negative
	// with nothing to measure.
	CacheHit float64
	Callers  []callerDay
}

type callerDay struct {
	Caller        string
	Calls, Errors int
	P50, P95      int64
	Spent         float64
	Hours         []float64
}

// pct is the nearest rank percentile of sorted.
func pct(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[max(0, min(i, len(sorted)-1))]
}

func (s *site) dayStats(now time.Time) dayStats {
	d := dayStats{CacheHit: -1}
	for _, p := range []*[]float64{&d.CallsByHour, &d.ErrorsByHour, &d.P50ByHour, &d.P95ByHour, &d.SpendByHour, &d.CacheByHour} {
		*p = make([]float64, 24)
	}
	for i := range 24 {
		d.Labels = append(d.Labels, now.Add(time.Duration(i-23)*time.Hour).In(budgetZone).Format("Mon 15:00"))
	}
	hour := func(at int64) int { return 23 - int(now.Sub(time.Unix(at, 0)).Hours()) }

	rows, err := s.store.db.Query(`SELECT at, caller, ms, status, err FROM calls WHERE at >= ?`, now.Add(-48*time.Hour).Unix())
	if err != nil {
		return d
	}
	var all, prev []int64
	byHour := make([][]int64, 24)
	type acc struct {
		day callerDay
		ms  []int64
	}
	callers := map[string]*acc{}
	kinds := map[string]int{}
	for rows.Next() {
		var at, ms int64
		var status int
		var caller, e string
		if rows.Scan(&at, &caller, &ms, &status, &e) != nil {
			continue
		}
		i := hour(at)
		if i < 0 {
			d.PrevCalls++
			prev = append(prev, ms)
			continue
		}
		if i > 23 {
			continue
		}
		c := callers[caller]
		if c == nil {
			c = &acc{day: callerDay{Caller: caller, Hours: make([]float64, 24)}}
			callers[caller] = c
		}
		d.Calls++
		d.CallsByHour[i]++
		c.day.Calls++
		c.day.Hours[i]++
		c.ms = append(c.ms, ms)
		all = append(all, ms)
		byHour[i] = append(byHour[i], ms)
		if status >= 400 || e != "" {
			d.Errors++
			d.ErrorsByHour[i]++
			c.day.Errors++
			k := "err"
			if status > 0 {
				k = fmt.Sprint(status)
			}
			kinds[k]++
		}
	}
	rows.Close()

	slices.Sort(all)
	slices.Sort(prev)
	d.P50, d.P95, d.PrevP50 = pct(all, 0.5), pct(all, 0.95), pct(prev, 0.5)
	for i, ms := range byHour {
		slices.Sort(ms)
		d.P50ByHour[i], d.P95ByHour[i] = float64(pct(ms, 0.5)), float64(pct(ms, 0.95))
	}
	var ks []string
	for k := range kinds {
		ks = append(ks, k)
	}
	slices.SortFunc(ks, func(a, b string) int { return cmp.Or(kinds[b]-kinds[a], strings.Compare(a, b)) })
	for i, k := range ks {
		if i == 3 {
			break
		}
		ks[i] = fmt.Sprintf("%d× %s", kinds[k], k)
	}
	d.ErrorKinds = strings.Join(ks[:min(3, len(ks))], " · ")

	// Spend is by key, and a key's name is the caller a call row carries.
	srows, err := s.store.db.Query(`SELECT COALESCE(k.name, ''), s.micros, s.in_tok, s.cache_read, s.cache_write, s.at
		FROM spend s LEFT JOIN keys k ON k.id = s.key_id WHERE s.at >= ?`, now.Add(-24*time.Hour).Unix())
	if err == nil {
		var input, read float64
		readByHour, inByHour := make([]float64, 24), make([]float64, 24)
		for srows.Next() {
			var name string
			var micros, in, cr, cw, at int64
			if srows.Scan(&name, &micros, &in, &cr, &cw, &at) != nil {
				continue
			}
			i := hour(at)
			if i < 0 || i > 23 {
				continue
			}
			usd := float64(micros) / 1e6
			d.Spent += usd
			d.SpendByHour[i] += usd
			if c := callers[name]; c != nil {
				c.day.Spent += usd
			}
			input += float64(in + cr + cw)
			read += float64(cr)
			inByHour[i] += float64(in + cr + cw)
			readByHour[i] += float64(cr)
		}
		srows.Close()
		if input > 0 {
			d.CacheHit = 100 * read / input
		}
		for i := range inByHour {
			if inByHour[i] > 0 {
				d.CacheByHour[i] = 100 * readByHour[i] / inByHour[i]
			}
		}
	}

	var slowest int64
	for _, c := range callers {
		slices.Sort(c.ms)
		c.day.P50, c.day.P95 = pct(c.ms, 0.5), pct(c.ms, 0.95)
		if c.day.P95 > slowest && len(callers) > 1 {
			slowest, d.Slowest = c.day.P95, c.day.Caller
		}
		d.Callers = append(d.Callers, c.day)
	}
	slices.SortFunc(d.Callers, func(a, b callerDay) int { return cmp.Or(b.Calls-a.Calls, strings.Compare(a.Caller, b.Caller)) })
	return d
}

// spendDay is one day of the month on the budget's clock.
type spendDay struct {
	Day   time.Time
	Spent float64
}

func (s *site) spendByDay(m Month, now time.Time) []spendDay {
	days := int(now.In(budgetZone).Sub(m.Start).Hours()/24) + 1
	out := make([]spendDay, days)
	for i := range out {
		out[i].Day = m.Start.AddDate(0, 0, i)
	}
	rows, err := s.store.db.Query(`SELECT micros, at FROM spend WHERE at >= ?`, m.Start.Unix())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var micros, at int64
		if rows.Scan(&micros, &at) != nil {
			continue
		}
		i := int(time.Unix(at, 0).In(budgetZone).Sub(m.Start).Hours() / 24)
		if i >= 0 && i < days {
			out[i].Spent += float64(micros) / 1e6
		}
	}
	return out
}

// keySpend is one key's month against its cap, with Bar and Cap as shares of
// the widest row so the bars and the cap rules share a scale.
type keySpend struct {
	Name      string
	Spent     float64
	Cap       float64
	Bar, Mark float64
	Revoked   bool
}

func (s *site) spendByKey(m Month) []keySpend {
	rows, err := s.store.db.Query(`SELECT COALESCE(k.name, ''), COALESCE(k.cap_micros, 0), COALESCE(k.revoked_at, 0), COALESCE(SUM(s.micros), 0)
		FROM keys k LEFT JOIN spend s ON s.key_id = k.id AND s.at >= ?
		GROUP BY k.id HAVING SUM(s.micros) > 0 OR (k.cap_micros > 0 AND k.revoked_at = 0)
		ORDER BY 4 DESC, 1`, m.Start.Unix())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []keySpend
	var top float64
	for rows.Next() {
		var k keySpend
		var capMicros, revoked, micros int64
		if rows.Scan(&k.Name, &capMicros, &revoked, &micros) != nil {
			continue
		}
		k.Spent, k.Cap, k.Revoked = float64(micros)/1e6, float64(capMicros)/1e6, revoked != 0
		top = max(top, k.Spent, k.Cap)
		out = append(out, k)
	}
	for i := range out {
		if top > 0 {
			out[i].Bar, out[i].Mark = 100*out[i].Spent/top, 100*out[i].Cap/top
		}
	}
	return out
}

// mixPart is one line of the month's token mix.
type mixPart struct {
	Name, Kind string
	Tokens     int64
	Spent      float64
	Share      float64
}

// tokenMix is the month's spend by what it paid for. Each row is priced alone,
// since the long prompt rate is per response, and scaled to what the ledger
// charged. Rows from before the split was kept are only money, so they're apart.
func (s *site) tokenMix(m Month) (parts []mixPart, unsplit float64) {
	rows, err := s.store.db.Query(`SELECT micros, in_tok, cache_read, cache_write, out_tok, searches FROM spend WHERE at >= ?`, m.Start.Unix())
	if err != nil {
		return nil, 0
	}
	defer rows.Close()
	parts = []mixPart{{Name: "out", Kind: "out"}, {Name: "in, uncached", Kind: "in"}, {Name: "searches", Kind: "search"},
		{Name: "cache read", Kind: "read"}, {Name: "cache write", Kind: "write"}}
	var total float64
	for rows.Next() {
		var c Call
		var micros, out int64
		if rows.Scan(&micros, &c.InTok, &c.CacheRead, &c.CacheWrite, &out, &c.Searches) != nil {
			continue
		}
		c.OutputTok = int(out)
		if !c.recorded() && out == 0 && c.Searches == 0 {
			unsplit += float64(micros) / 1e6
			continue
		}
		ps := c.split(micros)
		for i := range parts {
			parts[i].Spent += ps[i].Spent
			parts[i].Tokens += ps[i].Tokens
			total += ps[i].Spent
		}
	}
	for i := range parts {
		if total > 0 {
			parts[i].Share = 100 * parts[i].Spent / total
		}
	}
	return parts, unsplit
}

// split is a call's cost by part in dollars, in the order tokenMix shows them,
// scaled so the parts add up to micros.
func (c Call) split(micros int64) []mixPart {
	p := priceParts(c.usage())
	raw := []float64{float64(p.Out), float64(p.In), float64(p.Search) * 1e6, float64(p.CacheRead), float64(p.CacheWrite)}
	var sum float64
	for _, v := range raw {
		sum += v
	}
	scale := 1e-12
	if sum > 0 && micros > 0 {
		scale = float64(micros) / 1e6 / sum
	}
	u := c.usage()
	toks := []int64{u.OutputTokens, u.InputTokens, c.Searches, c.CacheRead, c.CacheWrite}
	names := [][2]string{{"out", "out"}, {"in, uncached", "in"}, {"searches", "search"}, {"cache read", "read"}, {"cache write", "write"}}
	out := make([]mixPart, len(raw))
	for i, v := range raw {
		out[i] = mixPart{Name: names[i][0], Kind: names[i][1], Tokens: toks[i], Spent: v * scale}
		if sum > 0 {
			out[i].Share = 100 * v / sum
		}
	}
	return out
}

// Parts is the call's cost split for its drawer.
func (c Call) Parts() []mixPart { return c.split(c.Cost) }

// CacheSaved is what the cache reads would have cost as plain input.
func (c Call) CacheSaved() float64 {
	if c.CacheRead == 0 {
		return 0
	}
	u := c.usage()
	plain := u
	plain.InputTokens += plain.CacheReadInputTokens
	plain.CacheReadInputTokens = 0
	return float64(costMicros(plain)-costMicros(u)) / 1e6
}

// budgetAlert is one alert this month as the overview lists it.
type budgetAlert struct {
	What string
	At   time.Time
	step bool
}

// alertLog is this month's alerts, and sent the day each step went out.
func (s *site) alertLog(m Month) (log []budgetAlert, sent map[string]string) {
	sent = map[string]string{}
	rows, err := s.store.db.Query(`SELECT kind, at FROM spend_alerts WHERE at >= ? ORDER BY at, kind`, m.Start.Unix())
	if err != nil {
		return nil, sent
	}
	defer rows.Close()
	month := m.Start.Format("2006-01") + "-"
	names := map[string]string{"50": "passed 50% of the budget", "80": "passed 80% of the budget", "over": "went over the budget", "pace": "running hot"}
	for rows.Next() {
		var kind string
		var at int64
		if rows.Scan(&kind, &at) != nil {
			continue
		}
		t := time.Unix(at, 0)
		if step, ok := strings.CutPrefix(kind, month); ok {
			sent[step] = t.In(budgetZone).Format("Jan 2")
			// A higher step marks the ones under it in the same second, and
			// only the higher one went out.
			if n := len(log); n > 0 && log[n-1].step && log[n-1].At.Equal(t) && step != "pace" {
				log = log[:n-1]
			}
			log = append(log, budgetAlert{What: names[step], At: t, step: step != "pace"})
		} else if strings.HasPrefix(kind, "week-") {
			log = append(log, budgetAlert{What: "the Monday line", At: t})
		}
	}
	return log, sent
}

// FirstPct and GenMS place a streamed call's first token on its waterfall.
func (c Call) FirstPct() float64 {
	if c.MS <= 0 {
		return 0
	}
	return min(100, 100*float64(c.FirstMS)/float64(c.MS))
}

func (c Call) GenMS() int64 { return max(0, c.MS-c.FirstMS) }
