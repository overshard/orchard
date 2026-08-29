package main

import "strings"

// Identity, stated once and not made configurable: hardcode identity, never
// hardcode credentials.
//
// A BASE_URL environment variable is the trap this avoids. The collector
// snippet and the sitemap both need an absolute origin, so an empty one ships
// a site whose own tracking silently does nothing. The only value left in the
// environment is ANALYTICS_PASSWORD, which is a genuine secret.
//
// Staging below derives from baseURL, so this one line also controls the
// noindex, the robots.txt Disallow and the analytics collector.
const (
	baseURL    = "https://analytics.bythewood.me"
	siteName   = "Analytics"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"
)

// Staging keeps a test hostname out of search results. It does not gate
// self-tracking: a staging instance has its own database and its own Proprium
// row, and the collector posts same-origin, so there is no path from one to
// the other.
var Staging = !strings.HasSuffix(baseURL, "//analytics.bythewood.me")

// sourceURL is where the footer link goes.
const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/analytics.bythewood.me"
