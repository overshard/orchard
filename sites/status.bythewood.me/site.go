package main

import "strings"

// Identity, stated once and not made configurable: hardcode identity, never
// hardcode credentials.
//
// A BASE_URL environment variable is the trap this avoids. Two things here need
// an absolute origin, and an empty default degrades quietly rather than loudly:
// sitemap.xml ships `<loc>/</loc>`, which is invalid, and an alert links at
// `/<uuid>` with no host, which is unopenable from a phone. Both are built from
// the constant below, so neither can be unset.
//
// The only value left in the environment is STATUS_PASSWORD.
//
// Staging below derives from baseURL, so this one line also controls the
// noindex, the robots.txt Disallow and the analytics collector.
const (
	baseURL    = "https://status.bythewood.me"
	siteName   = "Status"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"

	// This site's own property in analytics.
	analyticsID = "231ffafe-7a96-46d3-8a55-92f460fd98fb"
)

// Staging keeps a test hostname out of search results. There is nothing to
// cross-contaminate: status probes whatever properties its own database lists,
// so a staging instance probes its own.
var Staging = !strings.HasSuffix(baseURL, "//status.bythewood.me")

// sourceURL is where the footer link goes.
const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/status.bythewood.me"
