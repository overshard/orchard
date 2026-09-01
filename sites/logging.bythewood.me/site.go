package main

import "strings"

// Identity is hardcoded. Signing in happens on auth.bythewood.me, so the only
// environment variable this app reads is the ntfy token its alerts publish with.
const (
	baseURL    = "https://logging.bythewood.me"
	siteName   = "Logging"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"

	// Must match the first hostname label, like every other source label.
	selfSource = "logging"

	analyticsID = "4edc6fc5-97da-4e65-86c8-2b1cee18c44e"
)

// Staging is true on any hostname but the real one, and keeps it out of search results.
var Staging = !strings.HasSuffix(baseURL, "//logging.bythewood.me")

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/logging.bythewood.me"
