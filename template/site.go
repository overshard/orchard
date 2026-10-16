package main

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

const sourceURL = "https://github.com/overshard/orchard/tree/main/sites/__SITE__"
