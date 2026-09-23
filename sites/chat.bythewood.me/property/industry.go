package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Whatever you would not want to find out about after closing, like a data
// centre going up behind the tree line, a landfill, a quarry, a plant with an
// air permit or a Superfund site.
//
// Two sources, since neither covers it alone. OpenStreetMap has the landfills,
// quarries, data centres and power plants that somebody mapped, and EPA's
// facility layers have every plant that holds an air permit and every Superfund
// site whether or not anybody mapped it.
const industrySearchFeet = 4 * 5280

// EPA's permit points are dense in a mill town, so their circle is smaller. A
// furniture plant four miles off is the next town over.
const epaSearchMiles = 3

const epaFacilities = "https://geopub.epa.gov/arcgis/rest/services/EMEF/efpoints/MapServer"

// Layer 0 is Superfund and 3 is ICIS-AIR. Layer 1, the toxics inventory, was left
// out because a plant that closed in 2005 is still on it with nothing saying so.
const (
	epaSuperfundLayer = 0
	epaAirLayer       = 3
)

// Bump when what is cached changes shape, so an older row is fetched again rather
// than decoded into zeroes.
const industryCacheKind = "industry1"

// How many of each kind are worth naming. A town with nine furniture plants reads
// the same with three named and a count.
const industryPerKind = 3

type IndustrySite struct {
	Kind  string  `json:"kind"`
	Name  string  `json:"name"`
	Miles float64 `json:"miles"`
}

type IndustryResult struct {
	Measured bool `json:"measured"`
	// Nearest few of each kind, closest first overall.
	Sites []IndustrySite `json:"sites"`
	// How many of each kind were inside the search, including the unnamed ones.
	Counts  map[string]int `json:"counts"`
	Partial bool           `json:"partial"`
}

type Industry struct {
	db    *sql.DB
	roads *Roads // shares the Overpass mirrors and their guards
	epa   *Guard
}

func NewIndustry(db *sql.DB, roads *Roads) *Industry {
	return &Industry{
		db:    db,
		roads: roads,
		epa: NewGuard(db, "epa-facilities", GuardOpts{
			MinInterval: 1500 * time.Millisecond,
			Budget:      60,
			Timeout:     30 * time.Second,
		}),
	}
}

func (in *Industry) Lookup(ctx context.Context, lat, lon float64) (IndustryResult, error) {
	// Three decimal places is about a hundred yards, which moves nothing that is
	// measured in miles.
	key := fmt.Sprintf("%.3f,%.3f", lat, lon)

	var payload string
	err := in.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = ? AND key = ?`, industryCacheKind, key).Scan(&payload)
	if err == nil {
		var cached IndustryResult
		if json.Unmarshal([]byte(payload), &cached) == nil && !cached.Partial {
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return IndustryResult{}, err
	}

	out := IndustryResult{Measured: true, Counts: map[string]int{}}
	var all []IndustrySite
	var firstErr error

	if sites, err := in.osm(ctx, lat, lon); err != nil {
		out.Partial = true
		firstErr = err
	} else {
		all = append(all, sites...)
	}
	for _, layer := range []int{epaSuperfundLayer, epaAirLayer} {
		sites, err := in.epaLayer(ctx, layer, lat, lon)
		if err != nil {
			out.Partial = true
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = append(all, sites...)
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].Miles < all[j].Miles })
	named := map[string]int{}
	for _, s := range all {
		out.Counts[s.Kind]++
		if named[s.Kind] >= industryPerKind {
			continue
		}
		named[s.Kind]++
		out.Sites = append(out.Sites, s)
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := in.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES (?,?,?,?)`,
		industryCacheKind, key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, firstErr
}

