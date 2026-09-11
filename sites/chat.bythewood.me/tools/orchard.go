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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// SessionCookie is the name web/session.go uses, repeated here rather than
// imported because tools is a leaf package.
const SessionCookie = "bw_session"

// Site names resolve on the bridge, so these never leave the machine and never
// pass through Cloudflare. A public hostname would work and would be slower,
// cached, and a lie about where the data went.
// Vars rather than constants so a test can stand a real server up in front of
// one, which is the only way to check that a tool walks a repository correctly.
var (
	loggingBase   = "http://orchard-logging:8000"
	statusBase    = "http://orchard-status:8000"
	analyticsBase = "http://orchard-analytics:8000"
	reposBase     = "http://orchard-repos:8000"
	dashBase      = "http://orchard-dash:8000"
)

// errEstateMissing is a 404 from one of the sites, which for a tool walking a
// repository is a path that is not there rather than a failure.
var errEstateMissing = fmt.Errorf("no such path")

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
	case resp.StatusCode == http.StatusNotFound:
		return errEstateMissing
	case resp.StatusCode >= 400:
		return fmt.Errorf("%s answered %d", hostOf(rawURL), resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
}

var OrchardLogs = Tool{
	Name: "orchard_logs",
	Description: "Read Isaac's own log aggregation at logging.bythewood.me: how many records and " +
		"errors each of his sites produced, every kind of error with the reason it gives, and the " +
		"busiest paths with their p95 latency. Use it for anything about whether his sites are " +
		"misbehaving, what is erroring, or what is slow. Each error comes back grouped, so count " +
		"and first_seen say how big it is and how long it has run, and the details field carries " +
		"the reason, which is where the actual cause is rather than in the message. Call it a " +
		"second time with source or contains to narrow down on one thing. Read only.",
	Schema: obj(map[string]any{
		"hours":    num("how far back to look, default 24, up to 720"),
		"errors":   num("how many kinds of error to return, default 20"),
		"source":   str("one site by name, optional, such as search or repos or chat"),
		"contains": str("only errors whose message, reason or path contains this, optional"),
	}),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		q := url.Values{}
		if h := int(argNum(a, "hours", 0)); h > 0 {
			q.Set("hours", strconv.Itoa(h))
		}
		if e := int(argNum(a, "errors", 0)); e > 0 {
			q.Set("errors", strconv.Itoa(e))
		}
		if v := strings.TrimSpace(argStr(a, "source")); v != "" {
			q.Set("source", v)
		}
		if v := strings.TrimSpace(argStr(a, "contains")); v != "" {
			q.Set("contains", v)
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

// OrchardCode is the other half of orchard_repos: the listing says what exists
// and this says what is in it. Without it every question about Isaac's own code
// ended the same way, with the model guessing raw addresses on repos and
// github, collecting 404s, and eventually writing a file it said it had read.
// Nine of the seventeen failed fetches on 2026-09-08 were that.
//
// Listing and reading alone were not enough either. Asked on 2026-09-10 why dash
// was not showing Oracle, it listed the repository, listed sites, guessed
// "dash.bythewood.me" without the prefix, ran out of rounds on the error and
// answered with a guess that was wrong. Four rounds of walking never reached a
// line of code. find and search are there so the first call lands on the file.
var OrchardCode = Tool{
	Name: "orchard_code",
	Description: "Read the source of one of Isaac's repositories on repos.bythewood.me, read only, " +
		"and it can never change anything. Four actions: find locates a file by name anywhere in " +
		"the repository, search finds a string inside the files and gives the path and line of each " +
		"hit, read returns one file, list shows one directory. Start with find or search rather " +
		"than walking down from the top, since a name or a symbol gets there in one call. On a long " +
		"file read the lines around a hit with from and to instead of the whole thing. Never guess " +
		"a url for his code and never fetch one, this is the only way in.",
	Schema: obj(map[string]any{
		"repo": str("the repository name, as orchard_repos lists it"),
		"action": map[string]any{
			"type":        "string",
			"enum":        []string{"find", "search", "read", "list"},
			"description": "find a file by name, search file contents, read a file, or list a directory",
		},
		"query": str("for find, part of a file name such as earnings.go; for search, the exact text to look for such as a function name"),
		"path":  str("for read and list, the full path; for find and search, an optional directory to stay inside"),
		"from":  integer("for read, the first line to return, optional"),
		"to":    integer("for read, the last line to return, optional"),
		"rev":   str("a branch, tag or commit, optional, defaults to the default branch"),
	}, "repo"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		repo := strings.Trim(strings.TrimSpace(argStr(a, "repo")), "/")
		if repo == "" {
			return nil, fmt.Errorf("repo is required, and orchard_repos lists the names")
		}
		rev := strings.TrimSpace(argStr(a, "rev"))
		if rev == "" {
			rev = "HEAD"
		}
		path := strings.Trim(strings.TrimSpace(argStr(a, "path")), "/")
		query := strings.TrimSpace(argStr(a, "query"))
		base := reposBase + "/api/repos/" + url.PathEscape(repo)

		// The action is what the model said it wants, but a call naming one
		// thing and asking for another is common enough that the arguments
		// decide when they disagree. A query with no action is a search.
		switch action := strings.ToLower(strings.TrimSpace(argStr(a, "action"))); {
		case action == "find":
			q := url.Values{}
			q.Set("q", query)
			var out map[string]any
			err := estateGet(ctx, d, base+"/find/"+url.PathEscape(rev)+"?"+q.Encode(), &out)
			return out, err

		case action == "search" || (action == "" && query != ""):
			if query == "" {
				return nil, fmt.Errorf("search needs a query, which is the text to look for")
			}
			q := url.Values{}
			q.Set("q", query)
			if path != "" {
				q.Set("path", path)
			}
			var out map[string]any
			err := estateGet(ctx, d, base+"/grep/"+url.PathEscape(rev)+"?"+q.Encode(), &out)
			return out, err
		}

		return orchardWalk(ctx, d, base, repo, rev, path, a)
	},
}

// orchardWalk is read and list, which are one call because a path with no dot in
// its last segment is a directory far more often than not and guessing wrong
// either way costs a round. The file read is tried first and a miss falls
// through to the listing, so an extensionless file still reads and a directory
// still lists.
func orchardWalk(ctx context.Context, d *Deps, base, repo, rev, path string, a map[string]any) (any, error) {
	if path != "" {
		q := url.Values{}
		if from := int(argNum(a, "from", 0)); from > 0 {
			q.Set("from", strconv.Itoa(from))
		}
		if to := int(argNum(a, "to", 0)); to > 0 {
			q.Set("to", strconv.Itoa(to))
		}
		fileURL := base + "/file/" + url.PathEscape(rev) + "/" + escapePath(path)
		if len(q) > 0 {
			fileURL += "?" + q.Encode()
		}
		var file map[string]any
		err := estateGet(ctx, d, fileURL, &file)
		if err == nil {
			return file, nil
		}
		// Only a missing file falls through. A refused session or a binary
		// file is the answer, and listing the directory would bury it.
		if !errors.Is(err, errEstateMissing) {
			return nil, err
		}
	}
	treeURL := base + "/tree/" + url.PathEscape(rev)
	if path != "" {
		treeURL += "/" + escapePath(path)
	}
	var tree map[string]any
	if err := estateGet(ctx, d, treeURL, &tree); err != nil {
		if path == "" {
			return nil, err
		}
		// A wrong path used to end the turn. It is the commonest mistake there
		// is here, the model drops a directory from the front, so the error
		// carries the way out rather than the level above.
		return nil, fmt.Errorf("%s has no file or directory at %q. Call again with "+
			"action find and query %q to get its real path", repo, path, lastSegment(path))
	}
	return tree, nil
}

// lastSegment is what to hand find when a path was wrong, since the file name is
// the part the model usually has right.
func lastSegment(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 && i+1 < len(p) {
		return p[i+1:]
	}
	return p
}

// escapePath escapes each segment and keeps the separators, since the whole
// path is one wildcard on the other side and escaping it whole would turn every
// slash into %2F.
func escapePath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

var OrchardDash = Tool{
	Name: "orchard_dash",
	Description: "Read Isaac's dashboard at dash.bythewood.me: markets, Hacker News, Lobsters, the " +
		"weather, earnings, and whether each of his sites is answering. Pass section to get one " +
		"panel, which is almost always what a question wants, and leave it empty only when the " +
		"question really does span most of the dashboard. Read only.",
	Schema: obj(map[string]any{
		"section": str("one panel, such as earnings, markets or weather. Call once with it empty to see the names"),
	}),
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
		if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
			return nil, err
		}
		return dashSection(out, argStr(a, "section"))
	},
}

// dashSection picks one panel out of the state. The whole thing is about six
// thousand tokens of a sixty four thousand token window, and a question about
// earnings was paying for the air quality, the alerts, the store listings and
// everything else to sit in the context for the rest of the conversation.
//
// A name that is not there returns the list rather than an error, since the
// names are the state's own keys and nothing here should have to keep a copy of
// them in step.
func dashSection(state any, section string) (any, error) {
	section = strings.ToLower(strings.TrimSpace(section))
	m, ok := state.(map[string]any)
	if section == "" || !ok {
		return state, nil
	}
	if v, ok := m[section]; ok {
		return map[string]any{"section": section, section: v}, nil
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("dash has no %q panel. The panels are: %s",
		section, strings.Join(names, ", "))
}
