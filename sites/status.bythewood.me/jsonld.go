package main

import (
	"encoding/json"
	"html/template"
)

// The Person node carries the same @id the other sites use, so they describe one
// person rather than several strangers sharing a name.
const (
	personID  = "https://isaacbythewood.com/#person"
	websiteID = baseURL + "/#website"
	appID     = baseURL + "/#app"
)

// jsonLD marshals a graph for the script tag. template.JS is safe here because
// encoding/json escapes <, > and & by default, so nothing can close the element.
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
			"name":                "Status",
			"url":                 baseURL + "/",
			"description":         "Self-hosted uptime monitoring with status pages, Lighthouse audits and crawl findings.",
			"applicationCategory": "DeveloperApplication",
			"operatingSystem":     "Any",
			"author":              map[string]any{"@id": personID},
			"publisher":           map[string]any{"@id": personID},
			"image":               baseURL + "/static/og/card.png",
			// A zero price is how schema.org says free; omitting offers reads as unknown.
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
			"description": "Self-hosted uptime monitoring with status pages, Lighthouse audits and crawl findings.",
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
