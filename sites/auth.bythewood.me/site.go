package main

import "strings"

// Identity is hardcoded. Only the secrets come from the environment, and this
// site reads two: the ntfy publish token and, optionally, the cookie domain.
const (
	baseURL    = "https://auth.bythewood.me"
	siteName   = "Auth"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"

	// Must match the first hostname label, like every other source label.
	selfSource = "auth"
)

// Staging is true on any hostname but the real one, and keeps it out of search results.
var Staging = !strings.HasSuffix(baseURL, "//auth.bythewood.me")

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/auth.bythewood.me"
