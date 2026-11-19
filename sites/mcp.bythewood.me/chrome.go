package main

import (
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"sort"
	"time"

	"mcp.bythewood.me/suite"
)

func (s *site) chrome(r *http.Request, on string) (suite.Page, bool) {
	in := s.auth.Authenticated(r)
	p := suite.Page{Tool: "mcp", SignedIn: in, Source: sourceURL, Author: author, Year: time.Now().Year(), Meta: resourceURL}
	tabs := []suite.Tab{{Name: "home", Href: "/"}, {Name: "docs", Href: "/docs"}, {Name: "source", Href: sourceURL}}
	if in {
		tabs = []suite.Tab{{Name: "overview", Href: "/"}, {Name: "calls", Href: "/calls"}, {Name: "docs", Href: "/docs"}}
	}
	for _, t := range tabs {
		t.On = t.Name == on
		p.Tabs = append(p.Tabs, t)
	}
	p.Right = []suite.Seg{{Text: "read only"}, {Text: "times are UTC"}}
	p.Status = []suite.Seg{{Text: fmt.Sprintf("%d tools", len(tools))}}
	if in {
		grants, _ := s.store.Grants()
		day := s.dayStats(time.Now())
		p.Status = append(p.Status, suite.Seg{Text: count(len(grants), "connector", "connectors")})
		if day.Failed > 0 {
			p.Status = append(p.Status, suite.Seg{Text: fmt.Sprintf("● %d failed 24h", day.Failed), Kind: "bad"})
		} else {
			p.Status = append(p.Status, suite.Seg{Text: "● all answered", Kind: "ok"})
		}
		p.Status = append(p.Status, suite.Seg{Text: count(day.Calls, "call", "calls") + " 24h"})
	}
	return p, in
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// dayStats is the last 24 hours of tool calls an hour each, oldest first. A
// tools/list or a handshake isn't a call anybody asked for, so only tools count.
type dayStats struct {
	Calls, Failed                  int
	P50                            int64
	ByHour, FailedByHour, MSByHour []float64
}

func (s *site) dayStats(now time.Time) dayStats {
	d := dayStats{ByHour: make([]float64, 24), FailedByHour: make([]float64, 24), MSByHour: make([]float64, 24)}
	calls, _ := s.store.Calls(CallFilter{Since: now.Add(-24 * time.Hour), Limit: 100000})
	sums := make([]float64, 24)
	var all []int64
	for _, c := range calls {
		if c.Tool == "" {
			continue
		}
		i := 23 - int(now.Sub(c.At).Hours())
		if i < 0 || i > 23 {
			continue
		}
		d.Calls++
		d.ByHour[i]++
		sums[i] += float64(c.MS)
		all = append(all, c.MS)
		if !c.OK {
			d.Failed++
			d.FailedByHour[i]++
		}
	}
	for i := range sums {
		if d.ByHour[i] > 0 {
			d.MSByHour[i] = sums[i] / d.ByHour[i]
		}
	}
	if len(all) > 0 {
		slices.Sort(all)
		d.P50 = all[len(all)/2]
	}
	return d
}

type toolUse struct {
	Tool          string
	Calls, Failed int
	Share         float64
}

// toolUses is each tool's calls over a window, every tool listed even when
// nothing called it, busiest first.
func (s *site) toolUses(since time.Time) []toolUse {
	calls, _ := s.store.Calls(CallFilter{Since: since, Limit: 100000})
	by := map[string]*toolUse{}
	for _, t := range tools {
		by[t.Name] = &toolUse{Tool: t.Name}
	}
	top := 0
	for _, c := range calls {
		u := by[c.Tool]
		if u == nil {
			continue
		}
		u.Calls++
		if !c.OK {
			u.Failed++
		}
		top = max(top, u.Calls)
	}
	out := make([]toolUse, 0, len(by))
	for _, u := range by {
		if top > 0 {
			u.Share = 100 * float64(u.Calls) / float64(top)
		}
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		return out[i].Tool < out[j].Tool
	})
	return out
}

func headline(day dayStats, grants int) template.HTML {
	switch {
	case grants == 0 && day.Calls == 0:
		return `Nothing connected, <em>add it to Claude</em>.`
	case day.Failed > 0:
		return template.HTML(fmt.Sprintf(`%s today, <em class="bad">%d failed</em>.`, count(day.Calls, "call", "calls"), day.Failed))
	case day.Calls == 0:
		return template.HTML(fmt.Sprintf(`%s, <em>quiet today</em>.`, count(grants, "connector", "connectors")))
	}
	return template.HTML(fmt.Sprintf(`%s today, <em>all answered</em>.`, count(day.Calls, "call", "calls")))
}

func secs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func comma(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
