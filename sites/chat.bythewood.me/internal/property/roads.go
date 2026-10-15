package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"
)

// NCDOT's traffic counts are measured but skip most small roads, and OSM's road
// class covers the street a house actually fronts, so neither is enough alone.
const (
	ncdotAADT = "https://services.arcgis.com/NuWFvHYDMVmmxMeM/arcgis/rest/services/NCDOT_AADT_Traffic_Segmentation/FeatureServer/2/query"

	// A rural driveway can run a few hundred feet.
	roadSearchFeet = 600

	// About two minutes of driving.
	rampSearchFeet = 10560

	// Bump when RoadResult changes shape, since an older row reads as a ramp
	// nought feet away and no roads near.
	roadCacheKind = "roadv2"
)

// The main Overpass instance answers 504 under load often enough that one
// endpoint means no road data. Each mirror here holds North Carolina, since some
// are regional extracts that answer 200 with nothing in it.
var overpassMirrors = []string{
	"https://overpass-api.de/api/interpreter",
	"https://z.overpass-api.de/api/interpreter",
	"https://lz4.overpass-api.de/api/interpreter",
	"https://overpass.kumi.systems/api/interpreter",
	"https://overpass.private.coffee/api/interpreter",
}

type RoadResult struct {
	AADT      int     // count on the nearest counted segment, 0 when none nearby
	AADTRoute string  // NCDOT's own route name for that segment
	AADTFeet  float64 // so a count on a road a mile off is visibly not the frontage
	Class     string  // the OSM highway value of the nearest classified way
	RoadName  string
	ClassFeet float64 // fronting a road and being near one differ by this

	// Inside the corner radius. Two distinct ones is a corner lot.
	NearbyRoads []string

	// The pavement is noise and safety, and the ramp is getting places, which
	// matters far less.
	HighwayFeet float64
	HighwayName string
	RampFeet    float64
	RampName    string

	// Without it the zero value reads as a ramp nought feet away.
	Measured bool

	Partial bool
}

func newRoadResult() RoadResult {
	return RoadResult{AADTFeet: notFound, ClassFeet: notFound, RampFeet: notFound, HighwayFeet: notFound}
}

func (r RoadResult) Corner() bool { return len(r.NearbyRoads) > 1 }

type mirror struct {
	url   string
	guard *Guard
}

type Roads struct {
	db       *sql.DB
	ncdot    *Guard
	overpass []mirror
	bad      map[string]bool
	corner   float64
}

func NewRoads(db *sql.DB, badClasses []string, cornerFt float64) *Roads {
	b := map[string]bool{}
	for _, c := range badClasses {
		b[strings.ToLower(strings.TrimSpace(c))] = true
	}
	if cornerFt <= 0 {
		cornerFt = 150
	}
	return &Roads{
		db: db,
		ncdot: NewGuard(db, "ncdot-aadt", GuardOpts{
			MinInterval: 1500 * time.Millisecond,
			Budget:      60,
			Timeout:     30 * time.Second,
		}),
		overpass: overpassGuards(db),
		bad:      b,
		corner:   cornerFt,
	}
}

// Overpass asks for light use and bans by IP, and the ban isn't something to
// negotiate out of.
func overpassGuards(db *sql.DB) []mirror {
	var out []mirror
	for _, u := range overpassMirrors {
		out = append(out, mirror{
			url: u,
			guard: NewGuard(db, "overpass:"+hostOf(u), GuardOpts{
				MinInterval: 4 * time.Second,
				Budget:      40,
				Window:      time.Hour,
				Timeout:     overpassAttempt,
				// Overpass answers 406 to a browser string.
				UserAgent: clientUA,
			}),
		})
	}
	return out
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Host
}

// Every address here is a house with a road near it, so an empty answer means the
// mirror has no data for here, and caching it would read as no frontage at all.
var emptyIsNotAnAnswer = fmt.Errorf("mirror returned no elements, so it has no data for here")

// Bounds the whole mirror loop, not each attempt, so overpassAttempt has to
// divide into it or the first mirror to hang eats the lot.
const overpassBudget = 90 * time.Second

// Also written into the query as Overpass' own [timeout:], so the server gives up
// when the client does.
const overpassAttempt = 25 * time.Second

// Overpass enforces two concurrent queries per address, and an assessment runs
// several Overpass lookups at once, so one at a time everywhere keeps us inside it.
var overpassSlot sync.Mutex

// overpassQuery asks mirrors in order, since one whose breaker is open costs
// nothing to skip.
func (r *Roads) overpassQuery(ctx context.Context, query string) ([]byte, error) {
	overpassSlot.Lock()
	defer overpassSlot.Unlock()

	ctx, cancel := context.WithTimeout(ctx, overpassBudget)
	defer cancel()

	var lastErr error
	for _, m := range r.overpass {
		if ctx.Err() != nil {
			if lastErr == nil {
				lastErr = ctx.Err()
			}
			break
		}
		body, err := m.guard.GetRaw(ctx, m.url+"?data="+url.QueryEscape(query))
		if err != nil {
			lastErr = err
			continue
		}

		var res overpassResult
		if err := json.Unmarshal(body, &res); err != nil {
			lastErr = err
			continue
		}
		if len(res.Elements) == 0 {
			lastErr = emptyIsNotAnAnswer
			continue
		}
		return body, nil
	}
	return nil, lastErr
}

