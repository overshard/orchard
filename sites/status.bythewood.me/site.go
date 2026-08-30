package main

import "strings"

const (
	baseURL    = "https://status.bythewood.me"
	siteName   = "Status"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"

	// This site's own property in analytics.
	analyticsID = "231ffafe-7a96-46d3-8a55-92f460fd98fb"
)

// Staging keeps a non-production hostname out of search results.
var Staging = !strings.HasSuffix(baseURL, "//status.bythewood.me")

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/status.bythewood.me"
