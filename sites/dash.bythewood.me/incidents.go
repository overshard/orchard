package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Open incidents at the providers the VPS leans on. Every one of these runs an
// Atlassian Statuspage, and its unresolved incidents endpoint needs no key.
const incidentsEvery = 10 * time.Minute

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
	{"ANTHROPIC", []string{"https://status.claude.com"}, regexp.MustCompile(`(?i)\bapi\b|claude code|claude\.ai|remote control|console|opus|sonnet|haiku|fable`), false, nil},
	{"GITHUB", []string{"https://www.githubstatus.com"}, regexp.MustCompile(`(?i)git operations|\bapi\b|packages|actions|advisor`), false, nil},
}

// Incident is one provider's row. State is ok, elsewhere (open incidents that
// don't touch this setup) or hit.
type Incident struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Text  string `json:"text"`
	URL   string `json:"url,omitempty"`

	id, impact string
}

type statuspageIncidents struct {
	Incidents []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Shortlink  string `json:"shortlink"`
		Impact     string `json:"impact"`
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
	} `json:"incidents"`
}

func fetchIncidents(ctx context.Context, g *Guard, prev []Incident) []Incident {
	var out []Incident
	for i, p := range providers {
		row, err := providerIncidents(ctx, g, p)
		if err != nil {
			pollFailed(ctx, "statuspage", "security", err)
			if i < len(prev) && prev[i].Name == p.label {
				row = prev[i]
			} else {
				row = Incident{Name: p.label, State: "unknown", Text: "NO ANSWER"}
			}
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
			text := inc.Name
			if !p.nameOnly {
				for _, c := range inc.Components {
					text += " " + c.Name
				}
			}
			severe := inc.Impact == "major" || inc.Impact == "critical"
			if row.State != "hit" && (p.relevant.MatchString(text) || (severe && p.broad != nil && p.broad.MatchString(text))) {
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
