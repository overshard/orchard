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

	// The property this site is tracked under, hardcoded exactly as the other
	// five sites hardcode theirs. It used to be generated on first boot and
	// stashed in meta under the name "Proprium", which made this the one site
	// whose own property was named differently from every other and created by
	// a code path no other site had. Identity, not a credential: the id is in
	// the page source by design.
	analyticsID = "580ebab0-3d14-4a20-89da-d57fc3d7d9e8"
)

// Staging keeps a test hostname out of search results, and it gates
// self-tracking the same way the other five sites gate theirs. The collector
// posts same-origin, so a staging instance was never able to reach production's
// database anyway; what it would do instead is post an id its own database has
// no row for, which the collector rejects. Sending nothing is the honest
// version of that.
var Staging = !strings.HasSuffix(baseURL, "//analytics.bythewood.me")

// collectorID is the id handed to the self-tracking snippet, and it is empty on
// staging so the snippet renders nothing rather than posting an id that staging
// database has no row for.
func collectorID() string {
	if Staging {
		return ""
	}
	return analyticsID
}

// sourceURL is where the footer link goes.
const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/analytics.bythewood.me"
