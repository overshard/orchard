package main

import "strings"

// Identity is hardcoded, never configured: an empty BASE_URL would ship a site
// whose own tracking silently did nothing. There are no environment variables
// left at all now that signing in happens on auth.bythewood.me.
const (
	baseURL    = "https://analytics.bythewood.me"
	siteName   = "Analytics"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"

	// Identity, not a credential: this id is in the page source by design.
	analyticsID = "580ebab0-3d14-4a20-89da-d57fc3d7d9e8"
)

// Staging is true on any hostname but the real one; it drives the noindex, the
// robots.txt Disallow and self-tracking.
var Staging = !strings.HasSuffix(baseURL, "//analytics.bythewood.me")

// collectorID is empty on staging, so the snippet renders nothing rather than
// posting an id that database has no row for.
func collectorID() string {
	if Staging {
		return ""
	}
	return analyticsID
}

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/analytics.bythewood.me"
