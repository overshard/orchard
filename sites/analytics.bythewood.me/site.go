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
// baseURL went to the real hostname at cutover on 2026-08-27. Staging below
// derives from it, so flipping this one line also turned off the noindex, the
// robots.txt Disallow and the analytics collector.
const (
	baseURL    = "https://analytics.bythewood.me"
	siteName   = "Analytics"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"
)

// Staging keeps the test hostname out of search results.
//
// It deliberately does NOT gate self-tracking, which is what it did at first.
// That was reasoning from a wrong premise: the worry was staging filing
// phantom sessions into the property the production dashboard reads, but the
// staging instance has its own database and its own Proprium row, and the
// collector posts same-origin, so there is no path between the two. Gating it
// bought nothing and cost the ability to see the collector work before
// cutover.
var Staging = !strings.HasSuffix(baseURL, "//analytics.bythewood.me")

// sourceURL points at this site inside the monorepo rather than at the old
// per-project repo, which is where the footer link used to go and which is
// about to be archived.
const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/analytics.bythewood.me"
