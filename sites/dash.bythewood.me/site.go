package main

import "strings"

// Identity is hardcoded. This site reads no environment variable at all in
// normal operation, because nothing it serves is a secret: every panel is
// either public market data, a public feed, or the up/down state of a site
// that is already on the public internet.
const (
	baseURL  = "https://dash.bythewood.me"
	siteName = "Dash"

	// What the browser tab reads. siteName stays "Dash" for the structured data
	// and the feed, since this is the tab's own wording and not the site's name.
	titleBase  = "DASH · BYTHEWOOD.ME"
	authorName = "Isaac Bythewood"
	githubUser = "overshard"

	// Must match the first hostname label, like every other source label.
	selfSource = "dash"

	// analyticsID is in the page source of every site; it is identity, not a
	// credential. Its own property, since a copied one silently files this
	// site's traffic under whichever site it was copied from.
	analyticsID = "0b09f0d1-3016-411c-a003-f15ea72b20fa"
)

// Staging is true on any hostname but the real one, and keeps it out of search results.
var Staging = !strings.HasSuffix(baseURL, "//dash.bythewood.me")

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/dash.bythewood.me"

// Weather is for where Isaac is, so the coordinates are a constant rather than
// a lookup. Yadkin Valley, North Carolina, rounded to two places because
// open-meteo snaps to its own grid anyway and a precise home address in a
// public repository is worth nothing to anyone.
const (
	weatherLat   = 36.13
	weatherLon   = -80.62
	weatherPlace = "Yadkin Valley, NC"
)
