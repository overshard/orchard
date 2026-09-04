package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// The sources answer where the research came from, which is not the same as
// where the thing itself lives. Asked for the most popular Go project and told
// "Ollama", a reader wants a link to Ollama, not only to the listicle that said
// so.
//
// Candidates come from the links already harvested off the fetched pages, so
// finding one costs no extra search: a page that names a project nearly always
// links to it. Each candidate is then fetched and checked to actually be the
// thing before it is shown, because a wrong link is worse than none.

// EntityLink is a verified link to something an answer named.
type EntityLink struct {
	Name  string
	URL   string
	Title string
	Host  string
}

const (
	maxEntities = 4
	// Each check is one HTTP request, so the fan-out is bounded.
	maxCandidatesPerEntity = 3
)

// linkEntities finds what the answer named and resolves each to a checked URL.
func (e *Engine) linkEntities(ctx context.Context, question, answer string, links []Link) []EntityLink {
	if len(links) == 0 {
		return nil
	}
	names := e.namedEntities(ctx, question, answer)
	if len(names) == 0 {
		return nil
	}

	var (
		mu  sync.Mutex
		out []EntityLink
		wg  sync.WaitGroup
	)
	for _, name := range names {
		cands := rankCandidates(name, links)
		if len(cands) == 0 {
			continue
		}
		wg.Add(1)
		go func(name string, cands []Link) {
			defer wg.Done()
			for _, c := range cands {
				if title, ok := verifyLink(ctx, e.client, c.URL, name); ok {
					mu.Lock()
					out = append(out, EntityLink{
						Name: name, URL: c.URL, Title: title, Host: hostname(c.URL),
					})
					mu.Unlock()
					return
				}
			}
		}(name, cands)
	}
	wg.Wait()

	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// namedEntities asks which specific things the answer named. Schema
// constrained, and told to return nothing when the answer names nothing
// linkable, which is the common case for a question like the deepest river.
func (e *Engine) namedEntities(ctx context.Context, question, answer string) []string {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"entities": map[string]any{
				"type":     "array",
				"items":    map[string]any{"type": "string"},
				"maxItems": maxEntities,
			},
		},
		"required":             []string{"entities"},
		"additionalProperties": false,
	}
	system := strings.Join([]string{
		"You list the specific named things in an answer that a reader would want a link to.",
		"Include software projects, products, companies, tools, books, films and organisations, by their exact name.",
		"Exclude generic nouns, common concepts, units, places, and anything that is not a specific named thing someone could visit a page for.",
		"Return an empty list when the answer names nothing like that. An empty list is a normal and common answer.",
	}, " ")
	var out struct {
		Entities []string `json:"entities"`
	}
	if err := e.llm.Structured(ctx, system,
		"Question: "+question+"\n\nAnswer:\n"+truncate(plainText(answer), 1600),
		200, schema, &out); err != nil {
		return nil
	}

	var names []string
	seen := map[string]bool{}
	for _, n := range out.Entities {
		n = strings.TrimSpace(n)
		k := strings.ToLower(n)
		if len(n) < 2 || len(n) > 60 || seen[k] {
			continue
		}
		seen[k] = true
		names = append(names, n)
	}
	return names
}

// rankCandidates orders the harvested links by how well each looks like the
// home of the named thing. Anchor text matching first, then the host and path.
func rankCandidates(name string, links []Link) []Link {
	type scored struct {
		link  Link
		score int
	}
	slug := slugify(name)
	if slug == "" {
		return nil
	}
	lower := strings.ToLower(name)

	var all []scored
	for _, l := range links {
		s := 0
		text := strings.ToLower(l.Text)
		host := strings.ToLower(hostname(l.URL))
		path := strings.ToLower(l.URL)

		switch {
		case text == lower:
			s += 6
		case strings.Contains(text, lower):
			s += 3
		}
		// The strongest signal there is: the thing's name is the domain.
		if strings.HasPrefix(registrable(host), slug+".") || registrable(host) == slug+".com" {
			s += 6
		} else if strings.Contains(strings.ReplaceAll(host, "-", ""), slug) {
			s += 3
		}
		if strings.Contains(strings.ReplaceAll(path, "-", ""), slug) {
			s++
		}
		// A repository or a docs page is usually the canonical home for the
		// kind of thing that gets named in these answers.
		if strings.Contains(host, "github.com") && strings.Contains(path, slug) {
			s += 3
		}
		if s > 0 {
			all = append(all, scored{l, s})
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].score > all[b].score })

	var out []Link
	for i, c := range all {
		if i >= maxCandidatesPerEntity || c.score < 3 {
			break
		}
		out = append(out, c.link)
	}
	return out
}

func slugify(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		}
		return -1
	}, s)
}

// verifyLink fetches a candidate and confirms the page is about the thing. This
// is the difference between a link and a guess: a 404, a parked domain, or a
// page about something else all fail here rather than being handed over.
func verifyLink(ctx context.Context, client *http.Client, target, name string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return "", false
	}
	browserHeaders(req, "")
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "html") {
		return "", false
	}
	body, err := readBody(resp, 512<<10)
	if err != nil {
		return "", false
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", false
	}

	title := metaTitle(doc)
	slug := slugify(name)
	// The name has to appear in the title, the description, or the first
	// heading. A page that never says what it is called is not evidence.
	haystack := slugify(title) + " " + slugify(metaContent(doc, "og:description")) +
		" " + slugify(metaContent(doc, "description")) + " " + slugify(firstHeading(doc))
	if !strings.Contains(strings.ReplaceAll(haystack, " ", ""), slug) {
		return "", false
	}
	return title, true
}

func firstHeading(root *html.Node) string {
	var out string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if out != "" {
			return
		}
		if n.Type == html.ElementNode && (n.DataAtom == atom.H1 || n.DataAtom == atom.H2) {
			out = strings.Join(strings.Fields(textContent(n)), " ")
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}
