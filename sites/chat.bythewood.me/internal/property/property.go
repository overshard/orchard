// Package property answers what a house at an address would be like to live in
// and what it would cost, from public records and nothing else. Every backend is
// keyless except the FBI's crime API.
package property

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// band maps a measurement onto 0 to 1 between the best and the worst value worth
// distinguishing.
func band(v, best, worst float64) float64 {
	if best == worst {
		return 0.5
	}
	t := (v - worst) / (best - worst)
	return math.Max(0, math.Min(1, t))
}

// bearingDeg is the initial bearing, clockwise from true north.
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

// mustJSON is for a struct built here that can't fail to marshal, and an empty
// object is the right thing to store if one ever does.
func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
func round2(f float64) float64 { return math.Round(f*100) / 100 }

// num reads $250,000, "1.25 ac" and a bare float alike.
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

// addressKey makes "402 Sample Road" and "402 Sample Rd." one house, and a
// cached report is keyed on it.
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

// Length alone isn't the test, since "Rd" is two letters and isn't a direction.
var directions = map[string]bool{
	"n": true, "s": true, "e": true, "w": true,
	"ne": true, "nw": true, "se": true, "sw": true,
	// The tax roll writes "358 WEST MAIN AVENUE" against a site address of
	// "358 MAIN AVE", and reading WEST as the street name moved the owner.
	"north": true, "south": true, "east": true, "west": true,
	"northeast": true, "northwest": true, "southeast": true, "southwest": true,
}

// The geocoder and the tax roll answer in capitals.
func titleAddress(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		trimmed := strings.Trim(w, ".")
		// A spelled out direction is a word and reads as one.
		if directions[trimmed] && len(trimmed) <= 2 {
			words[i] = strings.ToUpper(w)
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
