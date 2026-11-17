package tools

import (
	"context"
	"fmt"
	"strings"
)

type SearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type webSearchArgs struct {
	Query string
	Site  string
	N     int
}

func (a *webSearchArgs) params() []param {
	return []param{
		strArg(&a.Query, "query", "what to search for, as a person would type it").required(),
		strArg(&a.Site, "site", "one domain to search within, like x.com for posts on X, optional"),
		intArg(&a.N, "n", "how many results to return, default 6").otherwise(6),
	}
}

var WebSearch = Define(Tool{
	Name:   "web_search",
	Traits: Untrusted | OpenWeb | Outbound,
	Cites:  searchHits,
	Description: "Search the web and get back titles, urls and snippets. Use it for anything current, " +
		"local, priced or contested. Snippets are short, so follow up with web_fetch when you need what a page actually says.",
}, func(ctx context.Context, d *Turn, a webSearchArgs) (any, error) {
	q := a.Query
	if q == "" {
		return nil, fmt.Errorf("query is required")
	}
	n := a.N
	if n < 1 || n > 12 {
		n = 6
	}
	var domains []string
	if site := strings.TrimSpace(a.Site); site != "" {
		domains = []string{site}
	}
	hits, err := search(ctx, d, q, domains, n)
	if err != nil {
		return nil, err
	}
	return map[string]any{"query": q, "results": hits}, nil
})

// search asks the gateway, which gets its results from Anthropic's web search.
func search(ctx context.Context, d *Turn, q string, domains []string, n int) ([]SearchHit, error) {
	if d.Search == nil {
		return nil, fmt.Errorf("no search behind the gateway")
	}
	hits, err := d.Search.Search(ctx, q, domains, n)
	if err == nil && len(hits) == 0 {
		err = fmt.Errorf("the search found nothing")
	}
	return hits, err
}

func searchHits(m map[string]any) []Cited {
	hits, _ := m["results"].([]SearchHit)
	out := make([]Cited, 0, len(hits))
	for _, h := range hits {
		out = append(out, Cited{URL: h.URL, Title: h.Title, Text: h.Title + " " + h.Snippet})
	}
	return out
}
