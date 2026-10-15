package main

import "strings"

// Identity is hardcoded, never configured. Only secrets and paths a Dockerfile
// sets come from the environment.
const (
	baseURL  = "https://__SITE__"
	siteName = "__LABEL__"
	siteDesc = "What __SITE__ is for, in a sentence."

	// Must match the first hostname label, like every other source label.
	selfSource = "__LABEL__"

	// The property id from analytics.bythewood.me, and empty until one is made
	// there. It is in the page source by design, so it is not a secret.
	analyticsID = ""
)

// Staging is true on any hostname but the real one, and keeps it out of search
// results.
var Staging = !strings.HasSuffix(baseURL, "//__SITE__")

const sourceURL = "https://github.com/overshard/orchard/tree/main/sites/__SITE__"
