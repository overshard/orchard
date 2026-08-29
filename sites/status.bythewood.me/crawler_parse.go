package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTML extraction for the SEO crawler.
//
// x/net/html is a tokenizer and a tree with no selector engine, so this walks
// the tree once and collects everything in a single pass rather than running a
// document query per field.

// ParsedHTML is everything the checks need from one page. The JSON tags are
// fixed by the stored data these are serialised into and read back from, not
// chosen here.
type ParsedHTML struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Canonical   string              `json:"canonical"`
	RobotsMeta  string              `json:"robots_meta"`
	Viewport    string              `json:"viewport"`
	Lang        string              `json:"lang"`
	OG          OpenGraph           `json:"og"`
	Twitter     TwitterCard         `json:"twitter"`
	Headings    map[string][]string `json:"headings"`
	Links       []Link              `json:"links"`
	Images      []Image             `json:"images"`
	Resources   []string            `json:"resources"`
	JSONLD      []json.RawMessage   `json:"json_ld"`
	JSONLDBad   int                 `json:"json_ld_bad"`
	Favicon     string              `json:"favicon"`
	Forms       []Form              `json:"forms"`
	WordCount   int                 `json:"word_count"`
	TextHash    string              `json:"text_hash"`
}

type OpenGraph struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	URL         string `json:"url"`
}

