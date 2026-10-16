package main

// Identity is hardcoded since an empty BASE_URL would ship a site whose own
// tracking silently did nothing.
const (
	baseURL    = "https://analytics.bythewood.me"
	siteName   = "Analytics"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"

	// Not a credential, this id is public in the page source.
	analyticsID = "580ebab0-3d14-4a20-89da-d57fc3d7d9e8"
)

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/analytics.bythewood.me"
