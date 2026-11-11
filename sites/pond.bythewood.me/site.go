package main

// Identity is hardcoded, never configured. Only secrets and paths a Dockerfile
// sets come from the environment.
const (
	baseURL  = "https://pond.bythewood.me"
	siteName = "pond"
	siteDesc = "A pond where everybody gets a frog of their own. Raise it, find it hats, and take it diving for pebbles."

	// Must match the first hostname label, like every other source label.
	selfSource = "pond"

	// The property id from analytics.bythewood.me. It is in the page source by
	// design, so it is not a secret.
	analyticsID = "e89471b9-1904-4761-a53f-1d92ef294569"
)

const sourceURL = "https://github.com/overshard/orchard/tree/main/sites/pond.bythewood.me"
