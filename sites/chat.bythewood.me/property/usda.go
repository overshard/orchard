package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// Whether an address sits somewhere USDA will lend on, which is the question
// every USDA answer turns on and the one a mortgage calculator never asks. A
// no-money-down loan is worth a great deal and most of this county qualifies,
// while a house three miles further in does not.
//
// The map publishes the ineligible areas as polygons rather than the eligible
// ones, so a point that matches nothing is a point USDA will lend on. Getting
// that backwards would call the whole county ineligible, which is why the test
// pins both a town and a rural address.
var usdaLayer = "https://rdgdwe.sc.egov.usda.gov/arcgis/rest/services/Eligibility/Eligibility/MapServer/4/query"

type USDA struct {
	db    *sql.DB
	guard *Guard
}

func NewUSDA(db *sql.DB) *USDA {
	return &USDA{
		db: db,
		guard: NewGuard(db, "usda-eligibility", GuardOpts{
			MinInterval: time.Second,
			Budget:      400,
			Window:      time.Hour,
			Timeout:     30 * time.Second,
			UserAgent:   clientUA,
		}),
	}
}

// USDAArea is what the map said about one point.
type USDAArea struct {
	Eligible bool   `json:"eligible"`
	Measured bool   `json:"measured"`
	Source   string `json:"source"`
}

// The boundaries move when USDA redraws them off a census, which is every ten
// years with a revision or two in between, so a year is not long to hold one.
const usdaTTL = 365 * 24 * time.Hour

func (u *USDA) Lookup(ctx context.Context, lat, lon float64) (USDAArea, error) {
	key := fmt.Sprintf("%.5f,%.5f", lat, lon)

	var payload string
	var fetched int64
	err := u.db.QueryRowContext(ctx,
		`SELECT payload, fetched_at FROM lookups WHERE kind = 'usda' AND key = ?`, key).
		Scan(&payload, &fetched)
	if err == nil && time.Since(time.Unix(fetched, 0)) < usdaTTL {
		var a USDAArea
		if json.Unmarshal([]byte(payload), &a) == nil && a.Measured {
			return a, nil
		}
	}

	q := url.Values{}
	q.Set("geometry", fmt.Sprintf("%f,%f", lon, lat))
	q.Set("geometryType", "esriGeometryPoint")
	q.Set("inSR", "4326")
	q.Set("spatialRel", "esriSpatialRelIntersects")
	q.Set("returnGeometry", "false")
	q.Set("returnCountOnly", "true")
	q.Set("f", "json")

	var res struct {
		Count int `json:"count"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := u.guard.Get(ctx, usdaLayer+"?"+q.Encode(), &res); err != nil {
		return USDAArea{}, err
	}
	if res.Error != nil {
		return USDAArea{}, fmt.Errorf("usda eligibility: %s", res.Error.Message)
	}

	a := USDAArea{
		Eligible: res.Count == 0,
		Measured: true,
		Source:   "USDA Rural Development eligibility map, single family housing layer",
	}
	if _, err := u.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('usda',?,?,?)`,
		key, mustJSON(a), time.Now().Unix()); err != nil {
		return a, nil
	}
	return a, nil
}
