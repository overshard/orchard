package main

import "strings"

// Identity, stated once and not made configurable.
//
// decisions/0008 draws the line: hardcode identity, never hardcode
// credentials. The Rust version read STATUS_ROOT, STATUS_DATA_DIR, BASE_URL,
// PORT and STATUS_COOKIE_SECRET out of a .env through dotenvy. Every one of
// those was product scaffolding for a deployer who does not exist.
//
// BASE_URL was the load-bearing one here, as it was in analytics, and it
// degraded quietly rather than loudly: it defaulted to the empty string, and
// the two places that need an absolute origin just emitted a relative one.
// sitemap.xml shipped `<loc>/</loc>`, which is invalid, and the alert mail
// linked at `/<uuid>` with no host, which is unclickable in a mail client.
// Both are now built from the constant below, so neither can be unset.
//
// What is left in the environment is exactly one value, STATUS_PASSWORD.
//
// baseURL is next-status.bythewood.me while the Go rewrite is proved out
// behind the tunnel, the same as the three sites before it. Cutover is this
// one line. The hyphen is not a style choice: Cloudflare's free Universal SSL
// signs the apex and exactly one wildcard level, so next.status.bythewood.me
// has no certificate and fails the TLS handshake looking like a dead tunnel.
const (
	baseURL    = "https://next-status.bythewood.me"
	siteName   = "Status"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"
)

// Staging keeps the test hostname out of search results.
//
// Unlike analytics there is nothing here to cross-contaminate: status writes
// only what it probes, and it probes whatever properties its own database
// lists. A staging instance with its own database probes its own properties.
var Staging = !strings.HasSuffix(baseURL, "//status.bythewood.me")

// sourceURL points at this site inside the monorepo rather than at the old
// per-project repo, which is where the footer link used to go and which is
// about to be archived.
const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/status.bythewood.me"
