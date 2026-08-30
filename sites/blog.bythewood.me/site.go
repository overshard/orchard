package main

import "strings"

// Identity is hardcoded, never configured. This site has no credentials at all,
// so it needs no .env.
const (
	baseURL     = "https://blog.bythewood.me"
	siteName    = "Isaac Bythewood's Blog"
	siteTagline = "Blog"
	authorName  = "Isaac Bythewood"
	githubUser  = "overshard"
	analyticsID = "0d379e18-9ea7-4228-a8bf-82369c25ab84"
)

// Staging is true on any hostname but the real one; it drives the noindex, the
// robots.txt Disallow and the analytics collector.
var Staging = !strings.HasSuffix(baseURL, "//blog.bythewood.me")

// FooterLink is one entry in a footer column.
type FooterLink struct {
	Label string
	Href  string
}

var (
	footerProjects = []FooterLink{
		{Label: "Taproot", Href: "https://github.com/overshard/taproot"},
		{Label: "Dark Furrow", Href: "https://github.com/overshard/darkfurrow.com"},
		{Label: "Timelite", Href: "https://github.com/overshard/timelite"},
		{Label: "Status", Href: "https://github.com/overshard/status"},
		{Label: "Analytics", Href: "https://github.com/overshard/analytics"},
	}

	footerLinks = []FooterLink{
		{Label: "Portfolio", Href: "https://isaacbythewood.com/"},
		{Label: "GitHub", Href: "https://github.com/overshard"},
		{Label: "LinkedIn", Href: "https://www.linkedin.com/in/ibythewood/"},
		{Label: "Blog Analytics", Href: "https://analytics.bythewood.me/" + analyticsID},
		{Label: "Blog Status", Href: "https://status.bythewood.me/3b7aec61-6397-4d8f-b1b4-1a4649f149cd"},
	}
)

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/blog.bythewood.me"
