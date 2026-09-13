package main

import (
	"fmt"
	"strings"
)

// Every rule produces a Flag rather than a yes or no, and the flag's severity is
// a config value. Five of these are stated dealbreakers, so they exclude. The
// rest are wants: a corner lot or an old doublewide does not sink an otherwise
// perfect house, it is points off and a chip on the card saying why.
//
// Flipping any rule between the two is one word in config, because which of them
// is a dealbreaker is the reader's call and it moves as they see houses.
type Flag struct {
	Key     string  `json:"key"`
	Why     string  `json:"why"`
	Exclude bool    `json:"exclude"`
	Penalty float64 `json:"penalty"`
}

// The rule keys, which are what config's severity and penalty maps are keyed on.
const (
	ruleOverPrice     = "over_price"
	ruleFloodZone     = "flood_zone"
	ruleWaterBuffer   = "water_buffer"
	ruleMainRoad      = "main_road"
	ruleOutsideBand   = "outside_band"
	ruleNoCoordinate  = "no_coordinate"
	ruleManufactured  = "manufactured"
	ruleCorner        = "corner_lot"
	ruleNextToHighway = "next_to_highway"
	ruleTwoStops      = "two_stop_morning"
)

// The defaults. Exclude for the five stated dealbreakers, points off for the
// wants, and the sizes are rough on purpose: a doublewide sinking twenty points
// is meant to put it below every site-built house without hiding it.
var defaultSeverity = map[string]string{
	ruleOverPrice:     "exclude",
	ruleFloodZone:     "exclude",
	ruleWaterBuffer:   "exclude",
	ruleMainRoad:      "exclude",
	ruleOutsideBand:   "exclude",
	ruleNoCoordinate:  "exclude",
	ruleManufactured:  "penalty",
	ruleCorner:        "penalty",
	ruleNextToHighway: "penalty",
	ruleTwoStops:      "penalty",
}

var defaultPenalties = map[string]float64{
	ruleManufactured:  20,
	ruleCorner:        10,
	ruleNextToHighway: 8,
	ruleTwoStops:      15,
}

func (f Filters) severityOf(rule string) (exclude bool, penalty float64) {
	sev := f.Severity[rule]
	if sev == "" {
		sev = defaultSeverity[rule]
	}
	if sev == "exclude" {
		return true, 0
	}
	p, ok := f.Penalties[rule]
	if !ok {
		p = defaultPenalties[rule]
	}
	return false, p
}

// Flags runs every rule and returns what fired. All of them run, so a listing
// that breaks three says all three: a filter you cannot audit is a filter that
// quietly hides the right house.
func Flags(cfg Config, a *Assessment) []Flag {
	var out []Flag
	l := a.Listing
	f := cfg.Filters

	add := func(rule, why string) {
		exclude, penalty := f.severityOf(rule)
		out = append(out, Flag{Key: rule, Why: why, Exclude: exclude, Penalty: penalty})
	}

	if l.Lat == 0 || l.Lon == 0 {
		add(ruleNoCoordinate, "no coordinate, so nothing about it could be measured")
		return out
	}

	if l.Price > f.MaxPrice {
		add(ruleOverPrice, fmt.Sprintf("asking %s, over the %s ceiling", money(l.Price), money(f.MaxPrice)))
	}

	// Construction and age together. An old house with good bones is fine and an
	// old manufactured one is not, so age alone is not the test.
	if kind, manufactured := manufacturedKind(l, f); manufactured {
		switch {
		case kind == "singlewide":
			add(ruleManufactured, "singlewide manufactured home")
		case l.YearBuilt > 0 && l.YearBuilt < f.ManufacturedMinYear:
			add(ruleManufactured, fmt.Sprintf("%s built %d, before the %d cutoff", kind, l.YearBuilt, f.ManufacturedMinYear))
		case l.YearBuilt == 0:
			add(ruleManufactured, kind+" with no year built reported")
		}
	}

	if a.Flood.SFHA {
		add(ruleFloodZone, "FEMA flood zone "+orUnnamed(a.Flood.Zone, "an SFHA"))
	}
	// One test per kind of water, against its own buffer. A buffer of zero means
	// that kind never excludes and only moves the flood score, which is where the
	// ditches belong.
	if a.Flood.Measured {
		for _, w := range []struct {
			dist   float64
			buffer float64
			name   string
			what   string
		}{
			{a.Flood.PerennialFeet, f.WaterBufferFeet, a.Flood.PerennialName, "water that runs year round"},
			{a.Flood.IntermittentFeet, f.IntermittentBufferFeet, a.Flood.IntermittentName, "a wet-weather stream"},
			{a.Flood.DitchFeet, f.DitchBufferFeet, "a mapped ditch", "a ditch"},
		} {
			if w.buffer > 0 && w.dist != notFound && w.dist < w.buffer {
				add(ruleWaterBuffer, fmt.Sprintf("%s is about %s from the house, closer than we wanted for %s",
					orUnnamed(w.name, "water on the map"), feet(w.dist), w.what))
			}
		}
	}

	if why, bad := frontsMainRoad(cfg, a.Road); bad {
		add(ruleMainRoad, why)
	}

	// Road on more than one side. Being off a small road that leads to a main one
	// is the good case and is not this: this is the house with a road down the
	// side as well as the front.
	if a.Road.Corner() {
		add(ruleCorner, "road on more than one side, "+strings.Join(a.Road.NearbyRoads, " and "))
	}

	// Right beside the interstate, which the class test above only catches when the
	// frontage itself is the highway. This is the house a field away from it, which
	// is quieter than fronting it and is still not quiet.
	if a.Road.Measured && a.Road.HighwayFeet != notFound && a.Road.HighwayFeet < f.HighwayTooCloseFeet {
		add(ruleNextToHighway, fmt.Sprintf("%s from %s, close enough to hear it",
			feet(a.Road.HighwayFeet), orUnnamed(a.Road.HighwayName, "an interstate")))
	}

	if a.Morning.OppositeWays {
		add(ruleTwoStops, "the zoned elementary and middle are in opposite directions, a two stop morning")
	}

	// A stretch listing is not flagged. It goes in its own section with its drive
	// time on it, because the justification is a hybrid schedule to be negotiated
	// rather than a house that failed a test.
	if why, outside := outsideBand(cfg, a); outside && !a.Stretch {
		add(ruleOutsideBand, why)
	}

	return out
}

