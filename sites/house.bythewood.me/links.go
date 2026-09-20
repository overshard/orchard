package main

import (
	"fmt"
	"net/url"
	"strings"
)

// Links out to the places worth looking at a house from. Every one is built from
// the address and the coordinate, and nothing here asks anybody's server
// anything. They are hrefs, and the browser follows them when somebody clicks.
//
// Looking the address up on a search engine from here would mean a request per
// house to somebody who does not want the traffic, and it would be blocked.
type OutLink struct {
	Label string
	URL   string
	Note  string
	// Which mark to draw beside it. The note is a title attribute rather than a
	// line of its own, because six links each explaining themselves in a sentence
	// is most of a screen for what is six links.
	Icon string
}

// Links is everywhere worth a look for one address.
func (c Card) Links() []OutLink {
	var out []OutLink

	if c.URL != "" {
		out = append(out, OutLink{"Listing", c.URL, "wherever this came from", "page"})
	}

	if c.Lat != 0 || c.Lon != 0 {
		lat, lon := c.mapPoint()
		out = append(out,
			OutLink{
				Icon:  "map",
				Label: "Map",
				URL: fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.6f&mlon=%.6f#map=17/%.6f/%.6f",
					lat, lon, lat, lon),
				Note: "the roads, as the map itself has them",
			},
			OutLink{
				Icon:  "satellite",
				Label: "Satellite",
				URL: fmt.Sprintf("https://www.google.com/maps/@%.6f,%.6f,300m/data=!3m1!1e3",
					lat, lon),
				Note: "what the lot and the roof actually look like",
			},
			OutLink{
				Icon:  "street",
				Label: "Street view",
				// The older cbll form rather than map_action=pano, which wants a
				// panorama close to the point it is given and shows black when
				// there is not one. cbll snaps to the nearest camera on the road.
				URL: fmt.Sprintf("https://www.google.com/maps?q=&layer=c&cbll=%.6f,%.6f",
					c.Lat, c.Lon),
				Note: "the drive past, without the drive",
			},
		)
	}

	full := c.searchAddress()
	if full == "" {
		return out
	}

	// Both of these take an address in the path and land on the property when they
	// have it and on a search for the street when they do not, which is the right
	// behaviour either way.
	out = append(out,
		OutLink{
			Icon:  "page",
			Label: "Zillow",
			URL:   "https://www.zillow.com/homes/" + url.PathEscape(dashed(full)) + "_rb/",
			Note:  "their page for it, or the street if they have no page",
		},
		OutLink{
			Icon:  "page",
			Label: "Realtor",
			URL:   "https://www.realtor.com/realestateandhomes-search/" + url.PathEscape(realtorSlug(c)),
			Note:  "same again",
		},
		OutLink{
			Icon:  "page",
			Label: "Redfin",
			URL:   "https://www.redfin.com/zipcode/" + url.PathEscape(c.Zip),
			Note:  "and their download button, which is how listings get in here",
		},
	)
	return out
}

// mapPoint is the middle of the lot when the county knows it. The geocoder
// interpolates along the street centreline, so its point is in the road and a
// pin on it can sit a few doors down from the house.
func (c Card) mapPoint() (float64, float64) {
	if c.ParcelLat != 0 && c.ParcelLon != 0 && !c.ParcelMismatch() {
		return c.ParcelLat, c.ParcelLon
	}
	return c.Lat, c.Lon
}

func (c Card) searchAddress() string {
	parts := []string{c.Address}
	if c.City != "" {
		parts = append(parts, c.City)
	}
	if c.State != "" {
		parts = append(parts, c.State)
	}
	if c.Zip != "" {
		parts = append(parts, c.Zip)
	}
	joined := strings.TrimSpace(strings.Join(parts, " "))
	if strings.TrimSpace(c.Address) == "" {
		return ""
	}
	return joined
}

// Zillow wants the whole thing hyphenated.
func dashed(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, ",", "")), "-")
}

// Realtor splits the address from the town with underscores and hyphenates
// within each part.
func realtorSlug(c Card) string {
	parts := []string{dashed(c.Address)}
	for _, p := range []string{c.City, c.State, c.Zip} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, dashed(p))
		}
	}
	return strings.Join(parts, "_")
}
