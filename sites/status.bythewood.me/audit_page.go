package main

import (
	"bytes"
	"cmp"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// pageInfo is everything the audit checks read off one HTML document.
type pageInfo struct {
	Doctype     bool
	Lang        string
	Charset     bool
	Title       string
	Description string
	Viewport    string
	RobotsMeta  string
	Canonical   string
	Refresh     bool
	Elements    int
	HasMain     bool
	BodyHidden  bool

	Stylesheets []pageRef
	Scripts     []pageRef
	Images      []pageImage
	Links       []pageLink
	Unnamed     []string // buttons and controls with no accessible name
	Unlabelled  []string // form fields with no label
	Frames      []string // iframes with no title
	BadRoles    []string
	PosTabindex int
	DupIDs      []string
	HeadingJump string
	Mixed       []string
}

// pageRef is a stylesheet or script. Blocking means it holds up the first
// paint, a stylesheet in head for screen or a classic script without
// async or defer.
type pageRef struct {
	URL      string
	Blocking bool
}

type pageImage struct {
	URL     string
	HasAlt  bool
	Sized   bool
	Decor   bool
	Snippet string
}

type pageLink struct {
	URL  string
	Name string
}

var knownRoles = map[string]bool{}

// mixedTags load what they point at, so an http:// URL on them is mixed content.
var mixedTags = map[string]bool{"img": true, "script": true, "link": true, "iframe": true, "video": true, "audio": true, "source": true}

func init() {
	for _, r := range strings.Fields(`alert alertdialog application article banner blockquote button caption cell
		checkbox code columnheader combobox complementary contentinfo definition deletion dialog directory
		document emphasis feed figure form generic grid gridcell group heading img insertion link list listbox
		listitem log main mark marquee math menu menubar menuitem menuitemcheckbox menuitemradio meter navigation
		none note option paragraph presentation progressbar radio radiogroup region row rowgroup rowheader
		scrollbar search searchbox separator slider spinbutton status strong subscript superscript switch tab
		table tablist tabpanel term textbox time timer toolbar tooltip tree treegrid treeitem`) {
		knownRoles[r] = true
	}
}

func walkPage(body []byte, base *url.URL) (*pageInfo, error) {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	p := &pageInfo{}
	head := bytes.ToLower(body[:min(len(body), 1024)])
	p.Charset = bytes.Contains(head, []byte("charset"))

	ids := map[string]int{}
	labelled := map[string]bool{}
	type field struct {
		id, desc string
		named    bool
	}
	var fields []field
	lastHeading := 0

	var walk func(n *html.Node, inHead, inLabel bool)
	walk = func(n *html.Node, inHead, inLabel bool) {
		switch n.Type {
		case html.DoctypeNode:
			p.Doctype = strings.EqualFold(n.Data, "html")
		case html.ElementNode:
			p.Elements++
			tag := n.Data
			if id := attrOr(n, "id", ""); id != "" {
				ids[id]++
			}
			if role := strings.TrimSpace(attrOr(n, "role", "")); role != "" {
				for _, r := range strings.Fields(strings.ToLower(role)) {
					if !knownRoles[r] {
						p.BadRoles = append(p.BadRoles, tag+`[role="`+role+`"]`)
						break
					}
				}
				if role == "main" {
					p.HasMain = true
				}
			}
			if t, ok := attr(n, "tabindex"); ok && strings.TrimSpace(t) != "" && t[0] >= '1' && t[0] <= '9' {
				p.PosTabindex++
			}
			if base.Scheme == "https" && mixedTags[tag] {
				v := attrOr(n, "src", "")
				if tag == "link" && slices.Contains(strings.Fields(strings.ToLower(attrOr(n, "rel", ""))), "stylesheet") {
					v = attrOr(n, "href", "")
				}
				if strings.HasPrefix(v, "http://") {
					p.Mixed = append(p.Mixed, tag+" "+snippet(v))
				}
			}

			switch tag {
			case "html":
				p.Lang = strings.TrimSpace(attrOr(n, "lang", ""))
			case "head":
				inHead = true
			case "body":
				inHead = false
				p.BodyHidden = attrOr(n, "aria-hidden", "") == "true"
			case "main":
				p.HasMain = true
			case "title":
				if p.Title == "" {
					p.Title = textOf(n)
				}
			case "meta":
				name := strings.ToLower(attrOr(n, "name", ""))
				content := attrOr(n, "content", "")
				switch {
				case name == "description":
					p.Description = content
				case name == "viewport":
					p.Viewport = content
				case name == "robots":
					p.RobotsMeta = strings.ToLower(content)
				case strings.EqualFold(attrOr(n, "http-equiv", ""), "refresh"):
					p.Refresh = true
				}
			case "link":
				rel := strings.ToLower(attrOr(n, "rel", ""))
				href := resolve(base, attrOr(n, "href", ""))
				switch {
				case strings.Contains(rel, "canonical"):
					p.Canonical = href
				case slices.Contains(strings.Fields(rel), "stylesheet") && !strings.Contains(rel, "alternate"):
					media := strings.ToLower(attrOr(n, "media", ""))
					blocking := inHead && (media == "" || media == "all" || media == "screen")
					p.Stylesheets = append(p.Stylesheets, pageRef{href, blocking})
				}
			case "script":
				if src, ok := attr(n, "src"); ok && src != "" {
					_, async := attr(n, "async")
					_, deferred := attr(n, "defer")
					typ := strings.ToLower(attrOr(n, "type", ""))
					blocking := inHead && !async && !deferred && typ != "module"
					p.Scripts = append(p.Scripts, pageRef{resolve(base, src), blocking})
				}
			case "img":
				src := attrOr(n, "src", "")
				if f := strings.Fields(attrOr(n, "srcset", "")); src == "" && len(f) > 0 {
					src = f[0]
				}
				_, hasAlt := attr(n, "alt")
				_, w := attr(n, "width")
				_, h := attr(n, "height")
				role := attrOr(n, "role", "")
				p.Images = append(p.Images, pageImage{
					URL: resolveIf(base, src), HasAlt: hasAlt, Sized: w && h,
					Decor:   role == "presentation" || role == "none" || attrOr(n, "aria-hidden", "") == "true",
					Snippet: snippet(cmp.Or(src, "img"+idSuffix(n))),
				})
			case "a":
				if href, ok := attr(n, "href"); ok {
					p.Links = append(p.Links, pageLink{URL: resolve(base, href), Name: accessibleName(n)})
				}
			case "button":
				if accessibleName(n) == "" {
					p.Unnamed = append(p.Unnamed, "button"+idSuffix(n))
				}
			case "iframe":
				if strings.TrimSpace(attrOr(n, "title", "")) == "" && attrOr(n, "aria-hidden", "") != "true" {
					p.Frames = append(p.Frames, snippet(attrOr(n, "src", "")))
				}
			case "label":
				if f := attrOr(n, "for", ""); f != "" {
					labelled[f] = true
				}
				inLabel = true
			case "input", "select", "textarea":
				typ := strings.ToLower(attrOr(n, "type", "text"))
				switch typ {
				case "hidden":
				case "submit", "reset", "button", "image":
					_, v := attr(n, "value")
					_, alt := attr(n, "alt")
					if typ == "image" && !alt || typ == "button" && !v && attrOr(n, "aria-label", "") == "" {
						p.Unnamed = append(p.Unnamed, "input["+typ+"]"+idSuffix(n))
					}
				default:
					named := inLabel || attrOr(n, "aria-label", "") != "" || attrOr(n, "aria-labelledby", "") != "" || attrOr(n, "title", "") != ""
					fields = append(fields, field{attrOr(n, "id", ""), tag + `[name="` + attrOr(n, "name", "") + `"]`, named})
				}
			case "h1", "h2", "h3", "h4", "h5", "h6":
				level := int(tag[1] - '0')
				if lastHeading > 0 && level > lastHeading+1 && p.HeadingJump == "" {
					p.HeadingJump = "h" + string(rune('0'+lastHeading)) + " to " + tag
				}
				lastHeading = level
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inHead, inLabel)
		}
	}
	walk(root, false, false)

	for _, f := range fields {
		if !f.named && (f.id == "" || !labelled[f.id]) {
			p.Unlabelled = append(p.Unlabelled, f.desc)
		}
	}
	for id, n := range ids {
		if n > 1 {
			p.DupIDs = append(p.DupIDs, "#"+id)
		}
	}
	return p, nil
}

// accessibleName is the text a screen reader would announce, close enough for
// a static check: the text inside, aria-label, title, or an image's alt.
func accessibleName(n *html.Node) string {
	if v := strings.TrimSpace(attrOr(n, "aria-label", "")); v != "" {
		return v
	}
	if attrOr(n, "aria-labelledby", "") != "" {
		return "labelledby"
	}
	if t := strings.TrimSpace(textOf(n)); t != "" {
		return t
	}
	var alt string
	var find func(*html.Node)
	find = func(c *html.Node) {
		for ; c != nil && alt == ""; c = c.NextSibling {
			if c.Type == html.ElementNode {
				switch c.Data {
				case "img":
					alt = strings.TrimSpace(attrOr(c, "alt", ""))
				case "svg":
					if l := attrOr(c, "aria-label", ""); l != "" {
						alt = l
					}
				case "title":
					alt = strings.TrimSpace(textOf(c))
				}
				find(c.FirstChild)
			}
		}
	}
	find(n.FirstChild)
	if alt != "" {
		return alt
	}
	return strings.TrimSpace(attrOr(n, "title", ""))
}

func idSuffix(n *html.Node) string {
	if id := attrOr(n, "id", ""); id != "" {
		return "#" + id
	}
	if c := attrOr(n, "class", ""); c != "" {
		return "." + strings.Fields(c)[0]
	}
	return ""
}

func snippet(s string) string {
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

func resolveIf(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	return resolve(base, ref)
}
