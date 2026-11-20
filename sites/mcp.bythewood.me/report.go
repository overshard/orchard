package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	insightLine = regexp.MustCompile("^- \\[(\\w+)\\] \\[(\\w+)\\] (.*?)(?::\\s*`([^`]*)`)?$")
	digits      = regexp.MustCompile(`\d+`)
)

// groupInsights folds a status report's crawl issues into one line per rule
// with a few example pages, since a big site lists the same rule a hundred times.
func groupInsights(body string) string {
	lines := strings.Split(body, "\n")
	type group struct {
		sev, cat, rule string
		pages          []string
	}
	var order []string
	groups := map[string]*group{}
	var out []string
	in := false
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") {
			in = strings.HasPrefix(l, "## SEO insights")
		}
		m := insightLine.FindStringSubmatch(l)
		if !in || m == nil {
			out = append(out, l)
			continue
		}
		rule := digits.ReplaceAllString(m[3], "N")
		key := m[1] + m[2] + rule
		g, ok := groups[key]
		if !ok {
			g = &group{sev: m[1], cat: m[2], rule: rule}
			groups[key] = g
			order = append(order, key)
			out = append(out, "\x00"+key)
		}
		if m[4] != "" {
			g.pages = append(g.pages, m[4])
		}
	}
	rank := map[string]int{"error": 0, "warning": 1, "info": 2}
	sort.SliceStable(order, func(i, j int) bool { return rank[groups[order[i]].sev] < rank[groups[order[j]].sev] })
	var b strings.Builder
	written := false
	for _, l := range out {
		if !strings.HasPrefix(l, "\x00") {
			b.WriteString(l + "\n")
			continue
		}
		if written {
			continue
		}
		written = true
		for _, k := range order {
			g := groups[k]
			fmt.Fprintf(&b, "- [%s] [%s] %s, on %d page", g.sev, g.cat, g.rule, max(len(g.pages), 1))
			if len(g.pages) != 1 {
				b.WriteString("s")
			}
			if len(g.pages) > 0 {
				ex := g.pages[:min(3, len(g.pages))]
				fmt.Fprintf(&b, ", like `%s`", strings.Join(ex, "`, `"))
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// dashBrief turns dash's state into the parts worth reading: the three briefs,
// advisories, incidents, the systems that aren't up, and what's ahead.
func dashBrief(ctx context.Context, rd reader, section string) (string, error) {
	body, err := rd.get(ctx, "dash", "/api/state")
	if err != nil {
		return "", err
	}
	var st struct {
		Updated string `json:"updated"`
		Briefs  map[string]struct {
			Title    string `json:"title"`
			Compiled string `json:"compiled"`
			Stories  int    `json:"stories"`
			Points   []struct {
				Label string `json:"label"`
				Text  string `json:"text"`
				Gist  string `json:"gist"`
				Mark  string `json:"mark"`
				Lean  string `json:"lean"`
				Move  string `json:"move"`
			} `json:"points"`
			Read *struct {
				Label, Spread, Text string
			} `json:"read"`
			Threads []struct {
				URL      string `json:"url"`
				Read     string `json:"read"`
				Comments int    `json:"comments"`
			} `json:"threads"`
		} `json:"briefs"`
		Signal struct {
			Level, Headline string
		} `json:"signal"`
		Advisories struct {
			Rows []struct {
				ID, Product, Title, Tag, Source, Age, Detail, Status, Note string
			} `json:"rows"`
			Checked   string `json:"checked"`
			NextPatch string `json:"next_patch"`
			Partial   bool   `json:"partial"`
		} `json:"advisories"`
		Incidents []struct {
			Name, State, Text string
		} `json:"incidents"`
		Systems struct {
			Rows []struct {
				Label, Host, State, Response string
				Errors                       int
			} `json:"rows"`
			Up, Total, Errors, Requests int
		} `json:"systems"`
		Feeds []struct {
			Name, State, Age string
		} `json:"feeds"`
		Ahead []struct {
			Label, When string
		} `json:"ahead"`
		Alerts []json.RawMessage `json:"alerts"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		return "", fmt.Errorf("dash's state didn't parse: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# dash, updated %s\n", st.Updated)
	want := func(s string) bool { return section == "" || section == "all" || section == s }

	for _, name := range []string{"news", "markets", "feeds"} {
		if !want(name) {
			continue
		}
		br, ok := st.Briefs[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "\n## %s brief, %s at %s, %d stories\n\n", name, strings.ToLower(br.Title), br.Compiled, br.Stories)
		if br.Read != nil && br.Read.Text != "" {
			fmt.Fprintf(&b, "%s, %s. %s\n\n", br.Read.Label, strings.ToLower(br.Read.Spread), br.Read.Text)
		}
		for _, p := range br.Points {
			b.WriteString("- ")
			if p.Label != "" {
				b.WriteString(p.Label + ": ")
			}
			b.WriteString(p.Text)
			if p.Move != "" {
				fmt.Fprintf(&b, " (%s %s)", p.Lean, p.Move)
			}
			if p.Mark != "" {
				b.WriteString(" [" + p.Mark + "]")
			}
			if p.Gist != "" && p.Gist != p.Text {
				b.WriteString(" " + p.Gist)
			}
			b.WriteString("\n")
		}
		for i, t := range br.Threads {
			if i == 8 {
				break
			}
			fmt.Fprintf(&b, "- thread %s, %d comments: %s\n", t.URL, t.Comments, t.Read)
		}
	}
	if want("markets") && st.Signal.Headline != "" {
		fmt.Fprintf(&b, "\n## Market signal\n\n%s, %s\n", st.Signal.Level, st.Signal.Headline)
	}
	if want("advisories") {
		a := st.Advisories
		fmt.Fprintf(&b, "\n## Advisories, checked %s, next patch Tuesday %s\n\n", a.Checked, a.NextPatch)
		if a.Partial {
			b.WriteString("Some sources didn't answer, so this list may be short.\n\n")
		}
		for _, r := range a.Rows {
			fmt.Fprintf(&b, "- %s %s %s, %s from %s, %s: %s", r.Status, r.Product, r.ID, r.Tag, r.Source, strings.ToLower(r.Age), r.Title)
			if r.Detail != "" {
				b.WriteString(", " + r.Detail)
			}
			if r.Note != "" {
				b.WriteString(", " + r.Note)
			}
			b.WriteString("\n")
		}
		if len(st.Incidents) > 0 {
			b.WriteString("\n## Incidents\n\n")
			for _, in := range st.Incidents {
				fmt.Fprintf(&b, "- %s: %s, %s\n", in.Name, in.State, in.Text)
			}
		}
	}
	if want("systems") {
		s := st.Systems
		fmt.Fprintf(&b, "\n## Systems, %d of %d up, %d errors in %d requests\n\n", s.Up, s.Total, s.Errors, s.Requests)
		for _, r := range s.Rows {
			if r.State != "up" || r.Errors > 0 {
				fmt.Fprintf(&b, "- %s (%s): %s, %d errors, %s\n", r.Label, r.Host, r.State, r.Errors, r.Response)
			}
		}
		var bad []string
		for _, f := range st.Feeds {
			if f.State != "ok" {
				bad = append(bad, fmt.Sprintf("%s %s %s ago", f.Name, f.State, f.Age))
			}
		}
		if len(bad) > 0 {
			fmt.Fprintf(&b, "- feeds not ok: %s\n", strings.Join(bad, ", "))
		}
		if len(st.Alerts) > 0 {
			fmt.Fprintf(&b, "- %d weather alerts\n", len(st.Alerts))
		}
	}
	if want("markets") && len(st.Ahead) > 0 {
		b.WriteString("\n## Ahead\n\n")
		for _, a := range st.Ahead {
			fmt.Fprintf(&b, "- %s %s\n", a.Label, a.When)
		}
	}
	return b.String(), nil
}

func authActivity(ctx context.Context, rd reader, limit int) (string, error) {
	body, err := rd.get(ctx, "auth", "/activity?format=json")
	if err != nil {
		return "", err
	}
	var a struct {
		Events []struct {
			TS                                  time.Time
			Kind, IP, Country, City, UA, Detail string
		}
		Sessions []struct {
			Created, LastSeen, Expires time.Time
			IP, Country, City, UA      string
		}
		Stats map[string]int
	}
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		return "", fmt.Errorf("auth's activity didn't parse: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# auth activity\n\nLast 30 days: %d sign ins, %d codes sent, %d refused, from %d places.\n",
		a.Stats["sign_ins"], a.Stats["codes"], a.Stats["refused"], a.Stats["places"])
	fmt.Fprintf(&b, "\n## %d live sessions\n\n", len(a.Sessions))
	for _, s := range a.Sessions {
		fmt.Fprintf(&b, "- %s, %s %s %s, made %s, last seen %s, ends %s\n", s.UA, s.IP, s.City, s.Country,
			s.Created.UTC().Format("2006-01-02"), s.LastSeen.UTC().Format("2006-01-02 15:04"), s.Expires.UTC().Format("2006-01-02"))
	}
	fmt.Fprintf(&b, "\n## Newest %d events, UTC\n\n", min(limit, len(a.Events)))
	for i, e := range a.Events {
		if i == limit {
			break
		}
		fmt.Fprintf(&b, "- %s %s from %s %s %s", e.TS.UTC().Format("2006-01-02 15:04"), e.Kind, e.IP, e.City, e.Country)
		if e.Detail != "" {
			b.WriteString(", " + e.Detail)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func llmUsage(ctx context.Context, rd reader, caller string, failed bool, limit int) (string, error) {
	q := fmt.Sprintf("/calls?format=json&limit=%d", limit)
	if caller != "" {
		q += "&caller=" + url.QueryEscape(caller)
	}
	if failed {
		q += "&failed=1"
	}
	body, err := rd.get(ctx, "llm", q)
	if err != nil {
		return "", err
	}
	var u struct {
		Day struct {
			Calls, Errors int
			P50           int64 `json:"p50_ms"`
		}
		Month struct{ Spent, Budget, Projected float64 }
		Usage []struct {
			Caller        string
			Calls, Errors int
			PromptTok     int     `json:"prompt_tokens"`
			OutputTok     int     `json:"output_tokens"`
			AvgTPS        float64 `json:"avg_tps"`
		}
		Calls []struct {
			Caller, Model, Messages, Completion, Err string
			PromptTok                                int `json:"prompt_tokens"`
			OutputTok                                int `json:"output_tokens"`
			MS                                       int64
			Status                                   int
			At                                       time.Time
		}
	}
	if err := json.Unmarshal([]byte(body), &u); err != nil {
		return "", fmt.Errorf("llm's calls didn't parse: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# llm usage\n\n$%.2f of $%.0f this month, on pace for $%.2f. Today %d calls, %d failed, p50 %dms.\n",
		u.Month.Spent, u.Month.Budget, u.Month.Projected, u.Day.Calls, u.Day.Errors, u.Day.P50)
	b.WriteString("\n## Last 30 days by caller\n\n| Caller | Calls | Failed | Prompt tokens | Output tokens | Avg tok/s |\n|---|---|---|---|---|---|\n")
	for _, c := range u.Usage {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %.0f |\n", c.Caller, c.Calls, c.Errors, c.PromptTok, c.OutputTok, c.AvgTPS)
	}
	fmt.Fprintf(&b, "\n## Newest %d calls, UTC\n\n", len(u.Calls))
	for _, c := range u.Calls {
		fmt.Fprintf(&b, "- %s %s %s %d, %dms, %d in %d out", c.At.UTC().Format("01-02 15:04"), c.Caller, c.Model, c.Status, c.MS, c.PromptTok, c.OutputTok)
		if c.Err != "" {
			b.WriteString(", error: " + trim(c.Err, 300))
		}
		b.WriteString("\n")
		if failed && c.Messages != "" {
			fmt.Fprintf(&b, "  asked: %s\n", trim(c.Messages, 400))
		}
	}
	return b.String(), nil
}

func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
