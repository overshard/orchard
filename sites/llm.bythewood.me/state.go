package main

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"llm.bythewood.me/suite"
)

// suiteState is llm's entry in the merged index, the month against the
// budget and every key with what it's spent.
func (s *site) suiteState(r *http.Request) suite.State {
	now := time.Now()
	m, _ := s.month(now)
	day := s.dayStats(now)
	base := "https://llm.bythewood.me"
	st := suite.State{Tool: "llm", Level: "ok", Items: []suite.Item{
		{Kind: "page", Name: "overview", Href: base + "/", Note: fmt.Sprintf("$%.2f spent this month", m.Spent)},
		{Kind: "page", Name: "calls", Href: base + "/calls", Note: fmt.Sprintf("%d in the last 24 hours", day.Calls)},
		{Kind: "page", Name: "failed calls", Href: base + "/calls?failed=1", Note: fmt.Sprintf("%d failed in the last 24 hours", day.Errors)},
		{Kind: "page", Name: "keys", Href: base + "/keys", Note: "one per caller"},
	}}
	st.Note = fmt.Sprintf("$%.2f this month, %s today", m.Spent, count(day.Calls, "call", "calls"))
	if m.Budget > 0 {
		st.Note = fmt.Sprintf("$%.2f of $%.0f this month, %s today", m.Spent, m.Budget, count(day.Calls, "call", "calls"))
		if m.Projected > m.Budget {
			st.Level = "warn"
			st.Note = fmt.Sprintf("on pace for $%.2f of $%.0f", m.Projected, m.Budget)
		}
	}
	if day.Errors > 0 {
		st.Level, st.Count = "warn", day.Errors
		st.Note = count(day.Errors, "failed call", "failed calls") + " today, " + st.Note
	}
	use := s.keyUses(m, now)
	keys, _ := s.store.Keys()
	for _, k := range keys {
		if k.Revoked {
			continue
		}
		it := suite.Item{Kind: "key", Name: k.Name, Href: base + "/calls?caller=" + url.QueryEscape(k.Name), Note: "no calls in 14 days"}
		if u := use[k.ID]; u != nil {
			it.Note = fmt.Sprintf("%d calls in 14 days · $%.2f this month", u.Calls, u.Spent)
		}
		st.Items = append(st.Items, it)
	}
	return st
}