func orUnnamed(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	if strings.TrimSpace(b) != "" {
		return b
	}
	return "an unnamed road"
}

func (r *Roads) Lookup(ctx context.Context, lat, lon float64) (RoadResult, error) {
	key := fmt.Sprintf("%.5f,%.5f", lat, lon)

	var payload string
	err := r.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = ? AND key = ?`, roadCacheKind, key).Scan(&payload)
	if err == nil {
		var cached RoadResult
		if json.Unmarshal([]byte(payload), &cached) == nil && !cached.Partial {
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return RoadResult{}, err
	}

	p := point{lat, lon}
	out := RoadResult{Measured: true, AADTFeet: math.Inf(1), ClassFeet: math.Inf(1),
		RampFeet: math.Inf(1), HighwayFeet: math.Inf(1)}

	var res esriQueryResult
	u := envelopeQuery(ncdotAADT, lat, lon, roadSearchFeet, "AADT_2023,AADT_2022,AADT_2021,AADT,ROUTE,LOCATION", "")
	if err := r.ncdot.Get(ctx, u, &res); err != nil {
		out.Partial = true
	} else {
		for _, feat := range res.Features {
			// NCDOT adds a column per survey year, so the newest present wins.
			count, ok := attrFloat(feat.Attributes, "AADT_2023", "AADT_2022", "AADT_2021", "AADT")
			if !ok {
				continue
			}
			d := feat.Geometry.distanceFeet(p)
			if d < out.AADTFeet {
				out.AADTFeet = d
				out.AADT = int(count)
				out.AADTRoute = attrString(feat.Attributes, "ROUTE", "LOCATION")
			}
		}
	}

	if osm, err := r.osmAround(ctx, lat, lon, r.corner); err != nil {
		out.Partial = true
	} else {
		out.Class, out.RoadName, out.ClassFeet = osm.class, osm.name, osm.classFeet
		out.NearbyRoads = osm.nearby
		out.RampFeet, out.RampName = osm.rampFeet, osm.rampName
		out.HighwayFeet, out.HighwayName = osm.highwayFeet, osm.highwayName
	}

	for _, f := range []*float64{&out.AADTFeet, &out.ClassFeet, &out.RampFeet, &out.HighwayFeet} {
		if math.IsInf(*f, 1) {
			*f = notFound
		}
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := keep(ctx, r.db,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES (?,?,?,?)`,
		roadCacheKind, key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

type overpassResult struct {
	Elements []struct {
		Type     string            `json:"type"`
		Tags     map[string]string `json:"tags"`
		Geometry []struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"geometry"`
	} `json:"elements"`
}

type osmAnswer struct {
	class       string
	name        string
	classFeet   float64
	nearby      []string
	rampFeet    float64
	rampName    string
	highwayFeet float64
	highwayName string
}

// osmAround uses `out geom` since a long way's centre can be half a mile off and
// pick the wrong road. The wide ring asks only for motorways, trunks and ramps,
// so the response stays small.
func (r *Roads) osmAround(ctx context.Context, lat, lon, corner float64) (osmAnswer, error) {
	const (
		nearM = int(roadSearchFeet * 3048 / 10000)
		rampM = int(rampSearchFeet * 3048 / 10000)
	)
	q := fmt.Sprintf(`[out:json][timeout:25];(
way(around:%d,%.6f,%.6f)["highway"~"^(motorway|trunk|primary|secondary|tertiary|unclassified|residential|service|track)$"];
way(around:%d,%.6f,%.6f)["highway"~"^(motorway|motorway_link|trunk)$"];
);out geom tags;`, nearM, lat, lon, rampM, lat, lon)

	var ans osmAnswer
	ans.classFeet = math.Inf(1)
	ans.rampFeet = math.Inf(1)
	ans.highwayFeet = math.Inf(1)

	body, err := r.overpassQuery(ctx, q)
	if err != nil {
		return ans, err
	}
	var res overpassResult
	if err := json.Unmarshal(body, &res); err != nil {
		return ans, err
	}

	p := point{lat, lon}
	seen := map[string]bool{}

	for _, el := range res.Elements {
		var ring [][]float64
		for _, g := range el.Geometry {
			ring = append(ring, []float64{g.Lon, g.Lat})
		}
		if len(ring) < 2 {
			continue
		}
		class := el.Tags["highway"]
		name := el.Tags["name"]
		if name == "" {
			name = el.Tags["ref"]
		}
		d := ringDistance(p, ring)

		if class == "motorway_link" {
			if d < ans.rampFeet {
				ans.rampFeet = d
				ans.rampName = orUnnamed(name, el.Tags["destination"])
			}
			continue
		}
		if class == "motorway" || class == "trunk" {
			if d < ans.highwayFeet {
				ans.highwayFeet = d
				ans.highwayName = orUnnamed(name, el.Tags["ref"])
			}
			// A motorway inside the near ring is also the frontage.
			if d > roadSearchFeet {
				continue
			}
		}

		if d < ans.classFeet {
			ans.classFeet, ans.class, ans.name = d, class, name
		}

		// A driveway and a track aren't road on a side of the house. An unnamed
		// way is counted by its class so two lanes with no name read as two.
		if d <= corner && class != "service" && class != "track" {
			label := orUnnamed(name, class)
			if !seen[label] {
				seen[label] = true
				ans.nearby = append(ans.nearby, label)
			}
		}
	}

	sortStrings(ans.nearby)
	return ans, nil
}

func sortStrings(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
