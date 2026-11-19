package main

import (
	"fmt"
	"net/http"

	"status.bythewood.me/suite"
)

// suiteState is status' entry in the merged index, every property with
// whether it's answering.
func (s *site) suiteState(r *http.Request) suite.State {
	st := suite.State{Tool: "status", Level: "ok"}
	rows, err := listProperties(r.Context(), s.db, "")
	if err != nil {
		return suite.State{Tool: "status", Note: "couldn't read the properties"}
	}
	base := "https://status.bythewood.me"
	st.Items = append(st.Items, suite.Item{Kind: "page", Name: "properties", Href: base + "/properties", Note: "every site and its last check"})
	for _, p := range rows {
		v, err := s.buildPropertyView(r.Context(), p)
		if err != nil {
			continue
		}
		it := suite.Item{Kind: "property", Name: v.Name, Href: base + "/" + v.ID, Note: fmt.Sprintf("up · %dms", v.AvgResponseTime)}
		if v.CurrentStatus != 200 {
			it.Level, it.Note = "bad", fmt.Sprintf("down · %d", v.CurrentStatus)
			st.Count++
		} else if v.HasSecurityIssue || len(v.CrawlerInsights) > 0 {
			it.Note += fmt.Sprintf(" · %d seo issues", len(v.CrawlerInsights))
		}
		st.Items = append(st.Items, it)
	}
	st.Note = count(len(rows), "property", "properties") + ", all up"
	if st.Count > 0 {
		st.Level = "bad"
		st.Note = fmt.Sprintf("%d of %d properties down", st.Count, len(rows))
	}
	return st
}
