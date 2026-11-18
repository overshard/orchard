package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Open incidents at the providers the VPS leans on. Every one of these runs an
// Atlassian Statuspage, and its unresolved incidents endpoint needs no key.
const (
	incidentsEvery = 10 * time.Minute

	// The history is a few hundred kilobytes a page, so it's read hourly while
	// the open incidents, a few hundred bytes, are read every ten minutes.
	historyEvery = time.Hour

	// Cloudflare's last fifty incidents reach back about this far.
	historyDays = 14
)

type provider struct {
	label string
	pages []string

	// An incident only counts as touching this setup when its name or one of
	// its components matches, since Cloudflare alone has several open most days.
	relevant *regexp.Regexp

	// OVH tags a global incident with every region's component, so only its
	// name says where it is.
	nameOnly bool

	// broad matches an incident across a whole country's network, which only
	// counts when the provider rates it major or worse.
	broad *regexp.Regexp
}

var providers = []provider{
	{"OVH VINT HILL", []string{"https://public-cloud.status-ovhcloud.com", "https://network.status-ovhcloud.com"}, regexp.MustCompile(`(?i)US-EAST-VA|vint hill|\bVIN\d?\b|\bVPS\b`), true, regexp.MustCompile(`\[US\]`)},
	{"CLOUDFLARE", []string{"https://www.cloudflarestatus.com"}, regexp.MustCompile(`(?i)tunnel|\bdns\b|cloudflared|zero trust|\baccess\b|ashburn|\bIAD\b`), false, nil},
	{"ANTHROPIC", []string{"https://status.claude.com"}, regexp.MustCompile(`(?i)\bapi\b|claude code|console|opus|sonnet|haiku|fable`), false, nil},
	{"GITHUB", []string{"https://www.githubstatus.com"}, regexp.MustCompile(`(?i)git operations|\bapi\b|packages|actions|advisor`), false, nil},
}

// Incident is one provider's row. State is ok, elsewhere (open incidents that
// don't touch this setup) or hit.
type Incident struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Text  string `json:"text"`
	URL   string `json:"url,omitempty"`

	// One per day, oldest first and today last: 0 quiet, 1 an incident
	// elsewhere at the provider, 2 one that touched this setup.
	Days []int `json:"days"`

	id, impact string
}

type statuspageIncident struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Shortlink  string     `json:"shortlink"`
	Impact     string     `json:"impact"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	Components []struct {
		Name string `json:"name"`
	} `json:"components"`
}

type statuspageIncidents struct {
	Incidents []statuspageIncident `json:"incidents"`
}

// touches says whether an incident reaches this setup.
func (p provider) touches(inc statuspageIncident) bool {
	text := inc.Name
	if !p.nameOnly {
		for _, c := range inc.Components {
			text += " " + c.Name
		}
	}
	severe := inc.Impact == "major" || inc.Impact == "critical"
	return p.relevant.MatchString(text) || (severe && p.broad != nil && p.broad.MatchString(text))
}

// historyDaysFor lays a provider's recent incidents over the last historyDays
// New York days, each day keeping the worst thing that happened on it.
func historyDaysFor(p provider, incidents []statuspageIncident, now time.Time) []int {
	loc := easternTime()
	today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	first := today.AddDate(0, 0, -(historyDays - 1))
	days := make([]int, historyDays)
	for _, inc := range incidents {
		end := now
		if inc.ResolvedAt != nil {
			end = *inc.ResolvedAt
		}
		level := 1
		if p.touches(inc) {
			level = 2
		}
		for i := range days {
			start := first.AddDate(0, 0, i)
			if inc.CreatedAt.Before(start.AddDate(0, 0, 1)) && !end.Before(start) && level > days[i] {
				days[i] = level
			}
		}
	}
	return days
}

var historyCache = struct {
	sync.Mutex
	at   map[string]time.Time
	days map[string][]int
}{at: map[string]time.Time{}, days: map[string][]int{}}

// providerHistory is read at most hourly, and a failed read keeps the last one.
func providerHistory(ctx context.Context, g *Guard, p provider, now time.Time) []int {
	historyCache.Lock()
	at, days := historyCache.at[p.label], historyCache.days[p.label]
	historyCache.Unlock()
	if days != nil && now.Sub(at) < historyEvery {
		return days
	}
	var all []statuspageIncident
	for _, page := range p.pages {
		var s statuspageIncidents
		if err := getJSONHeaders(ctx, g, "statushistory", page+"/api/v2/incidents.json", politeAgent, &s); err != nil {
			pollFailed(ctx, "status history", "security", err)
			return days
		}
		all = append(all, s.Incidents...)
	}
	days = historyDaysFor(p, all, now)
	historyCache.Lock()
	historyCache.at[p.label], historyCache.days[p.label] = now, days
	historyCache.Unlock()
	return days
}

func fetchIncidents(ctx context.Context, g *Guard, prev []Incident) []Incident {
	var out []Incident
	for i, p := range providers {
		row, err := providerIncidents(ctx, g, p)
		row.Days = providerHistory(ctx, g, p, time.Now())
		if err != nil {
			pollFailed(ctx, "statuspage", "security", err)
			days := row.Days
			if i < len(prev) && prev[i].Name == p.label {
				row = prev[i]
			} else {
				row = Incident{Name: p.label, State: "unknown", Text: "NO ANSWER"}
			}
			row.Days = days
		}
		out = append(out, row)
	}
	return out
}

func providerIncidents(ctx context.Context, g *Guard, p provider) (Incident, error) {
	row := Incident{Name: p.label, State: "ok", Text: "CLEAR"}
	open := 0
	for _, page := range p.pages {
		var s statuspageIncidents
		if err := getJSONHeaders(ctx, g, "statuspage", page+"/api/v2/incidents/unresolved.json", politeAgent, &s); err != nil {
			return row, err
		}
		for _, inc := range s.Incidents {
			open++
			if row.State != "hit" && p.touches(inc) {
				row.State, row.Text, row.URL, row.id, row.impact = "hit", strings.ToUpper(inc.Name), inc.Shortlink, inc.ID, inc.Impact
			}
		}
	}
	if row.State != "hit" && open > 0 {
		row.State = "elsewhere"
		row.Text = fmt.Sprintf("%d OPEN, NOT %s", open, untouched[p.label])
	}
	return row, nil
}

// What an elsewhere row says this setup's part of the provider is, in words.
var untouched = map[string]string{
	"OVH VINT HILL": "VINT HILL",
	"CLOUDFLARE":    "TUNNEL OR DNS",
	"ANTHROPIC":     "CODE OR MODELS",
	"GITHUB":        "GIT OR THE API",
}

func incidentNotices(rows []Incident) []Notice {
	var out []Notice
	for _, r := range rows {
		// Providers post minor notices for weeks at a time, so the row shows
		// them and only a major outage is worth a notification.
		if r.State != "hit" || r.id == "" || (r.impact != "major" && r.impact != "critical") {
			continue
		}
		out = append(out, Notice{
			ID:    "incident:" + r.id,
			Kind:  "security",
			Title: r.Name + " incident",
			Body:  r.Text,
			URL:   r.URL,
		})
	}
	return out
}