func (in *Industry) osm(ctx context.Context, lat, lon float64) ([]IndustrySite, error) {
	south, west, north, east := boxAround(lat, lon, industrySearchFeet)
	box := fmt.Sprintf("(%.4f,%.4f,%.4f,%.4f)", south, west, north, east)

	// The highway clause is there so a mirror's answer is never empty. The mirror
	// code reads an empty list as a mirror with no data, and a house with nothing
	// industrial for four miles is the answer everybody hopes for.
	q := fmt.Sprintf("[out:json][timeout:%d];(", int(overpassAttempt.Seconds())) +
		`nwr["telecom"="data_center"]` + box + ";" +
		`nwr["building"="data_center"]` + box + ";" +
		`nwr["man_made"~"^(works|wastewater_plant)$"]` + box + ";" +
		`nwr["landuse"~"^(landfill|quarry)$"]` + box + ";" +
		`nwr["power"="plant"]` + box + ";" +
		`nwr["power"="substation"]["substation"="transmission"]` + box + ";" +
		`nwr["amenity"="prison"]` + box + ";" +
		`nwr["aeroway"="aerodrome"]` + box + ";" +
		`nwr["sport"~"^(motor|motocross|shooting)$"]` + box + ";" +
		`nwr["landuse"="industrial"]["name"]` + box + ";" +
		`way["highway"~"^(primary|secondary|tertiary)$"]` + box + ";" +
		");out tags center;"

	body, err := in.roads.overpassQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	var res struct {
		Elements []struct {
			Tags   map[string]string `json:"tags"`
			Lat    float64           `json:"lat"`
			Lon    float64           `json:"lon"`
			Center *struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"center"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	var out []IndustrySite
	for _, el := range res.Elements {
		kind := industryKind(el.Tags)
		if kind == "" {
			continue
		}
		elat, elon := el.Lat, el.Lon
		if el.Center != nil {
			elat, elon = el.Center.Lat, el.Center.Lon
		}
		if elat == 0 && elon == 0 {
			continue
		}
		miles := haversineFeet(lat, lon, elat, elon) / 5280
		// The box's corners are further out than its sides.
		if miles > industrySearchFeet/5280 {
			continue
		}
		out = append(out, IndustrySite{Kind: kind, Name: el.Tags["name"], Miles: miles})
	}
	return out, nil
}

// industryKind maps an element back to the arm of the union that returned it.
// Order matters, since a data centre is often also tagged as industrial land.
func industryKind(t map[string]string) string {
	switch {
	case t["telecom"] == "data_center" || t["building"] == "data_center":
		return "data centre"
	case t["landuse"] == "landfill":
		return "landfill"
	case t["landuse"] == "quarry":
		return "quarry"
	case t["man_made"] == "wastewater_plant":
		return "sewage works"
	case t["power"] == "plant":
		// A solar farm is a power plant to OSM and is nothing like a gas turbine
		// to live beside.
		if t["plant:source"] == "solar" {
			return "solar farm"
		}
		return "power plant"
	case t["power"] == "substation":
		return "transmission substation"
	case t["amenity"] == "prison":
		return "prison"
	case t["aeroway"] == "aerodrome":
		return "airfield"
	case t["sport"] == "motor" || t["sport"] == "motocross":
		return "motor racing track"
	case t["sport"] == "shooting":
		return "shooting range"
	case t["man_made"] == "works":
		return "factory"
	case t["landuse"] == "industrial":
		return "industrial land"
	}
	return ""
}

func (in *Industry) epaLayer(ctx context.Context, layer int, lat, lon float64) ([]IndustrySite, error) {
	v := url.Values{
		"geometry":       {fmt.Sprintf("%.6f,%.6f", lon, lat)},
		"geometryType":   {"esriGeometryPoint"},
		"inSR":           {"4326"},
		"distance":       {fmt.Sprint(epaSearchMiles)},
		"units":          {"esriSRUnit_StatuteMile"},
		"spatialRel":     {"esriSpatialRelIntersects"},
		"outFields":      {"primary_name,latitude,longitude"},
		"returnGeometry": {"false"},
		"f":              {"json"},
	}
	var res esriQueryResult
	if err := in.epa.Get(ctx, fmt.Sprintf("%s/%d/query?%s", epaFacilities, layer, v.Encode()), &res); err != nil {
		return nil, err
	}
	if res.Error != nil {
		return nil, fmt.Errorf("epa layer %d: %s", layer, res.Error.Message)
	}
	kind := "plant with an air permit"
	if layer == epaSuperfundLayer {
		kind = "Superfund site"
	}
	var out []IndustrySite
	for _, f := range res.Features {
		name := attrString(f.Attributes, "primary_name")
		// ICIS-AIR keeps closed permits and says so only in the name.
		if layer == epaAirLayer && strings.Contains(strings.ToUpper(name), "INACTIVE") {
			continue
		}
		flat, ok1 := attrFloat(f.Attributes, "latitude")
		flon, ok2 := attrFloat(f.Attributes, "longitude")
		if !ok1 || !ok2 {
			continue
		}
		out = append(out, IndustrySite{
			Kind:  kind,
			Name:  titleAddress(name),
			Miles: haversineFeet(lat, lon, flat, flon) / 5280,
		})
	}
	return out, nil
}

// Line is the one sentence the summary carries.
func (r IndustryResult) Line() string {
	if !r.Measured {
		return "not measured"
	}
	if len(r.Sites) == 0 {
		if r.Partial {
			return "could not be looked up just now"
		}
		return "nothing industrial mapped within four miles"
	}
	// The nearest of each kind, so three plants in town cannot push the landfill
	// out of the sentence.
	var parts []string
	seen := map[string]bool{}
	for _, s := range r.Sites {
		if len(parts) == 4 {
			break
		}
		if seen[s.Kind] {
			continue
		}
		seen[s.Kind] = true
		parts = append(parts, s.describe())
	}
	return "closest: " + strings.Join(parts, "; ")
}

func (s IndustrySite) describe() string {
	miles := trimFloat(math.Round(s.Miles*10) / 10)
	if s.Name == "" {
		return fmt.Sprintf("a %s, %s miles", s.Kind, miles)
	}
	return fmt.Sprintf("%s (%s), %s miles", s.Name, s.Kind, miles)
}
