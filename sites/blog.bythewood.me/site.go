package main

import "strings"

// Identity, stated once and not made configurable.
//
// decisions/0008 draws the line: hardcode identity, never hardcode
// credentials. The Rust version read BLOG_ROOT and PORT from a .env through
// dotenvy, which was product scaffolding for a deployer who does not exist.
// This site has no credentials at all, so it needs no .env of any kind.
//
// baseURL went to the real hostname at cutover on 2026-08-27. Staging below
// derives from it, so flipping this one line also turned off the noindex, the
// robots.txt Disallow and the analytics collector.
const (
	baseURL     = "https://blog.bythewood.me"
	siteName    = "Isaac Bythewood's Blog"
	siteTagline = "Blog"
	authorName  = "Isaac Bythewood"
	githubUser  = "overshard"
	analyticsID = "0d379e18-9ea7-4228-a8bf-82369c25ab84"
)

// Staging keeps the test hostname out of search results.
//
// Without it next.blog.bythewood.me is crawled and indexed as a complete
// duplicate of the real blog, which is the one way a staging host can actually
// damage the production one. robots.txt and a noindex meta tag both key off
// it, because a meta tag alone is useless on a URL robots.txt has already told
// the crawler not to fetch.
var Staging = !strings.HasSuffix(baseURL, "//blog.bythewood.me")

// FooterLink is one entry in a footer column.
type FooterLink struct {
	Label string
	Href  string
}

// The footer columns, lifted from the Rust templates where they were hand
// written twice over. Data here, one loop in the template.
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

// sourceURL points at this site inside the monorepo rather than at the old
// per-project repo, which is where the footer link used to go.
const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/blog.bythewood.me"
