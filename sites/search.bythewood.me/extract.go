package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Page is a fetched and cleaned document, ready to chunk into passages.
type Page struct {
	URL       string
	Title     string
	Site      string
	Published string // ISO date, empty when the page carries none
	Markdown  string
	Links     []Link
}

// Link is an outbound link found in a page's article body, with the words that
// were linked.
//
// These are harvested rather than searched for. A page that answers "the most
// popular Go project" almost always links to the project it names, so the
// canonical URL is already in hand and costs no extra search.
type Link struct {
	URL  string
	Text string
}

// chrome are the elements that never carry article text. Dropping them and then
// taking the semantic content element beats a full-page conversion by enough to
// matter, and it measures level with a readability port on real article pages.
var chrome = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Nav: true, atom.Header: true,
	atom.Footer: true, atom.Aside: true, atom.Form: true, atom.Iframe: true,
	atom.Noscript: true, atom.Svg: true, atom.Button: true, atom.Select: true,
	atom.Textarea: true, atom.Dialog: true, atom.Template: true,
}

// Fetch pulls a URL and reduces it to markdown plus whatever metadata the page
// was willing to state about itself.
func Fetch(client *http.Client, target string) (*Page, error) {
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return nil, err
	}
	browserHeaders(req, "")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", target, resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "html") {
		return nil, fmt.Errorf("%s: not html (%s)", target, ct)
	}

	body, err := readBody(resp, 8<<20)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	p := &Page{URL: target}
	p.Title = metaTitle(doc)
	p.Site = metaContent(doc, "og:site_name")
	if t, ok := pubDate(doc); ok {
		p.Published = t.Format("2006-01-02")
	}

	// Before stripAndPick, which removes script tags in place and takes the
	// structured data with them.
	recipe, hasRecipe := recipeFromJSONLD(doc)

	content := stripAndPick(doc)
	p.Links = harvestLinks(content, target)
	md, err := htmltomarkdown.ConvertString(renderNode(content))
	if err != nil {
		return nil, err
	}
	p.Markdown = tidyMarkdown(md)
	if len(strings.Fields(p.Markdown)) < 40 {
		return nil, fmt.Errorf("%s: too little text", target)
	}
	// A recipe page publishes its ingredients with quantities as data and then
	// describes them without quantities in the prose, so the structured copy
	// goes first where the passage ranking will find it.
	if hasRecipe {
		p.Markdown = recipe.Markdown() + "\n\n" + p.Markdown
	}
	return p, nil
}

// stripAndPick removes page chrome, then returns the semantic content element
// if there is one. Without a readability port this is what separates an article
// from the navigation around it, and on real article pages it measures level
// with one.
func stripAndPick(root *html.Node) *html.Node {
	var prune func(*html.Node)
	prune = func(n *html.Node) {
		var next *html.Node
		for c := n.FirstChild; c != nil; c = next {
			next = c.NextSibling
			if c.Type == html.ElementNode && chrome[c.DataAtom] {
				n.RemoveChild(c)
				continue
			}
			if c.Type == html.CommentNode {
				n.RemoveChild(c)
				continue
			}
			prune(c)
		}
	}
	prune(root)

	var best *html.Node
	bestLen := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.DataAtom == atom.Article || n.DataAtom == atom.Main) {
			if l := textLen(n); l > bestLen {
				best, bestLen = n, l
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if best != nil && bestLen > 400 {
		return best
	}
	return root
}

// harvestLinks pulls the outbound links out of an article body. Same-host links
// are dropped because they are navigation, and so is anything whose anchor text
// is too short or too long to name a thing.
func harvestLinks(root *html.Node, pageURL string) []Link {
	base, err := neturl.Parse(pageURL)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Link

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			href := ""
			for _, a := range n.Attr {
				if a.Key == "href" {
					href = a.Val
				}
			}
			if u, err := neturl.Parse(href); err == nil && href != "" {
				abs := base.ResolveReference(u)
				text := strings.Join(strings.Fields(textContent(n)), " ")
				if keepLink(abs, base, text) && !seen[abs.String()] {
					seen[abs.String()] = true
					out = append(out, Link{URL: abs.String(), Text: text})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func keepLink(u, base *neturl.URL, text string) bool {
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" || sameSite(u.Host, base.Host) {
		return false
	}
	if len(text) < 2 || len(text) > 80 {
		return false
	}
	// Sharing widgets and the sites every article links to regardless of what
	// it is about.
	for _, junk := range []string{
		"facebook.com", "twitter.com", "x.com", "instagram.com", "pinterest.com",
		"linkedin.com", "reddit.com/submit", "t.me/share", "whatsapp.com",
		"doubleclick", "googletagmanager", "amazon-adsystem",
	} {
		if strings.Contains(u.Host+u.Path, junk) {
			return false
		}
	}
	return true
}

func sameSite(a, b string) bool {
	return registrable(a) == registrable(b)
}

// registrable is a rough eTLD+1. It only has to tell one publisher from
// another, so the two label rule is enough and a public suffix list would be a
// dependency for nothing.
func registrable(host string) string {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textContent(c))
	}
	return b.String()
}

func textLen(n *html.Node) int {
	if n.Type == html.TextNode {
		return len(strings.TrimSpace(n.Data))
	}
	total := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		total += textLen(c)
	}
	return total
}

