package main

import "strings"

// Identity is hardcoded, never configured. Unlike the other sites here there is
// no public half: every route is behind the session, because a page carrying an
// address, a budget and a child's school run is not one to publish.
const (
	baseURL    = "https://house.bythewood.me"
	siteName   = "House"
	githubUser = "overshard"
)

// Staging is true on any hostname but the real one. It drives the noindex,
// which this site carries everywhere regardless.
var Staging = !strings.HasSuffix(baseURL, "//house.bythewood.me")

// The one Vite entry this site has.
const appEntry = "static_src/app/index.js"

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/house.bythewood.me"
