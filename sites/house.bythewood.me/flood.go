package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

// Flood risk, from the two authorities rather than from a listing's own
// disclosure. FEMA's National Flood Hazard Layer says which zone a point is in,
// and the USGS National Hydrography Dataset says how far the nearest mapped
// water is, which is the thing FEMA misses: a creek too small to have been
// studied has no zone drawn around it and still floods a yard.
//
// Both answers come back as a distance, not a boolean. A house 40ft outside an
// AE zone and one 4000ft outside both pass the filter and are not the same
// house, so the margin is kept and scored.
const (
	nfhlFloodZones = "https://hazards.fema.gov/arcgis/rest/services/public/NFHL/MapServer/28/query"
	nhdFlowlines   = "https://hydro.nationalmap.gov/arcgis/rest/services/nhd/MapServer/6/query"
	nhdWaterbodies = "https://hydro.nationalmap.gov/arcgis/rest/services/nhd/MapServer/12/query"

	// How far out to look. A mile of margin is more than the score needs and
	// keeps one query per listing per layer instead of a widening ladder.
	floodSearchFeet = 5280

	// The cache payload shape, in the cache key. An older row has one water
	// distance where this one has three, and decoding it would read as no ditch
	// and no stream anywhere, so a shape change has to miss rather than decode.
	floodCacheKind = "floodv2"
)

// NHD codes the kind of water in FCode, and the distinction is the whole point
// here. A perennial stream or a lake is the thing a 300ft buffer is for. An
// intermittent or ephemeral blue line is a wet-weather ditch, and in these
// counties nearly every rural parcel has one inside 300ft, so buffering them the
// same way excludes most of the county for nothing.
//
// Codes from the NHD feature catalogue: 46006 perennial stream or river, 46003
// intermittent, 46007 ephemeral, 33600 canal or ditch and its subtypes, 39004 and
// 39009 perennial lake or pond, 39001 and 39006 intermittent, 43600 reservoir,
// 49300 estuary, 48400 wash.
func waterClass(fcode float64, ftype string) string {
	switch int(fcode) {
	case 46006, 46000, 39004, 39009, 39010, 39011, 39012, 43600, 43601, 43603,
		43604, 43605, 43606, 43607, 43608, 43609, 43610, 43611, 43612, 43613,
		43614, 43615, 43617, 43618, 43619, 43621, 43623, 43624, 43625, 43626,
		49300, 56600:
		return "perennial"
	case 46003, 39001, 39005, 39006, 48400:
		return "intermittent"
	case 46007:
		return "ephemeral"
	case 33600, 33601, 33603, 33400, 33401, 33402, 42800, 42801, 42802, 42803,
		42804, 42805, 42806, 42807, 42808, 42809, 42810, 42811, 42812, 42813,
		42814, 42815, 42816:
		return "ditch"
	}
	// An unknown code is treated as perennial, which is the cautious answer and
	// is what an unmapped FCode on a real river looks like.
	if ftype != "" {
		return "perennial"
	}
	return "perennial"
}

// Distances are -1 when nothing of that kind was found inside the search
// radius, never +Inf: JSON cannot carry an infinity and this struct is cached as
// JSON, so a marshal would fail on the very rows that are furthest from water.
const notFound = -1.0

type FloodResult struct {
	Zone     string  // the FEMA zone the point itself is in
	Subtype  string  // FEMA's own wording, e.g. AREA OF MINIMAL FLOOD HAZARD
	SFHA     bool    // inside a Special Flood Hazard Area
	SFHAFeet float64 // to the nearest SFHA, 0 inside one, -1 if none within a mile

	// The nearest water of each kind, kept apart because they mean different
	// things. Perennial is the river, the creek that runs all year and the lake.
	// Intermittent and ephemeral are the wet-weather lines, and a ditch is a ditch.
	PerennialFeet    float64
	PerennialName    string
	IntermittentFeet float64
	IntermittentName string
	DitchFeet        float64

	// The nearest of any kind, which is what the card shows in one line.
	WaterFeet float64
	WaterName string
	WaterKind string

	// Set when a lookup actually ran. Without it the zero value of this struct
	// reads as water nought feet away, which would flag every listing whose flood
	// lookup was skipped as standing in a creek.
	Measured bool

	Partial bool // a layer could not be reached, so the numbers are incomplete
}

// newFloodResult is the not-measured zero value, with every distance at the
// sentinel rather than at zero.
func newFloodResult() FloodResult {
	return FloodResult{
		SFHAFeet:         notFound,
		PerennialFeet:    notFound,
		IntermittentFeet: notFound,
		DitchFeet:        notFound,
		WaterFeet:        notFound,
	}
}

type Flood struct {
	db    *sql.DB
	fema  *Guard
	usgs  *Guard
	zones map[string]bool
}

func NewFlood(db *sql.DB, sfhaZones []string) *Flood {
	z := map[string]bool{}
	for _, s := range sfhaZones {
		z[strings.ToUpper(strings.TrimSpace(s))] = true
	}
	return &Flood{
		db: db,
		fema: NewGuard(db, "fema-nfhl", GuardOpts{
			MinInterval: 1500 * time.Millisecond,
			Budget:      400,
			Timeout:     30 * time.Second,
		}),
		usgs: NewGuard(db, "usgs-nhd", GuardOpts{
			MinInterval: 1500 * time.Millisecond,
			Budget:      800,
			Timeout:     30 * time.Second,
		}),
		zones: z,
	}
}

