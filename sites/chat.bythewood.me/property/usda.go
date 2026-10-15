package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// The map publishes the ineligible areas, not the eligible ones, so a point that
// matches nothing is one USDA will lend on.
var usdaLayer = "https://rdgdwe.sc.egov.usda.gov/arcgis/rest/services/Eligibility/Eligibility/MapServer/4/query"

type USDA struct {
	db    *sql.DB
	guard *Guard
}

func NewUSDA(db *sql.DB) *USDA {
	return &USDA{
		db: db,
		guard: NewGuard(db, "usda-eligibility", GuardOpts{
			MinInterval: 2 * time.Second,
			Budget:      60,
			Window:      time.Hour,
			Timeout:     30 * time.Second,
			UserAgent:   clientUA,
		}),
	}
}

type USDAArea struct {
	Eligible bool   `json:"eligible"`
	Measured bool   `json:"measured"`
	Source   string `json:"source"`
}

// USDA redraws the boundaries off each census with a revision or two between.
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
	if _, err := keep(ctx, u.db,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('usda',?,?,?)`,
		key, mustJSON(a), time.Now().Unix()); err != nil {
		return a, nil
	}
	return a, nil
}
