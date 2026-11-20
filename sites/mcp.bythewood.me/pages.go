package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
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
	top := 1
	for _, g := range grants {
		top = max(top, use[g.ID])
	}
	rows := make([]connRow, 0, len(grants))
	for _, g := range grants {
		rows = append(rows, newConnRow(g, use[g.ID], top, now))
	}
	recent, _ := s.store.Calls(CallFilter{Since: now.AddDate(0, 0, -7), Limit: 14})
	events, _ := s.store.Events(12)
	s.render(w, 200, "overview.html", map[string]any{"Suite": page, "Day": day, "Headline": headline(day, len(grants)),
		"Grants": rows, "Top": top, "Tools": s.toolUses(now.AddDate(0, 0, -7)), "Recent": recent, "Events": events, "Resource": resourceURL,
		"Causes": s.store.Causes(now.AddDate(0, 0, -7))})
}

// callRanges are the windows /calls can show, the last being everything kept.
var callRanges = []struct {
	Name string
	Dur  time.Duration
}{{"24h", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour}, {"30d", 30 * 24 * time.Hour}, {"90d", keepFor}}

type chip struct{ Name, Href, Kind string }

func (s *site) callsPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "calls")
	q := r.URL.Query()
	now := time.Now()
	f := CallFilter{Tool: q.Get("tool"), Client: q.Get("client"), Q: strings.TrimSpace(q.Get("q")), Failed: q.Get("failed") != "",
		Since: now.Add(-keepFor), Limit: 200}
	f.ID, _ = strconv.ParseInt(q.Get("id"), 10, 64)
	f.Range = "90d"
	for _, rg := range callRanges {
		if rg.Name == q.Get("range") {
			f.Range, f.Since = rg.Name, now.Add(-rg.Dur)
		}
	}
	calls, _ := s.store.Calls(f)
	total, failed := s.store.CountCalls(f)
	var ranges []chip
	for _, rg := range callRanges {
		c := chip{Name: rg.Name, Href: filterURL(f, "range", rg.Name)}
		if rg.Name == f.Range {
			c.Kind = "brass"
		}
		ranges = append(ranges, c)
	}
	head := "Nothing"
	switch {
	case total == 0:
	case total > len(calls):
		head = fmt.Sprintf("The newest %d of %s", len(calls), comma(total))
	default:
		head = comma(total)
	}
	ms := make([]int64, 0, len(calls))
	for _, c := range calls {
		ms = append(ms, c.MS)
	}
	slices.Sort(ms)
	var p50 int64
	if len(ms) > 0 {
		p50 = ms[len(ms)/2]
	}
	s.render(w, 200, "calls.html", map[string]any{"Suite": page, "Calls": calls, "Filter": f, "Clients": s.store.Clients(now.Add(-keepFor)), "Tools": tools,
		"Head": head, "Total": total, "Failed": failed, "P50": p50, "Ranges": ranges, "Keep": int(keepFor / (24 * time.Hour)),
		"Filtered": f.Tool != "" || f.Client != "" || f.Q != "" || f.Failed || f.ID > 0 || f.Range != "90d"})
}

