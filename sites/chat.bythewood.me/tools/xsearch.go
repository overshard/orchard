package tools

// Searching X posts without an X account, an API key, or a mirror.
//
// Isaac has asked for this twice. The two obvious routes are both closed:
// xcancel has no API of its own and was served a cease and desist by X Corp on
// 24 August 2026, and Nitter is the thing xcancel is built on, needs Redis and
// a server of your own, and is under the same letter. The official Posts Search
// API is real and works and is a paid tier with a key.
//
// What is left is the search engines, which index public post pages like any
// other page. That is what this does, and it is worth being plain about the
// limits rather than presenting it as X search: only public posts are indexed,
// only some of those, and a search engine's copy can be older than the post.
// The description says so, so an answer built on it can say so too.
//
// It costs a DuckDuckGo call out of the same pool everything else uses, which
// is the point of building it here rather than reaching for another host.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

var XSearch = Tool{
	Name: "x_search",
	Description: "Find public posts on X, formerly Twitter, through a web search of x.com. " +
		"Use it when Isaac asks what someone posted or what is being said on X. " +
		"It reads what a search engine has indexed rather than X itself, so it covers public " +
		"posts only, misses plenty of them, and can hand back a copy older than the post. " +
		"Say that when the answer rests on it. There is no free X API and this is the way in.",
	Schema: obj(map[string]any{
		"query":   str("what to look for, as a person would type it"),
		"account": str("one account to search within, with or without the @, optional"),
		"n":       integer("how many posts to return, default 6"),
	}, "query"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		q := strings.TrimSpace(argStr(a, "query"))
		if q == "" {
			return nil, fmt.Errorf("query is required")
		}
		n := int(argNum(a, "n", 6))
		if n < 1 || n > 12 {
			n = 6
		}

		// Both hostnames, since old posts are indexed under twitter.com and new
		// ones under x.com, and a search for one alone misses half of them.
		scope := "(site:x.com OR site:twitter.com)"
		if acct := strings.TrimSpace(strings.TrimPrefix(argStr(a, "account"), "@")); acct != "" {
			scope = "(site:x.com/" + acct + " OR site:twitter.com/" + acct + ")"
		}

		body, err := get(ctx, d, "https://html.duckduckgo.com/html/?q="+
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
	},
}

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