type TwitterCard struct {
	Card        string `json:"card"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type Link struct {
	URL  string   `json:"url"`
	Text string   `json:"text"`
	Rel  []string `json:"rel"`
}

// Image distinguishes an absent alt attribute from an empty one, which is what
// the pointer is for: `alt=""` is the correct markup for a decorative image,
// and flagging it would train the operator to ignore the check.
type Image struct {
	Src string  `json:"src"`
	Alt *string `json:"alt"`
}

type Form struct {
	Action    string      `json:"action"`
	Inputs    []FormInput `json:"inputs"`
	LabelFors []string    `json:"label_fors"`
}

type FormInput struct {
	Type      string  `json:"type"`
	Name      *string `json:"name"`
	ID        *string `json:"id"`
	AriaLabel *string `json:"aria_label"`
}

func attr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

func attrOr(n *html.Node, name, fallback string) string {
	if v, ok := attr(n, name); ok {
		return v
	}
	return fallback
}

func attrPtr(n *html.Node, name string) *string {
	if v, ok := attr(n, name); ok {
		return &v
	}
	return nil
}

// collapse normalises whitespace the way every comparison in checks.go
// assumes: runs of any whitespace become one space, ends trimmed.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// textOf returns the visible text of a subtree, with script, style and
// noscript contents dropped.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.DataAtom {
			case atom.Script, atom.Style, atom.Noscript:
				return
			}
		}
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
			// A separator, because "<b>foo</b><b>bar</b>" is two words on the
			// page and would otherwise hash and count as one.
			b.WriteByte(' ')
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return collapse(b.String())
}

// rawTextOf concatenates a node's direct text children, without textOf's
// skip list. Used for <script type="application/ld+json">, whose contents are
// data rather than prose.
func rawTextOf(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// resolve turns a possibly-relative href into an absolute URL.
func resolve(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := base.Parse(ref)
	if err != nil {
		return ref
	}
	return u.String()
}

// parseHTML walks a document once and pulls out everything the checks need.
func parseHTML(body []byte, pageURL string) (*ParsedHTML, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}

	p := &ParsedHTML{
		Headings:  map[string][]string{},
		Links:     []Link{},
		Images:    []Image{},
		Resources: []string{},
		JSONLD:    []json.RawMessage{},
		Forms:     []Form{},
	}
	for _, level := range []string{"h1", "h2", "h3", "h4", "h5", "h6"} {
		p.Headings[level] = []string{}
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type != html.ElementNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}

		switch n.DataAtom {
		case atom.Html:
			p.Lang = strings.TrimSpace(attrOr(n, "lang", ""))

		case atom.Title:
			if p.Title == "" {
				p.Title = textOf(n)
			}

		case atom.Meta:
			content := strings.TrimSpace(attrOr(n, "content", ""))
			// Both spellings, because og: uses property= and twitter: uses
			// name=, and plenty of real pages get that backwards.
			switch strings.ToLower(attrOr(n, "name", "")) {
			case "description":
				setIfEmpty(&p.Description, content)
			case "robots":
				setIfEmpty(&p.RobotsMeta, content)
			case "viewport":
				setIfEmpty(&p.Viewport, content)
			case "twitter:card":
				setIfEmpty(&p.Twitter.Card, content)
			case "twitter:title":
				setIfEmpty(&p.Twitter.Title, content)
			case "twitter:description":
				setIfEmpty(&p.Twitter.Description, content)
			}
			switch strings.ToLower(attrOr(n, "property", "")) {
			case "og:title":
				setIfEmpty(&p.OG.Title, content)
			case "og:description":
				setIfEmpty(&p.OG.Description, content)
			case "og:image":
				setIfEmpty(&p.OG.Image, content)
			case "og:url":
				setIfEmpty(&p.OG.URL, content)
			}

		case atom.Link:
			href := attrOr(n, "href", "")
			rels := strings.Fields(strings.ToLower(attrOr(n, "rel", "")))
			for _, rel := range rels {
				if rel == "canonical" && p.Canonical == "" {
					p.Canonical = resolve(base, href)
				}
				// "icon", "shortcut icon" and "apple-touch-icon" all contain
				// "icon", so a site with only an apple-touch-icon is not
				// flagged.
				if strings.Contains(rel, "icon") && p.Favicon == "" && href != "" {
					p.Favicon = resolve(base, href)
				}
			}
			if href != "" {
				p.Resources = append(p.Resources, resolve(base, href))
			}

		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			key := n.Data
			p.Headings[key] = append(p.Headings[key], textOf(n))

		case atom.A:
			if href, ok := attr(n, "href"); ok {
				href = strings.TrimSpace(href)
				// Fragments, mailto, tel and javascript are not pages and
				// probing them would produce nothing but noise.
				if href != "" &&
					!strings.HasPrefix(href, "#") &&
					!strings.HasPrefix(strings.ToLower(href), "javascript:") &&
					!strings.HasPrefix(strings.ToLower(href), "mailto:") &&
					!strings.HasPrefix(strings.ToLower(href), "tel:") {
					p.Links = append(p.Links, Link{
						URL:  resolve(base, href),
						Text: textOf(n),
						Rel:  strings.Fields(strings.ToLower(attrOr(n, "rel", ""))),
					})
				}
			}

		case atom.Img:
			src := strings.TrimSpace(attrOr(n, "src", ""))
			img := Image{Alt: attrPtr(n, "alt")}
			if src != "" {
				img.Src = resolve(base, src)
				p.Resources = append(p.Resources, img.Src)
			}
			p.Images = append(p.Images, img)

		case atom.Script:
			if strings.EqualFold(attrOr(n, "type", ""), "application/ld+json") {
				// rawTextOf, not textOf. textOf skips script elements
				// because their contents are not visible page text, which is
				// right for the word count and wrong here.
				raw := strings.TrimSpace(rawTextOf(n))
				if raw != "" {
					if json.Valid([]byte(raw)) {
						p.JSONLD = append(p.JSONLD, json.RawMessage(raw))
					} else {
						// Counted rather than appended as a null, so a page
						// whose valid JSON-LD is the literal `null` is not
						// reported as a parse failure.
						p.JSONLDBad++
					}
				}
			}
			if src := attrOr(n, "src", ""); src != "" {
				p.Resources = append(p.Resources, resolve(base, src))
			}

		case atom.Iframe, atom.Source:
			if src := attrOr(n, "src", ""); src != "" {
				p.Resources = append(p.Resources, resolve(base, src))
			}

		case atom.Form:
			p.Forms = append(p.Forms, parseForm(n, base, pageURL))
			// parseForm collects the form whole, and nested forms are not
			// legal HTML. Descend anyway, because a form can contain links,
			// images and headings the other checks want.
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	// Visible text, for the thin-content and duplicate-content checks. Parsing
	// decodes HTML entities, so `&nbsp;` does not count as a word.
	text := textOf(doc)
	p.WordCount = len(strings.Fields(text))
	sum := sha256.Sum256([]byte(text))
	p.TextHash = hex.EncodeToString(sum[:])

	return p, nil
}

func setIfEmpty(dst *string, v string) {
	if *dst == "" {
		*dst = v
	}
}

// parseForm collects a form's inputs and the ids its labels point at.
func parseForm(form *html.Node, base *url.URL, pageURL string) Form {
	f := Form{Inputs: []FormInput{}, LabelFors: []string{}}

	action, ok := attr(form, "action")
	if !ok || strings.TrimSpace(action) == "" {
		// An empty or absent action submits to the current page, which is what
		// the check reports as the form's identity.
		f.Action = pageURL
	} else {
		f.Action = resolve(base, action)
	}

	seenLabels := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Input, atom.Textarea, atom.Select:
				f.Inputs = append(f.Inputs, FormInput{
					// Per the HTML spec an input with no type attribute is a
					// text input, which is also the case that most needs a
					// label.
					Type:      strings.ToLower(attrOr(n, "type", "text")),
					Name:      attrPtr(n, "name"),
					ID:        attrPtr(n, "id"),
					AriaLabel: attrPtr(n, "aria-label"),
				})
			case atom.Label:
				if v, ok := attr(n, "for"); ok && !seenLabels[v] {
					seenLabels[v] = true
					f.LabelFors = append(f.LabelFors, v)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(form)

	return f
}