func (s *site) toolsPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "tools")
	now := time.Now()
	rows := s.store.toolRows(now)
	var calls, failed, failing, never int
	var ms []int64
	week := make([]float64, 7)
	weekFailed := make([]float64, 7)
	var labels []string
	if len(rows) > 0 {
		labels = rows[0].WeekLabels
	}
	for _, t := range rows {
		for i, v := range t.Week {
			week[i] += v
			weekFailed[i] += t.WeekFailed[i]
		}
		calls += t.Calls
		failed += t.Failed
		if t.Failed > 0 {
			failing++
		}
		if t.LastUsed.IsZero() {
			never++
		}
	}
	recent, _ := s.store.Calls(CallFilter{Since: now.AddDate(0, 0, -7), Limit: 100000})
	for _, c := range recent {
		if c.Tool != "" {
			ms = append(ms, c.MS)
		}
	}
	slices.Sort(ms)
	var p50 int64
	if len(ms) > 0 {
		p50 = ms[len(ms)/2]
	}
	grants, _ := s.store.Grants()
	use := s.store.GrantUse(now.AddDate(0, 0, -7))
	var conns []connRow
	top := 1
	for _, g := range grants {
		top = max(top, use[g.ID])
	}
	for _, g := range grants {
		conns = append(conns, newConnRow(g, use[g.ID], top, now))
	}
	reads, live := 0, 0
	for _, t := range tools {
		if !t.Writes {
			reads++
		}
		if t.Live {
			live++
		}
	}
	s.render(w, 200, "tools.html", map[string]any{"Suite": page, "Rows": rows, "Calls": calls, "Failed": failed, "Failing": failing,
		"Never": never, "P50": p50, "Grants": len(grants), "Headline": toolsHeadline(len(rows), failing, never),
		"Causes": s.store.Causes(now.AddDate(0, 0, -7)), "Args": s.store.TopArgs(now.AddDate(0, 0, -7), 6), "Conns": conns,
		"Reads": reads, "Writes": len(tools) - reads, "Live": live, "Week": week, "WeekFailed": weekFailed, "Labels": labels})
}

func toolsHeadline(n, failing, never int) template.HTML {
	all := count(n, "tool", "tools")
	switch {
	case failing > 0:
		return template.HTML(fmt.Sprintf(`%s, <em class="bad">%d failing</em>.`, all, failing))
	case never == n:
		return template.HTML(fmt.Sprintf(`%s, <em>none called yet</em>.`, all))
	}
	return template.HTML(fmt.Sprintf(`%s, <em>all answering</em>.`, all))
}

// connRow is a connector with when its auth session runs out and whether it
// has sat unused long enough to be worth revoking.
type connRow struct {
	Grant
	Week    int
	Share   float64
	Expires time.Time
	Idle    bool
}

// connectorTTL matches the session auth mints a connector, which doesn't slide.
const connectorTTL = 90 * 24 * time.Hour

const idleAfter = 30 * 24 * time.Hour

func newConnRow(g Grant, week, top int, now time.Time) connRow {
	last := g.LastUsed
	if last.IsZero() {
		last = g.Created
	}
	return connRow{Grant: g, Week: week, Share: 100 * float64(week) / float64(max(top, 1)),
		Expires: g.Created.Add(connectorTTL), Idle: now.Sub(last) > idleAfter}
}

func (s *site) docs(w http.ResponseWriter, r *http.Request) {
	page, in := s.chrome(r, "docs")
	data := map[string]any{"Suite": page, "SignedIn": in, "Resource": resourceURL, "Tools": tools,
		"Keep": int(keepFor / (24 * time.Hour)), "Footer": footer()}
	// Only a signed in reader sees whether a connector is live, since the
	// docs are public.
	if in {
		data["Clients"] = s.clientStates()
	}
	s.render(w, 200, "docs.html", data)
}

// clientState is whether a kind of Claude client is connected and when it
// last shook hands, for the docs' connect blocks.
type clientState struct {
	Connected bool
	Handshake time.Time
}

// clientStates is keyed by the metadata document a client signs in with.
// The desktop app adds connectors through claude.ai, so it shares that one.
func (s *site) clientStates() map[string]clientState {
	grants, _ := s.store.Grants()
	shakes := s.store.LastHandshake()
	out := map[string]clientState{}
	for _, g := range grants {
		key := "code"
		if g.ClientID == knownClients[0] {
			key = "web"
		}
		st := out[key]
		st.Connected = true
		if t := shakes[g.ClientName]; t.After(st.Handshake) {
			st.Handshake = t
		}
		out[key] = st
	}
	return out
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
	if f.Q != "" {
		v.Set("q", f.Q)
	}
	if f.Range != "" && f.Range != "90d" {
		v.Set("range", f.Range)
	}
	if val == "" || (key == "range" && val == "90d") {
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
	return suite.Footer{Tool: "mcp", About: "Lets an agent read every one of my tools, signed in as me and unable to change a thing beyond proposing a gate rule.",
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
			{Kind: "page", Name: "tools", Href: baseURL + "/tools", Note: "each tool's calls, failures and last error"},
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
