package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type orchardDashArgs struct {
	Section string
}

func (a *orchardDashArgs) params() []param {
	return []param{
		strArg(&a.Section, "section", "one panel, such as earnings, market, sectors or weather. Call once with it empty to see the names"),
	}
}

var OrchardDash = Define(Tool{
	Name:   "orchard_dash",
	Traits: Private | Estate,
	Description: "Read Isaac's dashboard at dash.bythewood.me: markets, Hacker News, Lobsters, the " +
		"weather, earnings, and whether each of his sites is answering. Section onair is whether TheBurntPeanut " +
		"is streaming, when he usually goes live, his last fourteen days and his next go live, and it is the " +
		"answer to any question about when he streams. Section earnings has the reports coming up and just out for " +
		"the hundred biggest S&P companies with the consensus estimate, and is the first place to look when he asks " +
		"when one reports or what it is expected to earn. Pass section to get one " +
		"panel, which is almost always what a question wants, and leave it empty only when the " +
		"question really does span most of the dashboard. Read only.",
}, func(ctx context.Context, d *Turn, a orchardDashArgs) (any, error) {
	var out any
	// dash publishes this without a session, since the page it feeds has no
	// login, so this one works signed out.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dashBase+"/api/state", nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dash is not answering: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("dash answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return dashSection(out, a.Section)
})

// dashSection picks one panel out of a state that's about six thousand tokens of
// a sixty four thousand token window. A name that isn't there returns the list
// instead of an error, since the names are the state's own keys.
func dashSection(state any, section string) (any, error) {
	section = strings.ToLower(strings.TrimSpace(section))
	m, ok := state.(map[string]any)
	if section == "" || !ok {
		return state, nil
	}
	// "markets" for the market panel, or "alert" for alerts.
	for _, name := range []string{section, strings.TrimSuffix(section, "s"), section + "s"} {
		if v, ok := m[name]; ok {
			return map[string]any{"section": name, name: v}, nil
		}
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("dash has no %q panel. The panels are: %s",
		section, strings.Join(names, ", "))
}
