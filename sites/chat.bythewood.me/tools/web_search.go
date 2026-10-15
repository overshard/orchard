package tools

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var ddgResult = regexp.MustCompile(`(?is)<a rel="nofollow" class="result__a" href="(.*?)".*?>(.*?)</a>.*?class="result__snippet".*?>(.*?)</a>`)

type SearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

var WebSearch = Tool{
	Name: "web_search",
	Description: "Search the web and get back titles, urls and snippets. Use it for anything current, " +
		"local, priced or contested. Snippets are short, so follow up with web_fetch when you need what a page actually says.",
	Schema: obj(map[string]any{
		"query": str("what to search for, as a person would type it"),
		"n":     integer("how many results to return, default 6"),
	}, "query"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		q := argStr(a, "query")
		if q == "" {
			return nil, fmt.Errorf("query is required")
		}
		n := int(argNum(a, "n", 6))
		if n < 1 || n > 12 {
			n = 6
		}
		body, err := get(ctx, d, "https://html.duckduckgo.com/html/?q="+url.QueryEscape(q), "text/html")
		if err != nil {
			return nil, err
		}
		hits := parseDDG(string(body), n)
		if len(hits) == 0 {
			return nil, fmt.Errorf("the search engine returned nothing, which usually means it is refusing us")
		}
		return map[string]any{"query": q, "results": hits}, nil
	},
}

// parseDDG pulls the results out of a DuckDuckGo HTML page. A limit of zero
// means every one, for a caller filtering them itself.
func parseDDG(body string, limit int) []SearchHit {
	var hits []SearchHit
	for _, m := range ddgResult.FindAllStringSubmatch(body, -1) {
		href := m[1]
		// DuckDuckGo wraps results in its own redirector, and the real address
		// is the uddg parameter inside it.
		if strings.HasPrefix(href, "//duckduckgo.com/l/") {
			if u, e := url.Parse("https:" + href); e == nil {
				if real := u.Query().Get("uddg"); real != "" {
					href = real
				}
			}
		}
		hits = append(hits, SearchHit{
			Title:   strings.TrimSpace(Text(m[2])),
			URL:     href,
			Snippet: strings.TrimSpace(Text(m[3])),
		})
		if limit > 0 && len(hits) >= limit {
			break
		}
	}
	return hits
}
