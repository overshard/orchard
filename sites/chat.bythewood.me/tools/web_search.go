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

type webSearchArgs struct {
	Query string
	N     int
}

func (a *webSearchArgs) params() []param {
	return []param{
		strArg(&a.Query, "query", "what to search for, as a person would type it").required(),
		intArg(&a.N, "n", "how many results to return, default 6").otherwise(6),
	}
}

var WebSearch = Define(Tool{
	Name:   "web_search",
	Traits: Untrusted | OpenWeb | Lookup,
	Cites:  searchHits,
	Description: "Search the web and get back titles, urls and snippets. Use it for anything current, " +
		"local, priced or contested. Snippets are short, so follow up with web_fetch when you need what a page actually says.",
}, func(ctx context.Context, d *Deps, a webSearchArgs) (any, error) {
	q := a.Query
	if q == "" {
		return nil, fmt.Errorf("query is required")
	}
	n := a.N
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
})

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

func searchHits(m map[string]any) []Cited {
	hits, _ := m["results"].([]SearchHit)
	out := make([]Cited, 0, len(hits))
	for _, h := range hits {
		out = append(out, Cited{URL: h.URL, Title: h.Title, Text: h.Title + " " + h.Snippet})
	}
	return out
}
