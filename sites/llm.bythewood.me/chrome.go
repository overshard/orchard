package main

import (
	"fmt"
	"html/template"
	"slices"
	"time"

	"llm.bythewood.me/suite"
)

// chrome is the header and status line for a signed in page.
func (s *site) chrome(on string, day dayStats, m Month) suite.Page {
	p := suite.Page{Tool: "llm", SignedIn: true, Source: sourceURL, Author: author, Year: time.Now().Year()}
	for _, t := range []suite.Tab{{Name: "overview", Href: "/"}, {Name: "calls", Href: "/calls"}, {Name: "keys", Href: "/keys"}} {
		t.On = t.Name == on
		p.Tabs = append(p.Tabs, t)
	}
	p.Status = []suite.Seg{{Text: s.modeName()}}
	if s.claude != nil && m.Budget > 0 {
		p.Meta = fmt.Sprintf("answering from %s · $%.2f of $%.0f this month", s.modeName(), m.Spent, m.Budget)
		kind := ""
		if m.Projected > m.Budget {
			kind = "warn"
		}
		p.Status = append(p.Status, suite.Seg{Text: fmt.Sprintf("$%.2f / $%.0f", m.Spent, m.Budget), Kind: kind})
	} else {
		p.Meta = "answering from " + s.modeName()
	}
	if day.Errors > 0 {
		p.Status = append(p.Status, suite.Seg{Text: "● " + count(day.Errors, "failed", "failed") + " 24h", Kind: "bad"})
	} else if day.Calls > 0 {
		p.Status = append(p.Status, suite.Seg{Text: "● none failed 24h", Kind: "ok"})
	}
	p.Status = append(p.Status, suite.Seg{Text: count(day.Calls, "call", "calls") + " 24h"})
	p.Right = []suite.Seg{{Text: fmt.Sprintf("calls kept %dd", int(s.retain.Hours()/24))}}
	return p
}

// modeName says what answers right now, Claude or the card, so no page claims
// the other.
func (s *site) modeName() string {
	if s.claude != nil {
		return "claude"
	}
	return s.model
}

type dayStats struct {
	Calls, Errors int
	P50           int64
	CallsByHour   []float64
	ErrorsByHour  []float64
	MSByHour      []float64
}

// dayStats buckets the last 24 hours of calls by hour, oldest first.
func (s *site) dayStats(now time.Time) dayStats {
	d := dayStats{CallsByHour: make([]float64, 24), ErrorsByHour: make([]float64, 24), MSByHour: make([]float64, 24)}
	rows, err := s.store.db.Query(`SELECT at, ms, status, err FROM calls WHERE at >= ?`, now.Add(-24*time.Hour).Unix())
	if err != nil {
		return d
	}
	defer rows.Close()
	var all []int64
	sums := make([]int64, 24)
	for rows.Next() {
		var at, ms int64
		var status int
		var e string
		if rows.Scan(&at, &ms, &status, &e) != nil {
			continue
		}
		i := 23 - int(now.Sub(time.Unix(at, 0)).Hours())
		if i < 0 || i > 23 {
			continue
		}
		d.Calls++
		d.CallsByHour[i]++
		sums[i] += ms
		all = append(all, ms)
		if status >= 400 || e != "" {
			d.Errors++
			d.ErrorsByHour[i]++
		}
	}
	for i := range sums {
		if d.CallsByHour[i] > 0 {
			d.MSByHour[i] = float64(sums[i]) / d.CallsByHour[i]
		}
	}
	if len(all) > 0 {
		slices.Sort(all)
		d.P50 = all[len(all)/2]
	}
	return d
}

type callerSpend struct {
	Caller string
	Spent  float64
	Share  float64
}

// spendByDay is the month so far in dollars a day on the budget's clock, and
// spendByCaller the same month split by the key that spent it.
func (s *site) spendByDay(m Month, now time.Time) []float64 {
	days := int(now.In(budgetZone).Sub(m.Start).Hours()/24) + 1
	out := make([]float64, days)
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
			out[i] += float64(micros) / 1e6
		}
	}
	return out
}

func (s *site) spendByCaller(m Month) []callerSpend {
	rows, err := s.store.db.Query(`SELECT COALESCE(k.name, ''), SUM(s.micros) FROM spend s
		LEFT JOIN keys k ON k.id = s.key_id WHERE s.at >= ? GROUP BY 1 ORDER BY 2 DESC`, m.Start.Unix())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []callerSpend
	for rows.Next() {
		var c callerSpend
		var micros int64
		if rows.Scan(&c.Caller, &micros) != nil {
			continue
		}
		c.Spent = float64(micros) / 1e6
		if m.Spent > 0 {
			c.Share = 100 * c.Spent / m.Spent
		}
		out = append(out, c)
	}
	return out
}

// headline is the overview's title, the month against the budget when there
// is one and the day's calls when there isn't.
func (s *site) headline(day dayStats, m Month) template.HTML {
	if s.claude != nil && m.Budget > 0 {
		cls := ""
		if m.Projected > m.Budget {
			cls = ` class="bad"`
		}
		pace := fmt.Sprintf("$%.0f", m.Projected)
		if m.Projected < 10 {
			pace = fmt.Sprintf("$%.2f", m.Projected)
		}
		return template.HTML(fmt.Sprintf(`On pace for <em%s>%s</em> of $%.0f.`, cls, pace, m.Budget))
	}
	if day.Errors > 0 {
		return template.HTML(fmt.Sprintf(`%s today, <em class="bad">%s</em>.`, count(day.Calls, "call", "calls"), count(day.Errors, "failed", "failed")))
	}
	return template.HTML(template.HTMLEscapeString(count(day.Calls, "call", "calls")) + " today.")
}

// count is n with the word that fits it, like 1 call or 3 calls.
func count(n int, one, many string) string {
	return fmt.Sprintf("%d %s", n, plural(n, one, many))
}

func secs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
