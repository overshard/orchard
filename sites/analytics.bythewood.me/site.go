package main

import "strings"

// Identity, stated once and not made configurable.
//
// decisions/0008 draws the line: hardcode identity, never hardcode
// credentials. The Rust version read ANALYTICS_ROOT, ANALYTICS_DATA_DIR,
// BASE_URL and PORT out of a .env through dotenvy. Every one of those was
// product scaffolding for a deployer who does not exist, and BASE_URL was the
// worst of them: it was load bearing, because the collector snippet and the
// sitemap both need an absolute origin, so a blank .env silently shipped a
// site whose own tracking did nothing.
//
// What is left in the environment is exactly one value, ANALYTICS_PASSWORD,
// and it is a genuine secret. That is the whole point of the exercise: no
// config-or-credential gray zone left to misjudge.
//
// baseURL is next-analytics.bythewood.me while the Go rewrite is proved out
// behind the tunnel, the same as the two sites before it. Cutover is this one
// line. The hyphen is not a style choice: Cloudflare's free Universal SSL
// signs the apex and exactly one wildcard level, so next.analytics.bythewood.me
// has no certificate and fails the TLS handshake looking like a dead tunnel.
const (
	baseURL    = "https://next-analytics.bythewood.me"
	siteName   = "Analytics"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"
)

// Staging keeps the test hostname out of search results, and does one more
// thing here that it does not do on the other two sites: it decides whether
// this app tracks itself.
//
// The Proprium property exists so analytics measures its own usage. Pointed at
// a staging hostname it would file that traffic under the same property as the
// real site, so the production dashboard would show phantom sessions from a
// host nobody visits. Off until cutover.
var Staging = !strings.HasSuffix(baseURL, "//analytics.bythewood.me")

// sourceURL points at this site inside the monorepo rather than at the old
// per-project repo, which is where the footer link used to go and which is
// about to be archived.
const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/analytics.bythewood.me"
