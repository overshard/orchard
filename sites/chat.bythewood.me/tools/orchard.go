package tools

// The estate's own sites, read only.
//
// These are the one group of tools that reach something private, so they work
// differently from the rest. Each one forwards the session cookie of whoever is
// chatting, and the site on the other end does its own check against
// auth.bythewood.me. Nothing here holds a credential of its own, which means
// this chat cannot read anything the person using it could not already open in
// a browser, and signing that session out stops these tools on the next call.
//
// Every one is a GET against an endpoint that only reads. There is no tool here
// that can change anything, and there is deliberately not going to be one.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// SessionCookie is the name web/session.go uses, repeated here rather than
// imported because tools is a leaf package.
const SessionCookie = "bw_session"

// Site names resolve on the bridge, so these never leave the machine and never
// pass through Cloudflare. A public hostname would work and would be slower,
// cached, and a lie about where the data went.
const (
	loggingBase   = "http://orchard-logging:8000"
	statusBase    = "http://orchard-status:8000"
	analyticsBase = "http://orchard-analytics:8000"
	reposBase     = "http://orchard-repos:8000"
	dashBase      = "http://orchard-dash:8000"
)

// estateGet fetches one of the sites with the caller's session on it. It does
// not go through get(): the Guard exists for third party endpoints that rate
// limit this address, and putting a container on the bridge in the penalty box
// would take a whole site out over a blip nobody else is throttling.
func estateGet(ctx context.Context, d *Deps, rawURL string, into any) error {
	if d.Session == "" {
		return fmt.Errorf("this needs you to be signed in, and the turn carried no session")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: d.Session})
	req.Header.Set("Accept", "application/json")

	resp, err := d.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s is not answering: %w", hostOf(rawURL), err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s refused the session, so it may have been signed out", hostOf(rawURL))
	case resp.StatusCode == http.StatusSeeOther, resp.StatusCode == http.StatusFound:
		// A redirect here is the login page, which means the same thing as a
		// 401 and would otherwise be decoded as malformed JSON.
		return fmt.Errorf("%s wants a sign in", hostOf(rawURL))
	case resp.StatusCode >= 400:
		return fmt.Errorf("%s answered %d", hostOf(rawURL), resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
}

var OrchardLogs = Tool{
	Name: "orchard_logs",
	Description: "Read Isaac's own log aggregation at logging.bythewood.me: how many records and " +
		"errors each of his sites produced, the most recent error messages with their paths, and the " +
		"busiest paths with their p95 latency. Use it for anything about whether his sites are " +
		"misbehaving, what is erroring, or what is slow. Read only.",
	Schema: obj(map[string]any{
		"hours":  num("how far back to look, default 24, up to 720"),
		"errors": num("how many recent error messages to return, default 20"),
	}),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		q := url.Values{}
		if h := int(argNum(a, "hours", 0)); h > 0 {
			q.Set("hours", strconv.Itoa(h))
		}
		if e := int(argNum(a, "errors", 0)); e > 0 {
			q.Set("errors", strconv.Itoa(e))
		}
		var out any
		err := estateGet(ctx, d, loggingBase+"/api/summary?"+q.Encode(), &out)
		return out, err
	},
}

var OrchardStatus = Tool{
	Name: "orchard_status",
	Description: "Read Isaac's own uptime monitoring at status.bythewood.me: every property he " +
		"watches, whether it is up, when it was last checked, its Lighthouse scores and its crawler " +
		"state. Use it for whether a site of his is down or slow, or how it scores. Read only.",
	Schema: obj(map[string]any{}),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		var out any
		err := estateGet(ctx, d, statusBase+"/api/properties", &out)
		return out, err
	},
}

var OrchardAnalytics = Tool{
	Name: "orchard_analytics",
	Description: "Read Isaac's own analytics at analytics.bythewood.me: sessions, page views, live " +
		"users, and the top pages, referrers, countries, browsers and devices for each property. Use " +
		"it for anything about his traffic or where his visitors come from. Read only.",
	Schema: obj(map[string]any{
		"days":     num("how many days back, default 7, up to 365"),
		"property": str("one property by name, optional, otherwise every one"),
	}),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		q := url.Values{}
		if dd := int(argNum(a, "days", 0)); dd > 0 {
			q.Set("days", strconv.Itoa(dd))
		}
		if p := strings.TrimSpace(argStr(a, "property")); p != "" {
			q.Set("property", p)
		}
		var out any
		err := estateGet(ctx, d, analyticsBase+"/api/summary?"+q.Encode(), &out)
		return out, err
	},
}

var OrchardRepos = Tool{
	Name: "orchard_repos",
	Description: "Read Isaac's own git remote at repos.bythewood.me: every repository, its " +
		"description, size, branch and tag counts, when it was last pushed, and how close it is to " +
		"the push size limit. Use it for what he is working on or what a repository holds. Read only.",
	Schema: obj(map[string]any{}),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		var out any
		err := estateGet(ctx, d, reposBase+"/api/repos", &out)
		return out, err
	},
}

var OrchardDash = Tool{
	Name: "orchard_dash",
	Description: "Read Isaac's dashboard at dash.bythewood.me in one call: markets, Hacker News, " +
		"Lobsters, the weather, upcoming earnings, and whether each of his sites is answering. Use it " +
		"when a question spans several of those rather than calling each tool separately. Read only.",
	Schema: obj(map[string]any{}),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		var out any
		// dash publishes this without a session, since the page it feeds has no
		// login, so it is the one here that works signed out.
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
		err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out)
		return out, err
	},
}
