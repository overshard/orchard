package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Result is one hit off a search engine, before anything has been fetched.
type Result struct {
	URL     string
	Title   string
	Snippet string
}

// SearchDDG runs one query against DuckDuckGo, retrying a rate limit.
//
// 202 is how DuckDuckGo says slow down, and it clears on its own in a few
// seconds, so backing off beats failing the whole question. It gives up rather
// than hammering, and the SERP cache is what keeps this rare in normal use.
func SearchDDG(client *http.Client, query string, limit int) ([]Result, error) {
	var err error
	for attempt, wait := range []time.Duration{0, 3 * time.Second, 8 * time.Second} {
		if wait > 0 {
			time.Sleep(wait)
		}
		var out []Result
		out, err = searchOnce(client, query, limit)
		if err == nil {
			return out, nil
		}
		if !errors.Is(err, errRateLimited) {
			return nil, err
		}
		_ = attempt
	}
	return nil, err
}

var errRateLimited = errors.New("the search source is asking us to slow down, try again in a minute")

func searchOnce(client *http.Client, query string, limit int) ([]Result, error) {
	// GET rather than POST. A person reaching this page arrives by navigating
	// to it, and POST is what a scraper does.
	endpoint := "https://html.duckduckgo.com/html/?" + url.Values{
		"q": {query}, "kl": {"us-en"},
	}.Encode()
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	browserHeaders(req, "https://duckduckgo.com/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		return nil, errRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search source: %s", resp.Status)
	}

	body, err := readBody(resp, 4<<20)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	return parseDDG(doc, limit), nil
}

func parseDDG(doc *html.Node, limit int) []Result {
	var out []Result
	seen := map[string]bool{}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(out) >= limit {
			return
		}
		if n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "result") && !hasClass(n, "result--ad") {
			r := Result{}
			var scan func(*html.Node)
			scan = func(m *html.Node) {
				if m.Type == html.ElementNode && m.Data == "a" {
					switch {
					case hasClass(m, "result__a"):
						r.Title = textOf(m)
						r.URL = cleanDDGHref(attr(m, "href"))
					case hasClass(m, "result__snippet") && r.Snippet == "":
						r.Snippet = textOf(m)
					}
				}
				if m.Type == html.ElementNode && m.Data == "div" && hasClass(m, "result__snippet") && r.Snippet == "" {
					r.Snippet = textOf(m)
				}
				for c := m.FirstChild; c != nil; c = c.NextSibling {
					scan(c)
				}
			}
			scan(n)
			if r.URL != "" && !seen[r.URL] && strings.HasPrefix(r.URL, "http") {
				seen[r.URL] = true
				out = append(out, r)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// cleanDDGHref unwraps DDG's /l/?uddg= redirector, which is what the HTML
// endpoint returns rather than the destination.
func cleanDDGHref(href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if q := u.Query().Get("uddg"); q != "" {
		return q
	}
	return u.String()
}

func hasClass(n *html.Node, class string) bool {
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == class {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(m *html.Node) {
		if m.Type == html.TextNode {
			b.WriteString(m.Data)
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
