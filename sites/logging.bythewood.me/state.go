package main

import (
	"fmt"
	"net/http"
	"time"

	"logging.bythewood.me/suite"
)

// suiteState is logging's entry in the merged index, every source with its
// last hour.
func (s *site) suiteState(r *http.Request) suite.State {
	now := time.Now()
	// A day to find every source, so a silent one still shows, and the last
	// hour for whether it's in trouble now.
	rows := sources(r.Context(), s.db, filter{StartMS: now.Add(-24 * time.Hour).UnixMilli(), EndMS: now.UnixMilli()})
	s.markHeard(rows)
	hour := map[string]SourceRow{}
	for _, src := range sources(r.Context(), s.db, filter{StartMS: now.Add(-time.Hour).UnixMilli(), EndMS: now.UnixMilli()}) {
		hour[src.Source] = src
	}
	base := "https://logging.bythewood.me"
	st := suite.State{Tool: "logging", Level: "ok", Items: []suite.Item{
		{Kind: "page", Name: "overview", Href: base + "/overview", Note: "every source, last 24 hours"},
		{Kind: "page", Name: "search", Href: base + "/search", Note: "every raw line for thirty days"},
	}}
	var quiet int
	for _, day := range rows {
		src := hour[day.Source]
		src.Source, src.IsLive = day.Source, day.IsLive
		it := suite.Item{Kind: "source", Name: src.Source, Href: base + "/sources/" + src.Source, Note: fmt.Sprintf("%s records 1h", formatNum(src.Records))}
		// One stray 5xx an hour is normal for a public site, five isn't.
		switch n := src.Errors + src.Server5xx; {
		case n >= 5:
			it.Level = "bad"
			it.Note = fmt.Sprintf("%d errors · %d 5xx in the last hour", src.Errors, src.Server5xx)
			st.Count++
		case n > 0:
			it.Level = "warn"
			it.Note = fmt.Sprintf("%d errors · %d 5xx in the last hour", src.Errors, src.Server5xx)
		case !src.IsLive:
			it.Level, it.Note = "warn", "quiet, no line in 15 minutes"
			quiet++
		}
		st.Items = append(st.Items, it)
	}
	switch {
	case st.Count > 0:
		st.Level = "bad"
		st.Note = fmt.Sprintf("%d of %d sources erroring in the last hour", st.Count, len(rows))
	case quiet > 0:
		st.Level, st.Count = "warn", quiet
		st.Note = fmt.Sprintf("%d of %d sources quiet", quiet, len(rows))
	default:
		st.Note = fmt.Sprintf("%d sources, nothing wrong in the last hour", len(rows))
	}
	return st
}
