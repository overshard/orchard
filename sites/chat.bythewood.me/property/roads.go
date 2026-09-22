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

// What the house fronts, from two sources that disagree usefully.
//
// NCDOT publishes annual average daily traffic per road segment, which is a
// measured count and the defensible half of "is this on a busy road". OSM
// publishes a road classification, which covers the roads NCDOT never counted,
// including the subdivision street a house actually fronts. Neither alone is
// enough: NC 90 has a count and the gravel lane beside it does not, and OSM
// calls both of them something.
const (
	ncdotAADT = "https://services.arcgis.com/NuWFvHYDMVmmxMeM/arcgis/rest/services/NCDOT_AADT_Traffic_Segmentation/FeatureServer/2/query"

	// How far from the house to look for a road. A rural driveway can run a few
	// hundred feet, so the nearest classified way inside this radius is the one
	// the house fronts often enough to be the right default.
	roadSearchFeet = 600

	// How far out to look for an interstate on-ramp. Being a couple of minutes
	// from one is a want, and two minutes of driving is about a mile and a half.
	rampSearchFeet = 10560

	// The cache payload shape, in the cache key. An older cached row has no ramp
	// distance and no way count, and reading one back would say a house is right
	// beside a ramp and has no roads near it, so a shape change has to miss
	// rather than decode.
	roadCacheKind = "roadv2"
)

// Overpass, for road class, the corner test and where the nearest interstate ramp
// is. The public instances ask for light use and mean it, so this is one query
// per listing behind the slowest guard here.
//
// More than one instance, because the main one answers 504 under load often
// enough that a single endpoint means no road data for a whole run. Each has its
// own guard, so a mirror that is down gets its own breaker.
//
// Every one is checked to hold data for North Carolina. Several public instances
// are regional extracts and overpass.osm.ch is Switzerland only, which answered
// 200 with an empty element list for every address here.
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
	AADTFeet  float64 // how far that segment is, so a count on a road a mile off is visibly not the frontage
	Class     string  // the OSM highway value of the nearest classified way
	RoadName  string
	ClassFeet float64 // how far that way is, which is what separates fronting a road from being near one

	// Roads inside the corner radius. Two distinct ones means the house has road
	// on more than one side, which is the corner lot rule.
	NearbyRoads []string

	// Two different questions about the same road. The pavement is the noise and
	// the safety one, since what nobody wants is an interstate at the bottom of
	// the garden. The ramp is the getting-places one and it matters far less.
	HighwayFeet float64
	HighwayName string
	RampFeet    float64
	RampName    string

	// Set when a lookup actually ran, for the same reason FloodResult carries one:
	// the zero value would otherwise read as a ramp nought feet away.
	Measured bool

	Partial bool
}

// newRoadResult is the not-measured zero value, with every distance at the
// sentinel rather than at zero.
func newRoadResult() RoadResult {
	return RoadResult{AADTFeet: notFound, ClassFeet: notFound, RampFeet: notFound, HighwayFeet: notFound}
}

// Corner reports whether road runs on more than one side of the house.
func (r RoadResult) Corner() bool { return len(r.NearbyRoads) > 1 }

// A mirror and its own guard, together. They were two parallel slices indexed by
// position, which panics the moment the lists disagree about their length.
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
			Budget:      400,
			Timeout:     30 * time.Second,
		}),
		overpass: overpassGuards(db),
		bad:      b,
		corner:   cornerFt,
	}
}

// Four seconds apart and a small budget each. Overpass asks for light use and
// bans by IP, and the ban is not something to negotiate out of.
func overpassGuards(db *sql.DB) []mirror {
	var out []mirror
	for _, u := range overpassMirrors {
		out = append(out, mirror{
			url: u,
			guard: NewGuard(db, "overpass:"+hostOf(u), GuardOpts{
				MinInterval: 4 * time.Second,
				Budget:      120,
				Window:      time.Hour,
				Timeout:     overpassAttempt,
				// Overpass answers 406 to a browser string. It is an API and it
				// wants to know who is calling, which is fair.
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

// emptyIsNotAnAnswer is returned when a mirror answers with nothing. Every
// address this runs against is a house somebody lives in, so there is a road within six hundred feet of it. An empty element list
// means the mirror has no data for this part of the world, and caching it would
// score the listing as having no frontage at all.
var emptyIsNotAnAnswer = fmt.Errorf("mirror returned no elements, so it has no data for here")

// This bounds the whole mirror loop rather than each attempt. Five mirrors at a
// forty second timeout each is over three minutes of waiting for an answer that
// is not coming, and on a bad Overpass day two of these run at once. The road
// and outing factors both degrade honestly when it runs out.
// Five mirrors have to fit inside it, so the per mirror timeout below has to
// divide into it or the first one to hang eats the lot and the rest are never
// asked.
const overpassBudget = 90 * time.Second

// What one mirror gets before we move on. It is also written into the query as
// Overpass' own [timeout:], so the server gives up at the same moment the client
// does rather than carrying on with work nobody is waiting for.
const overpassAttempt = 25 * time.Second

// Overpass publishes its own limit of two concurrent queries per address, which
// /api/status will tell you. An assessment asks for the roads and the outings at
// once and each of those walks four mirrors, so the two overlap and the answer is
// 429 for both. One at a time, everywhere, keeps us inside it.
var overpassSlot sync.Mutex

// overpassQuery tries each mirror in turn and returns the first real answer. A
// mirror whose breaker is open costs nothing to skip, which is why they are asked
// in order rather than at random.
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
			// Newest year present wins. NCDOT adds a column per survey year
			// rather than replacing one, and which years exist moves over time.
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
	if _, err := r.db.ExecContext(ctx,
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

// osmAround is one Overpass call answering three questions: what the house
// fronts, whether road runs on more than one side of it, and how far the nearest
// interstate on-ramp is.
//
// `out geom` on the near ring, because a way's centre can be half a mile off when
// the way is long and that picks the wrong road. The wide ring asks for ramps
// only, which are short ways, so the whole response stays small.
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
			// A motorway inside the near ring is also the frontage, so it falls
			// through to the class test below rather than being counted only here.
			if d > roadSearchFeet {
				continue
			}
		}

		if d < ans.classFeet {
			ans.classFeet, ans.class, ans.name = d, class, name
		}

		// A driveway and a track are not road on a side of the house, and an
		// unnamed way is counted by its class so two lanes with no name still
		// read as two roads.
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