type esriFeature struct {
	Attributes map[string]any `json:"attributes"`
	Geometry   esriGeometry   `json:"geometry"`
}

type esriQueryResult struct {
	Features []esriFeature             `json:"features"`
	Error    *struct{ Message string } `json:"error"`
}

func attrString(a map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := a[k]; ok && v != nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func attrFloat(a map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := a[k]; ok && v != nil {
			if f, ok := v.(float64); ok {
				return f, true
			}
			if s, ok := v.(string); ok {
				if f := num(s); f != 0 {
					return f, true
				}
			}
		}
	}
	return 0, false
}

// envelopeQuery is the one ArcGIS call shape every layer here uses: a box around
// a point, geometry returned in WGS84, trimmed to six decimal places because
// anything finer is under a foot and only makes the response bigger.
func envelopeQuery(base string, lat, lon, feet float64, outFields, where string) string {
	xmin, ymin, xmax, ymax := envelopeAround(lat, lon, feet)
	v := url.Values{
		"geometry":          {fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", xmin, ymin, xmax, ymax)},
		"geometryType":      {"esriGeometryEnvelope"},
		"inSR":              {"4326"},
		"outSR":             {"4326"},
		"spatialRel":        {"esriSpatialRelIntersects"},
		"outFields":         {outFields},
		"returnGeometry":    {"true"},
		"geometryPrecision": {"6"},
		"f":                 {"json"},
	}
	if where != "" {
		v.Set("where", where)
	}
	return base + "?" + v.Encode()
}

// Lookup answers both questions for one point, cached by rounded coordinate.
// Five decimal places is about a metre, so two listings on the same parcel share
// a cache entry and nothing meaningful is merged that should not be.
func (f *Flood) Lookup(ctx context.Context, lat, lon float64) (FloodResult, error) {
	key := fmt.Sprintf("%.5f,%.5f", lat, lon)

	var payload string
	err := f.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = ? AND key = ?`, floodCacheKind, key).Scan(&payload)
	if err == nil {
		var cached FloodResult
		if json.Unmarshal([]byte(payload), &cached) == nil && !cached.Partial {
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return FloodResult{}, err
	}

	p := point{lat, lon}
	out := FloodResult{
		Measured:         true,
		SFHAFeet:         math.Inf(1),
		PerennialFeet:    math.Inf(1),
		IntermittentFeet: math.Inf(1),
		DitchFeet:        math.Inf(1),
		WaterFeet:        math.Inf(1),
	}

	var femaRes esriQueryResult
	femaURL := envelopeQuery(nfhlFloodZones, lat, lon, floodSearchFeet,
		"FLD_ZONE,ZONE_SUBTY,SFHA_TF,STATIC_BFE", "")
	if err := f.fema.Get(ctx, femaURL, &femaRes); err != nil {
		out.Partial = true
	} else {
		for _, feat := range femaRes.Features {
			zone := strings.ToUpper(attrString(feat.Attributes, "FLD_ZONE"))
			d := feat.Geometry.distanceFeet(p)
			isSFHA := f.zones[zone] || attrString(feat.Attributes, "SFHA_TF") == "T"

			if d == 0 {
				// The point is inside this polygon. An SFHA wins over an X zone
				// when both claim the point, which happens on a boundary.
				if out.Zone == "" || isSFHA {
					out.Zone = zone
					out.Subtype = attrString(feat.Attributes, "ZONE_SUBTY")
				}
				if isSFHA {
					out.SFHA = true
					out.SFHAFeet = 0
				}
			}
			if isSFHA && d < out.SFHAFeet {
				out.SFHAFeet = d
			}
		}
		if out.Zone == "" && len(femaRes.Features) == 0 {
			// Outside every mapped polygon. Parts of these counties are
			// genuinely unmapped rather than safe, so say so.
			out.Zone = "UNMAPPED"
		}
	}

	for _, layer := range []string{nhdFlowlines, nhdWaterbodies} {
		var res esriQueryResult
		u := envelopeQuery(layer, lat, lon, floodSearchFeet, "gnis_name,fcode,ftype", "")
		if err := f.usgs.Get(ctx, u, &res); err != nil {
			out.Partial = true
			continue
		}
		for _, feat := range res.Features {
			d := feat.Geometry.distanceFeet(p)
			name := attrString(feat.Attributes, "gnis_name", "GNIS_NAME")
			if name == "" {
				name = "unnamed water"
			}
			fcode, _ := attrFloat(feat.Attributes, "fcode", "FCode", "FCODE")
			kind := waterClass(fcode, attrString(feat.Attributes, "ftype", "FType", "FTYPE"))

			switch kind {
			case "perennial":
				if d < out.PerennialFeet {
					out.PerennialFeet, out.PerennialName = d, name
				}
			case "ditch":
				if d < out.DitchFeet {
					out.DitchFeet = d
				}
			default:
				if d < out.IntermittentFeet {
					out.IntermittentFeet, out.IntermittentName = d, name
				}
			}

			if d < out.WaterFeet {
				out.WaterFeet, out.WaterName, out.WaterKind = d, name, kind
			}
		}
	}

	// Before the marshal, not in a deferred tidy-up afterwards.
	for _, d := range []*float64{&out.SFHAFeet, &out.PerennialFeet,
		&out.IntermittentFeet, &out.DitchFeet, &out.WaterFeet} {
		if math.IsInf(*d, 1) {
			*d = notFound
		}
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := f.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES (?,?,?,?)`,
		floodCacheKind, key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}