// Excluded splits the flags. Anything with Exclude goes to the drawer, and
// everything else stays in the grid with its points off.
func Excluded(flags []Flag) []string {
	var out []string
	for _, f := range flags {
		if f.Exclude {
			out = append(out, f.Why)
		}
	}
	return out
}

func Penalties(flags []Flag) []Flag {
	var out []Flag
	for _, f := range flags {
		if !f.Exclude && f.Penalty > 0 {
			out = append(out, f)
		}
	}
	return out
}

func PenaltyTotal(flags []Flag) float64 {
	var total float64
	for _, f := range Penalties(flags) {
		total += f.Penalty
	}
	return total
}

// manufacturedKind reports what the listing's own style and type fields say the
// construction is. MLS exports spell this six ways, which is why the list is
// config and not a switch.
func manufacturedKind(l Listing, f Filters) (string, bool) {
	hay := strings.ToLower(l.Style + " " + l.PropertyType + " " + l.Remarks)
	for _, needle := range f.ManufacturedStyles {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle == "" || !strings.Contains(hay, needle) {
			continue
		}
		switch needle {
		case "singlewide", "single wide":
			return "singlewide", true
		case "doublewide", "double wide":
			return "doublewide", true
		case "modular":
			// Modular is built to the state residential code on a permanent
			// foundation, so it is not the thing being avoided here. Recognised,
			// and not flagged.
			return "modular", false
		}
		return "manufactured home", true
	}
	return "", false
}

// frontsMainRoad is the frontage test, and distance is what makes it a frontage
// test rather than a proximity one. Off a small road that leads to a main road is
// the arrangement wanted, so a primary road four hundred feet away says nothing
// about this driveway.
func frontsMainRoad(cfg Config, r RoadResult) (string, bool) {
	cutoff := cfg.Filters.AADTCutoff
	radius := cfg.Filters.AADTRadiusFt
	class := strings.ToLower(r.Class)

	if r.AADT >= cutoff && r.AADTFeet != notFound && r.AADTFeet <= radius {
		return fmt.Sprintf("the house faces %s, with about %s cars a day on it",
			orUnnamed(r.AADTRoute, r.RoadName), commaInt(r.AADT)), true
	}

	// The class only speaks for the frontage at frontage distance. NCDOT counts
	// the highway and not the lane beside it, and OSM draws both.
	if r.ClassFeet == notFound || r.ClassFeet > radius {
		return "", false
	}

	for _, bad := range cfg.Filters.RoadClasses {
		if strings.ToLower(strings.TrimSpace(bad)) != class {
			continue
		}
		// Secondary is judged on traffic rather than on the label, because plenty
		// of secondary roads here are two quiet lanes.
		if class == "secondary" {
			if r.AADT >= cutoff {
				return fmt.Sprintf("the house faces a through road with about %s cars a day on it", commaInt(r.AADT)), true
			}
			return "", false
		}
		return fmt.Sprintf("the house faces %s, which is a %s road", orUnnamed(r.RoadName, r.AADTRoute), class), true
	}
	return "", false
}

// outsideBand is the geography test: a drive-time band from the office, plus the
// stated north bound, plus an arc of the compass. Not a lat/lon box, because the
// roads here do not run straight, so a box includes places you cannot reach in the
// time and excludes places you can.
func outsideBand(cfg Config, a *Assessment) (string, bool) {
	g := cfg.Geography

	if a.Listing.Lat > g.NorthLatCap {
		return "further north than we said we would look", true
	}
	if !g.InBearingArc(a.Bearing) {
		return "the wrong side of work, we are only looking south through west", true
	}

	// A routing outage must not rule a house out. The latitude and the bearing are
	// arithmetic on a coordinate we already have, so they still apply, but with no
	// drive time there is nothing to compare against the band. Excluding here put
	// every house in the drawer the moment the routing service had a bad hour,
	// which is a failed lookup hiding a house rather than merely scoring it.
	mins := a.Morning.Commute.Minutes
	if mins == 0 {
		return "", false
	}
	if mins > g.StretchMinutes {
		return fmt.Sprintf("%s from the office, past the %s stretch limit",
			fmtMinutes(mins), fmtMinutes(g.StretchMinutes)), true
	}
	if mins > g.MaxMinutes {
		return fmt.Sprintf("%s from the office, past the %s band", fmtMinutes(mins), fmtMinutes(g.MaxMinutes)), true
	}
	return "", false
}

// IsStretch marks the band between the normal limit and the stretch limit. These
// get their own section and never mix into the main grid, so a long commute
// cannot crowd out a close house on score alone.
func IsStretch(cfg Config, a *Assessment) bool {
	mins := a.Morning.Commute.Minutes
	return mins > cfg.Geography.MaxMinutes && mins <= cfg.Geography.StretchMinutes
}
