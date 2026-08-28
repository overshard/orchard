package main

import (
	"encoding/json"
	"html/template"
	"strconv"
	"strings"
)

// Structured data, as one JSON-LD graph per page.
//
// The blog had none, which is the largest single gap a technical SEO audit of
// this site turns up. Schema.org markup is what lets a search engine know that
// a page is an article rather than a list of links, who wrote it, when it was
// published and what it is about, and it is the input to article rich results.
// It is not a ranking shortcut and the 2026 evidence is that it does not move
// AI Overview citations either, so the claim here is narrow: it makes the page
// legible to a machine, and costs about a kilobyte.
//
// JSON-LD rather than microdata or RDFa because it is the only format Google
// recommends, and because it keeps the markup out of the templates entirely.
//
// One @graph rather than several sibling <script> blocks: the nodes reference
// each other by @id (an article points at its author, the author is stated
// once), and a graph is how you express that without repeating the Person on
// every page.

// jsonLD marshals a graph into the exact bytes that go inside the script tag.
//
// The return type is template.JS, which tells html/template this is already
// script content and must not be escaped again. What makes that safe is that
// encoding/json escapes <, > and & to \u003c, \u003e and \u0026 by default,
// so a post description containing "</script>" cannot close the element. That
// default is the reason this uses Marshal rather than an Encoder, which would
// need SetEscapeHTML(true) restating it.
func jsonLD(nodes ...map[string]any) template.JS {
	graph := map[string]any{
		"@context": "https://schema.org",
		"@graph":   nodes,
	}
	// Indentation is for the human reading view-source; the bytes are tiny
	// either way and gzip removes the difference.
	buf, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return ""
	}
	return template.JS(buf)
}

// personNode is the author, stated once and referenced by @id everywhere else.
func personNode() map[string]any {
	return map[string]any{
		"@type":    "Person",
		"@id":      authorID,
		"name":     authorName,
		"url":      "https://isaacbythewood.com/",
		"jobTitle": "Senior Solutions Architect",
		"sameAs": []string{
			"https://github.com/" + githubUser,
			"https://isaacbythewood.com/",
		},
	}
}

const (
	authorID  = "https://isaacbythewood.com/#person"
	websiteID = baseURL + "/#website"
)

// siteGraph is what every page that is not a post carries.
func siteGraph() template.JS {
	return jsonLD(personNode(), map[string]any{
		"@type":       "Blog",
		"@id":         websiteID,
		"url":         baseURL + "/",
		"name":        siteName,
		"description": "Writing about webdev, infrastructure, security, and tooling.",
		"inLanguage":  "en",
		"author":      map[string]any{"@id": authorID},
		"publisher":   map[string]any{"@id": authorID},
	})
}

// postGraph describes one article, plus the breadcrumb trail that leads to it.
func postGraph(p *Post) template.JS {
	article := map[string]any{
		"@type":            "BlogPosting",
		"@id":              baseURL + p.URL() + "#article",
		"headline":         p.Title,
		"description":      p.Description,
		"url":              baseURL + p.URL(),
		"datePublished":    feedTime(p.PublishDate),
		"dateModified":     feedTime(p.Date),
		"author":           map[string]any{"@id": authorID},
		"publisher":        map[string]any{"@id": authorID},
		"isPartOf":         map[string]any{"@id": websiteID},
		"mainEntityOfPage": map[string]any{"@type": "WebPage", "@id": baseURL + p.URL()},
		"inLanguage":       "en",
		"image": map[string]any{
			"@type":  "ImageObject",
			"url":    p.OGImage(),
			"width":  ogWidth,
			"height": ogHeight,
		},
	}
	if len(p.Tags) > 0 {
		article["keywords"] = strings.Join(p.Tags, ", ")
	}
	if p.ReadTime > 0 {
		// Schema.org wants an ISO 8601 duration.
		article["timeRequired"] = "PT" + strconv.Itoa(p.ReadTime) + "M"
	}

	crumbs := map[string]any{
		"@type": "BreadcrumbList",
		"itemListElement": []map[string]any{
			{"@type": "ListItem", "position": 1, "name": "Home", "item": baseURL + "/"},
			{"@type": "ListItem", "position": 2, "name": "Blog", "item": baseURL + "/blog/"},
			{"@type": "ListItem", "position": 3, "name": p.Title, "item": baseURL + p.URL()},
		},
	}

	return jsonLD(personNode(), article, crumbs)
}
