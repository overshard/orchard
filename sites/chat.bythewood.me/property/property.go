// Package property answers what a house at an address would actually be like to
// live in and what it would cost, from public records and nothing else. Flood
// zone, the road it fronts, the schools it is zoned for, how flat the yard is,
// what the county has it assessed at, who else lives on the street, the drive to
// work and back with the school run in it, and the all-in monthly under every
// loan a person round here can get.
//
// It came out of house.bythewood.me, which was a dashboard around this engine.
// Chat asks better questions of it than a grid of cards could, so the engine
// moved and the dashboard is going.
//
// Every backend is free and keyless bar one: the FBI's crime API wants a key,
// which is free and instant, and without it the crime figures say they have none
// rather than guessing.
package property

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// band maps a measurement onto 0 to 1 between the best and the worst value worth
// distinguishing, so a report can say "good" without the caller knowing the
// units.
func band(v, best, worst float64) float64 {
	if best == worst {
		return 0.5
	}
	t := (v - worst) / (best - worst)
	return math.Max(0, math.Min(1, t))
}

// bearingDeg is the initial compass bearing from one point to another, clockwise
// from true north.
func bearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	p := math.Pi / 180
	y := math.Sin((lon2-lon1)*p) * math.Cos(lat2*p)
	x := math.Cos(lat1*p)*math.Sin(lat2*p) -
		math.Sin(lat1*p)*math.Cos(lat2*p)*math.Cos((lon2-lon1)*p)
	return math.Mod(math.Atan2(y, x)/p+360, 360)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// mustJSON is for a struct built here that cannot fail to marshal. An error
// would mean a field type changed, and an empty object is the right thing to
// store either way.
func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
func round2(f float64) float64 { return math.Round(f*100) / 100 }

// num parses a number out of whatever a person or an export put in the field:
// $250,000 and "1.25 ac" and a bare float all have to come back as one.
func num(s string) float64 {
	var b strings.Builder
	seenDot := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' && !seenDot:
			seenDot = true
			b.WriteRune(r)
		case r == '-' && b.Len() == 0:
			b.WriteRune(r)
		}
	}
	v, err := strconv.ParseFloat(b.String(), 64)
	if err != nil {
		return 0
	}
	return v
}

// trimFloat writes a float without trailing zeroes, so 1.50 and 1.00 come out
// as 1.5 and 1.
func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

var streetAbbrev = map[string]string{
	"street": "st", "road": "rd", "drive": "dr", "lane": "ln", "avenue": "ave",
	"court": "ct", "circle": "cir", "place": "pl", "trail": "trl", "highway": "hwy",
	"boulevard": "blvd", "parkway": "pkwy", "terrace": "ter", "north": "n",
	"south": "s", "east": "e", "west": "w", "northeast": "ne", "northwest": "nw",
	"southeast": "se", "southwest": "sw",
}

// addressKey normalizes an address enough that "402 Sample Road" and
// "402 Sample Rd." are one house. It is what a cached report is keyed on, so
// asking about the same house twice in a conversation costs nothing.
func addressKey(addr, zip string) string {
	words := strings.Fields(strings.ToLower(addr))
	for i, w := range words {
		w = strings.Trim(w, ".,")
		if full, ok := streetAbbrev[w]; ok {
			w = full
		}
		words[i] = w
	}
	key := nonAlnum.ReplaceAllString(strings.Join(words, " "), "")
	return key + "|" + strings.TrimSpace(strings.SplitN(zip, "-", 2)[0])
}

// Directions stay in capitals and everything else is title cased. Length is not
// the test: "Rd" is two letters and is not a direction, which is how the first
// version of this produced "402 Sample RD".
var directions = map[string]bool{
	"n": true, "s": true, "e": true, "w": true,
	"ne": true, "nw": true, "se": true, "sw": true,
	// Spelled out too. The tax roll writes "358 WEST MAIN AVENUE" against a site
	// address of "358 MAIN AVE", and reading WEST as the street name said the
	// owner lived somewhere else.
	"north": true, "south": true, "east": true, "west": true,
	"northeast": true, "northwest": true, "southeast": true, "southwest": true,
}

// The geocoder shouts, and an address read back in capitals is being shouted at
// whoever asked.
func titleAddress(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		trimmed := strings.Trim(w, ".")
		// Only the abbreviations are shouted. A spelled out direction is a word
		// and reads as one.
		if directions[trimmed] && len(trimmed) <= 2 {
			words[i] = strings.ToUpper(w)
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
