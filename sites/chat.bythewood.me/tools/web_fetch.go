package tools

import (
	"context"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

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
		txt, ok := dirListing(string(body))
		if !ok {
			txt = Text(string(body))
		}
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

var (
	listingTitle = regexp.MustCompile(`(?i)<title>\s*Index of\s+([^<]*)</title>`)
	listingRow   = regexp.MustCompile(`(?i)<a href="([^"?][^"]*)">[^<]*</a>\s+(\d{1,2}-[A-Za-z]{3}-\d{4} \d{2}:\d{2})\s+(\d+|-)`)
)

// dirListing reads a server's index page as one row per file with its size in
// gigabytes, since a model reading a twelve digit byte count as prose drops a digit.
func dirListing(h string) (string, bool) {
	title := listingTitle.FindStringSubmatch(h)
	if title == nil {
		return "", false
	}
	rows := listingRow.FindAllStringSubmatch(h, -1)
	if len(rows) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("Index of " + strings.TrimSpace(html.UnescapeString(title[1])) + "\n")
	b.WriteString("One file per line: name, last changed, size.\n")
	for _, r := range rows {
		name, _ := url.PathUnescape(r[1])
		b.WriteString(name + " | " + r[2] + " | " + humanBytes(r[3]) + "\n")
	}
	return b.String(), true
}

func humanBytes(raw string) string {
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return raw
	}
	for _, u := range []struct {
		name string
		size float64
	}{{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}} {
		if n >= u.size {
			return strconv.FormatFloat(n/u.size, 'f', 1, 64) + " " + u.name + " (" + raw + " bytes)"
		}
	}
	return raw + " bytes"
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
