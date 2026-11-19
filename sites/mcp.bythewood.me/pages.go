package main

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"mcp.bythewood.me/suite"
)

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	page, in := s.chrome(r, "overview")
	if !in {
		page, _ = s.chrome(r, "home")
		s.render(w, 200, "landing.html", map[string]any{"Suite": page, "Resource": resourceURL, "Tools": tools,
			"Ways": waysFor(), "Footer": footer()})
		return
	}
	now := time.Now()
	day := s.dayStats(now)
	grants, _ := s.store.Grants()
	use := s.store.GrantUse(now.AddDate(0, 0, -7))
	type row struct {
		Grant
		Week int
	}
	rows := make([]row, 0, len(grants))
	top := 1
	for _, g := range grants {
		rows = append(rows, row{g, use[g.ID]})
		top = max(top, use[g.ID])
	}
	recent, _ := s.store.Calls(CallFilter{Since: now.AddDate(0, 0, -7), Limit: 14})
	events, _ := s.store.Events(12)
	s.render(w, 200, "overview.html", map[string]any{"Suite": page, "Day": day, "Headline": headline(day, len(grants)),
		"Grants": rows, "Top": top, "Tools": s.toolUses(now.AddDate(0, 0, -7)), "Recent": recent, "Events": events, "Resource": resourceURL})
}

func (s *site) callsPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "calls")
	q := r.URL.Query()
	f := CallFilter{Tool: q.Get("tool"), Client: q.Get("client"), Failed: q.Get("failed") != "", Since: time.Now().Add(-keepFor), Limit: 200}
	calls, _ := s.store.Calls(f)
	clients := map[string]bool{}
	all, _ := s.store.Calls(CallFilter{Since: time.Now().Add(-keepFor), Limit: 5000})
	for _, c := range all {
		clients[c.Client] = true
	}
	var names []string
	for c := range clients {
		names = append(names, c)
	}
	failed := 0
	for _, c := range calls {
		if !c.OK {
			failed++
		}
	}
	head := fmt.Sprintf("The newest %d", len(calls))
	if len(calls) == 0 {
		head = "Nothing"
	}
	s.render(w, 200, "calls.html", map[string]any{"Suite": page, "Calls": calls, "Filter": f, "Clients": names, "Tools": tools,
		"Head": head, "Failed": failed, "Keep": int(keepFor / (24 * time.Hour))})
}

func (s *site) docs(w http.ResponseWriter, r *http.Request) {
	page, in := s.chrome(r, "docs")
	s.render(w, 200, "docs.html", map[string]any{"Suite": page, "SignedIn": in, "Resource": resourceURL, "Tools": tools,
		"Keep": int(keepFor / (24 * time.Hour)), "Footer": footer()})
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "")
	s.render(w, http.StatusNotFound, "notfound.html", map[string]any{"Suite": page})
}

// filterURL is /calls with one filter changed and the others kept.
func filterURL(f CallFilter, key, val string) string {
	v := url.Values{}
	if f.Tool != "" {
		v.Set("tool", f.Tool)
	}
	if f.Client != "" {
		v.Set("client", f.Client)
	}
	if f.Failed {
		v.Set("failed", "1")
	}
	if val == "" {
		v.Del(key)
	} else {
		v.Set(key, val)
	}
	if len(v) == 0 {
		return "/calls"
	}
	return "/calls?" + v.Encode()
}

func waysFor() suite.Ways {
	return suite.Ways{
		Host:       "mcp.bythewood.me",
		APITitle:   "One url",
		APIText:    "Add it as a connector on claude.ai or in Claude Code and sign in once through auth.",
		API:        `<span class="a">claude</span> mcp add --transport http \` + "\n" + `  orchard https://mcp.bythewood.me/mcp`,
		AgentTitle: "Ask in plain words",
		AgentText:  "Claude picks the tool, reads the report and answers from it, starting with a brief of what's wrong.",
		Agent:      `<span class="c"># what's broken right now?</span>` + "\n" + `<span class="a">orchard_brief</span> &rarr; <span class="g">2 problems</span>`,
		RunText:    "add the url to Claude.",
	}
}

func footer() suite.Footer {
	return suite.Footer{Tool: "mcp", About: "Lets an agent read every one of my tools, signed in as me and unable to change a thing.",
		Source: sourceURL, Author: author, Year: time.Now().Year(),
		Pages: []suite.Tab{{Name: "Home", Href: "/"}, {Name: "Docs", Href: "/docs"}}}
}

// suiteState is mcp's entry in the merged index.
func (s *site) suiteState(r *http.Request) suite.State {
	day := s.dayStats(time.Now())
	grants, _ := s.store.Grants()
	st := suite.State{Tool: "mcp", Level: "ok", Note: fmt.Sprintf("%s, %s today", count(len(grants), "connector", "connectors"), count(day.Calls, "call", "calls")),
		Items: []suite.Item{
			{Kind: "page", Name: "overview", Href: baseURL + "/", Note: "connectors and what they asked"},
			{Kind: "page", Name: "calls", Href: baseURL + "/calls", Note: "every tool call, newest first"},
			{Kind: "page", Name: "failed calls", Href: baseURL + "/calls?failed=1", Note: fmt.Sprintf("%d in the last 24 hours", day.Failed)},
			{Kind: "page", Name: "docs", Href: baseURL + "/docs", Note: "adding it to Claude, and every tool"},
		}}
	if day.Failed > 0 {
		st.Level, st.Count = "warn", day.Failed
		st.Note = count(day.Failed, "failed call", "failed calls") + " today, " + st.Note
	}
	for _, g := range grants {
		st.Items = append(st.Items, suite.Item{Kind: "connector", Name: g.ClientName, Href: baseURL + "/calls?client=" + url.QueryEscape(g.ClientName),
			Note: "allowed " + ago(g.Created)})
	}
	return st
}
