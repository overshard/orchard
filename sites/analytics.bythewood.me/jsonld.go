package main

import (
	"encoding/json"
	"html/template"
)

// Structured data, as one JSON-LD graph per page.
//
// This site had none. It is a self-hosted tool with a public marketing page
// and public per-property pages, so the useful statement is what the thing is
// (a WebApplication), who made it, and that the pages belong to one site.
// Schema.org is not a ranking lever and the 2026 evidence is that it does not
// move AI Overview citations either; the claim is only that a machine reading
// this page can tell what it is looking at.
//
// The Person node carries the same @id the portfolio and the blog use, so all
// four sites describe one person rather than four strangers with one name.

const (
	personID  = "https://isaacbythewood.com/#person"
	websiteID = baseURL + "/#website"
	appID     = baseURL + "/#app"
)

// jsonLD marshals a graph for the script tag.
//
// template.JS means "already script content, do not escape again", which is
// safe because encoding/json escapes <, > and & to \u003c, \u003e and \u0026
// by default, so nothing in here can close the element early.
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

// pageGraph is the graph every page on this site carries.
func pageGraph(title, description, canonical string) template.JS {
	return jsonLD(
		map[string]any{
			"@type":    "Person",
			"@id":      personID,
			"name":     authorName,
			"url":      "https://isaacbythewood.com/",
			"jobTitle": "Senior Solutions Architect",
			"sameAs": []string{
				"https://github.com/" + githubUser,
				"https://isaacbythewood.com/",
			},
		},
		map[string]any{
			"@type":               "WebApplication",
			"@id":                 appID,
			"name":                "Analytics",
			"url":                 baseURL + "/",
			"description":         "Self-hosted website analytics. Page views, clicks, scrolls, sessions, and custom events.",
			"applicationCategory": "BusinessApplication",
			"operatingSystem":     "Any",
			"author":              map[string]any{"@id": personID},
			"publisher":           map[string]any{"@id": personID},
			"image":               baseURL + "/static/og/card.png",
			// Self-hosted and not for sale. Stating a zero price is how
			// schema.org expresses that, and omitting offers entirely reads
			// as "unknown" rather than "free".
			"offers": map[string]any{
				"@type":         "Offer",
				"price":         "0",
				"priceCurrency": "USD",
			},
		},
		map[string]any{
			"@type":       "WebSite",
			"@id":         websiteID,
			"url":         baseURL + "/",
			"name":        siteName,
			"description": "Self-hosted website analytics. Page views, clicks, scrolls, sessions, and custom events.",
			"inLanguage":  "en",
			"publisher":   map[string]any{"@id": personID},
		},
		map[string]any{
			"@type":       "WebPage",
			"@id":         canonical + "#webpage",
			"url":         canonical,
			"name":        title,
			"description": description,
			"isPartOf":    map[string]any{"@id": websiteID},
			"about":       map[string]any{"@id": appID},
			"inLanguage":  "en",
		},
	)
}
