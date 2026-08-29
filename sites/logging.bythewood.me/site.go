package main

import "strings"

// Identity, stated once and not made configurable. Hardcode identity, never
// hardcode credentials: the only environment variable this app reads is
// LOGGING_PASSWORD, because it is the only value that is actually a secret.
//
// There is deliberately no variable for the ingest endpoint either. It is a
// constant in web/shipper.go on the sending side, which is what lets the two
// FROM scratch sites keep reading zero environment variables.
const (
	baseURL    = "https://logging.bythewood.me"
	siteName   = "Logging"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"

	// The name this site files its own records under, matching its container
	// and every other source label: the first label of the hostname.
	selfSource = "logging"

	// The analytics property this site is tracked under, hardcoded exactly as
	// the other four sites hardcode theirs. Identity, not a credential: the id
	// is in the page source of every site here by design.
	analyticsID = "4edc6fc5-97da-4e65-86c8-2b1cee18c44e"
)

// Staging keeps a test hostname out of search results, and out of the index.
var Staging = !strings.HasSuffix(baseURL, "//logging.bythewood.me")

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/logging.bythewood.me"
