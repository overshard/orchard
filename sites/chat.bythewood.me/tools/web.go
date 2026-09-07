package tools

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

var (
	ddgResult = regexp.MustCompile(`(?is)<a rel="nofollow" class="result__a" href="(.*?)".*?>(.*?)</a>.*?class="result__snippet".*?>(.*?)</a>`)
	tagStrip  = regexp.MustCompile(`(?is)<[^>]+>`)
	// RE2 has no backreferences, so a paired tag needs its own expression
	// rather than one alternation closing on \1.
	blockTags = func() []*regexp.Regexp {
		var out []*regexp.Regexp
		for _, t := range []string{"script", "style", "nav", "header", "footer", "aside", "form", "svg", "noscript", "template"} {
			out = append(out, regexp.MustCompile(`(?is)<`+t+`[^>]*>.*?</`+t+`\s*>`))
		}
		return out
	}()
	articleRe = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<article[^>]*>(.*?)</article\s*>`),
		regexp.MustCompile(`(?is)<main[^>]*>(.*?)</main\s*>`),
	}
	spaces   = regexp.MustCompile(`[ \t]+`)
	blankRun = regexp.MustCompile(`\n{3,}`)
)

// Text pulls readable prose out of an HTML page. Forty lines of standard
// library beat a readability port on real article pages when this was measured
// for search, and it keeps the byline and the date that readability throws away.
func Text(h string) string {
	for _, re := range blockTags {
		h = re.ReplaceAllString(h, " ")
	}
	// The body of an <article> or <main> is the page, and everything around
	// it is furniture. Falling through with the whole document is fine when
	// neither is present.
	for _, re := range articleRe {
		if m := re.FindStringSubmatch(h); m != nil && len(m[1]) > 200 {
			h = m[1]
			break
		}
	}
	h = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|section)>`).ReplaceAllString(h, "\n")
	h = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(h, "\n")
	h = tagStrip.ReplaceAllString(h, " ")
	h = html.UnescapeString(h)
	h = spaces.ReplaceAllString(h, " ")
	var keep []string
	for _, line := range strings.Split(h, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			keep = append(keep, l)
		}
	}
	return blankRun.ReplaceAllString(strings.Join(keep, "\n"), "\n\n")
}

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
		var hits []SearchHit
		for _, m := range ddgResult.FindAllStringSubmatch(string(body), -1) {
			href := m[1]
			// DuckDuckGo wraps results in its own redirector, and the real
			// address is the uddg parameter inside it.
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
			if len(hits) >= n {
				break
			}
		}
		if len(hits) == 0 {
			return nil, fmt.Errorf("the search engine returned nothing, which usually means it is refusing us")
		}
		return map[string]any{"query": q, "results": hits}, nil
	},
}

var WebFetch = Tool{
	Name:        "web_fetch",
	Description: "Fetch one url and return its readable text. Use it after web_search when a snippet is not enough.",
	Schema: obj(map[string]any{
		"url":       str("the full address, including https://"),
		"max_chars": integer("how much text to return, default 6000"),
	}, "url"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		u, err := publicURL(argStr(a, "url"))
		if err != nil {
			return nil, err
		}
		max := int(argNum(a, "max_chars", 12000))
		if max < 500 || max > 24000 {
			max = 12000
		}
		body, err := get(ctx, d, u, "text/html")
		if err != nil {
			return nil, err
		}
		txt := Text(string(body))
		out := map[string]any{"url": u, "text": txt, "chars": len(txt)}
		if len(txt) > max {
			out["text"] = txt[:max]
			out["truncated"] = true
			out["note"] = "This is the start of a long page and it is usually enough to answer from. " +
				"Fetching the same url again returns the same text, so do not repeat this call."
		}
		if len(strings.TrimSpace(txt)) < 400 {
			out["note"] = "This page returned very little readable text, which usually means it needs " +
				"JavaScript or is behind a wall. Fetching it again will not help. Say so, or try another source."
		}
		return out, nil
	},
}
