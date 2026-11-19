package main

import (
	"fmt"
	"net/http"

	"analytics.bythewood.me/suite"
)

// suiteState is analytics' entry in the merged index, every property with its
// week of page views. Nothing here is ever trouble, so the dot stays green.
func (s *site) suiteState(r *http.Request) suite.State {
	ctx := r.Context()
	base := "https://analytics.bythewood.me"
	st := suite.State{Tool: "analytics", Level: "ok", Items: []suite.Item{
		{Kind: "page", Name: "properties", Href: base + "/properties", Note: "every property and its last 14 days"},
		{Kind: "page", Name: "docs", Href: base + "/documentation", Note: "the collector tag and custom events"},
	}}
	rows, err := s.db.QueryContext(ctx, "SELECT "+propertyColumns+" FROM properties ORDER BY is_protected DESC, created_at ASC")
	if err != nil {
		return suite.State{Tool: "analytics", Note: "couldn't read the properties"}
	}
	var props []*Property
	for rows.Next() {
		if p, err := scanProperty(rows.Scan); err == nil {
			props = append(props, p)
		}
	}
	rows.Close()
	var all int64
	for _, p := range props {
		var week int64
		for _, v := range s.dailyViews(ctx, p.ID[:], 7) {
			week += int64(v)
		}
		all += week
		note := fmt.Sprintf("%s views this week", formatNum(week))
		if week == 0 {
			note = "quiet this week"
		}
		st.Items = append(st.Items, suite.Item{Kind: "property", Name: p.Name, Href: base + "/" + p.ID.String(), Note: note})
	}
	what := "properties"
	if len(props) == 1 {
		what = "property"
	}
	st.Note = fmt.Sprintf("%s views this week across %d %s", formatNum(all), len(props), what)
	return st
}
