package tools

// X is searched through a search engine's copy of public post pages, since xcancel
// and Nitter are under a cease and desist from X Corp and the official search API
// is a paid tier. That copy misses plenty and can be older than the post.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type xSearchArgs struct {
	Query   string
	Account string
	N       int
}

func (a *xSearchArgs) params() []param {
	return []param{
		strArg(&a.Query, "query", "what to look for, as a person would type it").required(),
		strArg(&a.Account, "account", "one account to search within, with or without the @, optional"),
		intArg(&a.N, "n", "how many posts to return, default 6").otherwise(6),
	}
}

var XSearch = Define(Tool{
	Name:   "x_search",
	Traits: Untrusted,
	Description: "Find public posts on X, formerly Twitter, through a web search of x.com. " +
		"Use it when Isaac asks what someone posted or what is being said on X. " +
		"It reads what a search engine has indexed rather than X itself, so it covers public " +
		"posts only, misses plenty of them, and can hand back a copy older than the post. " +
		"Say that when the answer rests on it. There is no free X API and this is the way in.",
}, func(ctx context.Context, d *Turn, a xSearchArgs) (any, error) {
	q := a.Query
	if q == "" {
		return nil, fmt.Errorf("query is required")
	}
	n := a.N
	if n < 1 || n > 12 {
		n = 6
	}

	// Both hostnames, since old posts are indexed under twitter.com and new
	// ones under x.com, and a search for one alone misses half of them.
	scope := "(site:x.com OR site:twitter.com)"
	if acct := strings.TrimSpace(strings.TrimPrefix(a.Account, "@")); acct != "" {
		scope = "(site:x.com/" + acct + " OR site:twitter.com/" + acct + ")"
	}

	body, err := get(ctx, d.Deps, "https://html.duckduckgo.com/html/?q="+
		url.QueryEscape(scope+" "+q), "text/html")
	if err != nil {
		return nil, err
	}
	hits := parseDDG(string(body), 0)

	posts := make([]SearchHit, 0, n)
	for _, h := range hits {
		if !isXPost(h.URL) {
			continue
		}
		h.URL = asXCom(h.URL)
		posts = append(posts, h)
		if len(posts) >= n {
			break
		}
	}
	if len(posts) == 0 {
		return nil, fmt.Errorf("no indexed posts matched that. Search engines carry only part " +
			"of X and there is no free API, so say that rather than that nothing was posted")
	}
	return map[string]any{
		"query": q, "posts": posts, "count": len(posts),
		"note": "These come from a search engine's index of x.com, not from X. It holds public " +
			"posts only and not all of them, and a snippet can be older than the post. Do not " +
			"present this as a complete or current picture of what is on X.",
	}, nil
})

// isXPost keeps the status pages and drops the profile, hashtag and help pages
// a site search also returns.
func isXPost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	if host != "x.com" && host != "twitter.com" && host != "mobile.twitter.com" {
		return false
	}
	return strings.Contains(u.Path, "/status/")
}

// asXCom rewrites a twitter.com address to the hostname that still resolves, so
// every link in one answer goes to the same place.
func asXCom(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch strings.TrimPrefix(strings.ToLower(u.Host), "www.") {
	case "twitter.com", "mobile.twitter.com":
		u.Host = "x.com"
	}
	return u.String()
}
