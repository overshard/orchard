package main

import "strings"

const (
	baseURL    = "https://auth.bythewood.me"
	siteName   = "Auth"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"

	// Must match the first hostname label, like every other source label.
	selfSource = "auth"

	// Not a credential, it's public in every page's source.
	analyticsID = "75a24b68-7734-4337-bb7a-9265f219f397"
)

// Staging is true on any hostname but the real one, and keeps it out of search results.
var Staging = !strings.HasSuffix(baseURL, "//auth.bythewood.me")

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/auth.bythewood.me"
