package main

import "strings"

// Identity is hardcoded. Every panel is public data, so the environment only
// carries the model gateway and where the data lives.
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

	// analyticsID is in the page source of every site, so it names a site and
	// authorises nothing. Its own property, since a copied one files this
	// site's traffic under the site it was copied from.
	analyticsID = "0b09f0d1-3016-411c-a003-f15ea72b20fa"
)

// Staging is true on any hostname but the real one, and keeps it out of search results.
var Staging = !strings.HasSuffix(baseURL, "//dash.bythewood.me")

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/dash.bythewood.me"

// Weather is for where Isaac is, so the coordinates are a constant. Yadkin
// Valley, North Carolina, rounded to two places since open-meteo snaps to its
// own grid.
const (
	weatherLat   = 36.13
	weatherLon   = -80.62
	weatherPlace = "Yadkin Valley, NC"

	// A severe weather alert covers a dozen counties and lists them
	// alphabetically, so this pulls the one worth reading to the front.
	homeCounty = "YADKIN"
)
