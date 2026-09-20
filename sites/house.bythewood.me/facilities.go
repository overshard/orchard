package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// Where a nurse can work, as points on a map rather than as addresses to place.
//
// NC OneMap publishes the DHSR licensing data already located, so there is
// nothing to geocode and nothing to fail. Downloading the roster and geocoding
// every row fails on the ones listed at a PO box, and Caldwell Memorial is one.
//
// Two layers, hospitals and the nursing and assisted living homes, queried
// around the house rather than by county, since a county line is not a commute.
const ncHealth = "https://services.nconemap.gov/secure/rest/services/NC1Map_Health/MapServer"

type healthLayer struct {
	id    string
	kind  string
	label string
	// The name column, which differs between the two layers. Asking for a field a
	// layer does not have fails the whole query with "Failed to execute query" and
	// no clue which field was wrong.
	nameField string
}

var healthLayers = []healthLayer{
	{"0", "hospital", "hospital", "facility"},
	{"3", "nursing_home", "nursing or assisted living", "name"},
}

// How far around a house to look for somewhere to work. Wider than the search
// band for houses, because a commute to work is allowed to be longer than a
// commute to school and the score decides what is near rather than this.
const facilitySearchFeet = 35 * 5280

type Facility struct {
	ID     string
	Kind   string
	Label  string
	Name   string
	County string
	Lat    float64
	Lon    float64
}

type Facilities struct {
	db    *sql.DB
	guard *Guard
}

func NewFacilities(db *sql.DB, _ *Geocoder) *Facilities {
	return &Facilities{
		db: db,
		guard: NewGuard(db, "nc-health", GuardOpts{
			MinInterval: 2 * time.Second,
			Budget:      200,
			Window:      time.Hour,
			Timeout:     45 * time.Second,
		}),
	}
}

// Around returns every facility within the search radius of a point, cached on
// the rounded coordinate. Two requests per new area and none after that.
func (f *Facilities) Around(ctx context.Context, lat, lon float64) ([]Facility, error) {
	key := fmt.Sprintf("%.3f,%.3f", lat, lon)

	var payload string
	err := f.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = 'health' AND key = ?`, key).Scan(&payload)
	if err == nil {
		var cached []Facility
		if json.Unmarshal([]byte(payload), &cached) == nil {
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	xmin, ymin, xmax, ymax := envelopeAround(lat, lon, facilitySearchFeet)
	var out []Facility

	for _, layer := range healthLayers {
		v := url.Values{
			"geometry":          {fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", xmin, ymin, xmax, ymax)},
			"geometryType":      {"esriGeometryEnvelope"},
			"inSR":              {"4326"},
			"outSR":             {"4326"},
			"spatialRel":        {"esriSpatialRelIntersects"},
			"outFields":         {layer.nameField},
			"returnGeometry":    {"true"},
			"geometryPrecision": {"6"},
			"f":                 {"json"},
		}

		var res esriQueryResult
		if err := f.guard.Get(ctx, ncHealth+"/"+layer.id+"/query?"+v.Encode(), &res); err != nil {
			return nil, err
		}

		for _, feat := range res.Features {
			if feat.Geometry.X == nil || feat.Geometry.Y == nil {
				continue
			}
			name := attrString(feat.Attributes, layer.nameField)
			if name == "" {
				continue
			}
			out = append(out, Facility{
				ID:    layer.kind + ":" + name,
				Kind:  layer.kind,
				Label: layer.label,
				Name:  name,
				Lat:   *feat.Geometry.Y,
				Lon:   *feat.Geometry.X,
			})
		}
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := f.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('health',?,?,?)`,
		key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

// Nearest is the n closest by straight line, which only decides which are worth
// routing. A hospital is always kept alongside the homes, since the nearest
// three homes in one town would otherwise hide the hospital in the next.
func (f *Facilities) Nearest(ctx context.Context, lat, lon float64, n int) ([]Facility, error) {
	all, err := f.Around(ctx, lat, lon)
	if err != nil {
		return nil, err
	}

	byKind := map[string][]Facility{}
	for _, fc := range all {
		byKind[fc.Kind] = append(byKind[fc.Kind], fc)
	}

	var out []Facility
	for _, layer := range healthLayers {
		group := byKind[layer.kind]
		sortByDistance(group, lat, lon)
		for i := 0; i < len(group) && i < n; i++ {
			out = append(out, group[i])
		}
	}
	return out, nil
}

func sortByDistance(f []Facility, lat, lon float64) {
	d := func(x Facility) float64 { return haversineFeet(lat, lon, x.Lat, x.Lon) }
	for i := 1; i < len(f); i++ {
		for j := i; j > 0 && d(f[j]) < d(f[j-1]); j-- {
			f[j], f[j-1] = f[j-1], f[j]
		}
	}
}

// Unused, kept out: the DHSR bed counts and licence numbers. They are on the
// rosters and not on these layers, and neither changes whether a place is
// somewhere to apply.
