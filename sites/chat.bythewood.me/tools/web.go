package tools

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
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
		hits := parseDDG(string(body), n)
		if len(hits) == 0 {
			return nil, fmt.Errorf("the search engine returned nothing, which usually means it is refusing us")
		}
		return map[string]any{"query": q, "results": hits}, nil
	},
}

// parseDDG pulls the results out of a DuckDuckGo HTML page. A limit of zero
// means every one, which is what a caller filtering them itself needs.
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

var WebFetch = Tool{
	Name:        "web_fetch",
	Description: "Fetch one url and return its readable text. Use it after web_search when a snippet is not enough.",
	Schema: obj(map[string]any{
		"url":       str("the full address, including https://"),
		"max_chars": integer("how much text to return, default 6000"),
		"find":      str("optional, a heading or phrase to start reading from, for a long page whose answer is further down"),
	}, "url"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		u, err := publicURL(argStr(a, "url"))
		if err != nil {
			return nil, err
		}
		limit := int(argNum(a, "max_chars", 12000))
		if limit < 500 || limit > 24000 {
			limit = 12000
		}
		body, err := get(ctx, d, u, "text/html")
		if err != nil {
			return nil, err
		}
		txt := Text(string(body))
		out := map[string]any{"url": u, "text": txt, "chars": len(txt)}
		part, cut, note := window(txt, strings.TrimSpace(argStr(a, "find")), limit)
		out["text"] = part
		if cut {
			out["truncated"] = true
			note = firstNonEmpty(note, "This is part of a long page and it is usually enough to answer from. To read "+
				"further down, call again with find set to a heading or phrase from the part you need.")
		}
		if note != "" {
			out["note"] = note
		}
		if len(strings.TrimSpace(txt)) < 400 {
			out["note"] = "This page returned very little readable text, which usually means it needs " +
				"JavaScript or is behind a wall. Fetching it again will not help. Say so, or try another source."
		}
		return out, nil
	},
}

// window is the part of a page a fetch returns: from a little before find when
// it is there, and at most limit bytes, cut on a rune boundary.
func window(txt, find string, limit int) (part string, cut bool, note string) {
	from := 0
	if find != "" {
		lower, f := strings.ToLower(txt), strings.ToLower(find)
		i := strings.Index(lower, f)
		// The first mention near the top is usually a table of contents, and
		// the section itself is the next one.
		if i >= 0 && i < len(txt)/6 {
			if next := strings.Index(lower[i+len(f):], f); next >= 0 {
				i += len(f) + next
			}
		}
		if i < 0 {
			note = "The page has no " + strconv.Quote(find) + " in it, so this is the start of it."
		} else {
			// A little before the phrase, so a heading keeps the line above it.
			// Lowercasing can change a rune's width, hence the clamp.
			from = min(max(0, i-min(300, limit/10)), len(txt))
			for from > 0 && from < len(txt) && !utf8.RuneStart(txt[from]) {
				from--
			}
		}
	}
	if len(txt)-from <= limit {
		return txt[from:], false, note
	}
	end := from + limit
	for end > from && !utf8.RuneStart(txt[end]) {
		end--
	}
	return txt[from:end], true, note
}
