package property

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

// USGS hydrography is here alongside FEMA since a creek too small to have been
// studied has no zone drawn around it and still floods a yard. Both answers are
// distances, since 40ft outside an AE zone and 4000ft outside aren't the same house.
const (
	nfhlFloodZones = "https://hazards.fema.gov/arcgis/rest/services/public/NFHL/MapServer/28/query"
	nhdFlowlines   = "https://hydro.nationalmap.gov/arcgis/rest/services/nhd/MapServer/6/query"
	nhdWaterbodies = "https://hydro.nationalmap.gov/arcgis/rest/services/nhd/MapServer/12/query"

	// One query per layer with a mile of margin instead of a widening ladder.
	floodSearchFeet = 5280

	// Bump when FloodResult changes shape, since an older row decodes as no
	// ditch and no stream anywhere.
	floodCacheKind = "floodv2"
)

// waterClass reads NHD's FCode, from its feature catalogue: 46006 perennial
// stream, 46003 intermittent, 46007 ephemeral, 33600 ditch, 39004 and 39009
// perennial lake, 39001 and 39006 intermittent, 43600 reservoir, 49300 estuary.
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
	// An unknown code is perennial, which is cautious and is what an unmapped
	// FCode on a real river looks like.
	if ftype != "" {
		return "perennial"
	}
	return "perennial"
}

// Never +Inf, since JSON can't carry an infinity and a cached marshal would fail
// on the rows furthest from water.
const notFound = -1.0

type FloodResult struct {
	Zone     string  // the FEMA zone the point itself is in
	Subtype  string  // FEMA's own wording, e.g. AREA OF MINIMAL FLOOD HAZARD
	SFHA     bool    // inside a Special Flood Hazard Area
	SFHAFeet float64 // to the nearest SFHA, 0 inside one, -1 if none within a mile

	// Kept apart since nearly every rural parcel here has a wet-weather line or a
	// ditch inside 300ft, and only perennial water is what a buffer is for.
	PerennialFeet    float64
	PerennialName    string
	IntermittentFeet float64
	IntermittentName string
	DitchFeet        float64

	// The nearest of any kind.
	WaterFeet float64
	WaterName string
	WaterKind string

	// Without it the zero value reads as water nought feet away, so a skipped
	// lookup would be standing in a creek.
	Measured bool

	Partial bool // a layer could not be reached, so the numbers are incomplete
}

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
			Budget:      60,
			Timeout:     30 * time.Second,
		}),
		usgs: NewGuard(db, "usgs-nhd", GuardOpts{
			MinInterval: 1500 * time.Millisecond,
			Budget:      60,
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

// envelopeQuery trims geometry to six decimal places, since anything finer is
// under a foot and only makes the response bigger.
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

// Lookup is cached to five decimal places, about a metre, so two listings on the
// same parcel share an entry.
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
				// On a boundary both claim the point, and the SFHA wins.
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
			// Parts of these counties are unmapped, which isn't the same as safe.
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

	// Before the marshal, which fails on an infinity.
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
	if _, err := keep(ctx, f.db,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES (?,?,?,?)`,
		floodCacheKind, key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}