func renderNode(n *html.Node) string {
	var b bytes.Buffer
	html.Render(&b, n)
	return b.String()
}

// tidyMarkdown drops the wreckage a conversion leaves behind: image lines,
// tracking parameters, and the runs of blank lines that come from stripped
// elements.
func tidyMarkdown(md string) string {
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			blanks++
			if blanks > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blanks = 0
		// A line that is nothing but images carries no text for the model.
		if strings.HasPrefix(t, "![") && !strings.Contains(t, "](#") && len(stripImages(t)) < 3 {
			continue
		}
		out = append(out, stripTracking(l))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func stripImages(s string) string {
	for {
		i := strings.Index(s, "![")
		if i < 0 {
			return strings.TrimSpace(s)
		}
		j := strings.Index(s[i:], ")")
		if j < 0 {
			return strings.TrimSpace(s[:i])
		}
		s = s[:i] + s[i+j+1:]
	}
}

var trackingParams = []string{"utm_source", "utm_campaign", "utm_medium", "utm_content", "utm_term"}

func stripTracking(s string) string {
	for _, p := range trackingParams {
		for {
			i := strings.Index(s, "?"+p+"=")
			if i < 0 {
				i = strings.Index(s, "&"+p+"=")
			}
			if i < 0 {
				break
			}
			end := i + 1
			for end < len(s) && s[end] != '&' && s[end] != ')' && s[end] != ' ' && s[end] != '"' {
				end++
			}
			s = s[:i] + s[end:]
		}
	}
	return s
}

// ---- metadata ----

func metaTitle(root *html.Node) string {
	if v := metaContent(root, "og:title"); v != "" {
		return v
	}
	var out string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if out != "" {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Title && n.FirstChild != nil {
			out = strings.Join(strings.Fields(n.FirstChild.Data), " ")
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func metaContent(root *html.Node, key string) string {
	var out string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if out != "" {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Meta {
			var k, v string
			for _, a := range n.Attr {
				switch a.Key {
				case "property", "name", "itemprop":
					k = strings.ToLower(a.Val)
				case "content":
					v = a.Val
				}
			}
			if k == strings.ToLower(key) && v != "" {
				out = v
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

// pubDate reads a publication date out of structured metadata. It finds one on
// roughly half of real pages; the rest have it only as prose in the byline,
// which is what the model's extraction pass is asked to pick up.
func pubDate(root *html.Node) (time.Time, bool) {
	if t, ok := fromJSONLD(root); ok {
		return t, true
	}
	metas := map[string]string{}
	var times []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Meta:
				var k, v string
				for _, a := range n.Attr {
					switch a.Key {
					case "property", "name", "itemprop":
						k = strings.ToLower(a.Val)
					case "content":
						v = a.Val
					}
				}
				if k != "" && v != "" {
					metas[k] = v
				}
			case atom.Time:
				for _, a := range n.Attr {
					if a.Key == "datetime" {
						times = append(times, a.Val)
					}
				}
				if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					times = append(times, n.FirstChild.Data)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	for _, k := range []string{
		"article:published_time", "og:article:published_time", "datepublished",
		"publishdate", "date", "dc.date", "dc.date.issued", "sailthru.date",
		"parsely-pub-date", "article:modified_time",
	} {
		if t, ok := parseDate(metas[k]); ok {
			return t, true
		}
	}
	for _, v := range times {
		if t, ok := parseDate(v); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func fromJSONLD(root *html.Node) (time.Time, bool) {
	var out time.Time
	found := false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Script {
			for _, a := range n.Attr {
				if a.Key == "type" && strings.Contains(a.Val, "ld+json") && n.FirstChild != nil {
					var v any
					if json.Unmarshal([]byte(n.FirstChild.Data), &v) == nil {
						if t, ok := digDate(v); ok {
							out, found = t, true
							return
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out, found
}

func digDate(v any) (time.Time, bool) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"datePublished", "dateCreated", "uploadDate"} {
			if s, ok := x[k].(string); ok {
				if t, ok := parseDate(s); ok {
					return t, true
				}
			}
		}
		for _, sub := range x {
			if t, ok := digDate(sub); ok {
				return t, true
			}
		}
	case []any:
		for _, sub := range x {
			if t, ok := digDate(sub); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, f := range []string{
		time.RFC3339, "2006-01-02T15:04:05Z0700", "2006-01-02T15:04:05",
		"2006-01-02 15:04:05", "2006-01-02", "01/02/2006",
		"January 2, 2006", "2 January 2006", "Jan 2, 2006", "2 Jan 2006",
		"02 January 2006", time.RFC1123, time.RFC1123Z,
	} {
		if t, err := time.Parse(f, s); err == nil && t.Year() > 1990 && t.Year() < 2100 {
			return t, true
		}
	}
	return time.Time{}, false
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
