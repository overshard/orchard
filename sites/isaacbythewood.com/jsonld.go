package main

import (
	"encoding/json"
	"html/template"
)

// Structured data for the portfolio, as one JSON-LD graph per page. For a
// personal site the useful statement is the person: a job title, an employer
// and a set of profiles elsewhere, so a search engine can connect this page to
// the blog and the GitHub account instead of reading five hostnames as five
// unrelated strangers.
//
// The Person node uses the same @id the blog's graph references, so the two
// sites describe one entity rather than two people with the same name.

const (
	personID  = baseURL + "/#person"
	websiteID = baseURL + "/#website"
)

// jsonLD marshals a graph for the script tag. template.JS is safe here because
// encoding/json escapes <, > and & by default, so no string in the graph can
// close the element early.
func jsonLD(nodes ...map[string]any) template.JS {
	buf, err := json.MarshalIndent(map[string]any{
		"@context": "https://schema.org",
		"@graph":   nodes,
	}, "", "  ")
	if err != nil {
		return ""
	}
	return template.JS(buf)
}

func personNode() map[string]any {
	return map[string]any{
		"@type":    "Person",
		"@id":      personID,
		"name":     "Isaac Bythewood",
		"url":      baseURL + "/",
		"jobTitle": "Senior Solutions Architect",
		"email":    "mailto:" + contactMail,
		"image":    baseURL + "/static/og/card.png",
		"worksFor": map[string]any{
			"@type": "Organization",
			"name":  "Craftmaster Furniture",
		},
		"address": map[string]any{
			"@type":           "PostalAddress",
			"addressLocality": "Elkin",
			"addressRegion":   "NC",
			"addressCountry":  "US",
		},
		"sameAs": []string{
			"https://github.com/" + githubUser,
			"https://blog.bythewood.me/",
		},
	}
}

// pageGraph is the graph for one page: the person, the site, and the page
// itself pointing at both.
func pageGraph(title, description, canonical string, crumbs []NavPage) template.JS {
	nodes := []map[string]any{
		personNode(),
		{
			"@type":       "WebSite",
			"@id":         websiteID,
			"url":         baseURL + "/",
			"name":        "Isaac Bythewood",
			"description": siteDesc,
			"inLanguage":  "en",
			"publisher":   map[string]any{"@id": personID},
		},
		{
			"@type":       "WebPage",
			"@id":         canonical + "#webpage",
			"url":         canonical,
			"name":        title,
			"description": description,
			"isPartOf":    map[string]any{"@id": websiteID},
			"about":       map[string]any{"@id": personID},
			"inLanguage":  "en",
		},
	}

	// A breadcrumb is only meaningful below the root. On the home page the
	// trail would be one item pointing at itself.
	if len(crumbs) > 0 {
		items := []map[string]any{
			{"@type": "ListItem", "position": 1, "name": "Home", "item": baseURL + "/"},
		}
		for i, c := range crumbs {
			items = append(items, map[string]any{
				"@type":    "ListItem",
				"position": i + 2,
				"name":     c.Title,
				"item":     baseURL + c.Href,
			})
		}
		nodes = append(nodes, map[string]any{
			"@type":           "BreadcrumbList",
			"itemListElement": items,
		})
	}

	return jsonLD(nodes...)
}
