package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The Census geocoder, because it needs no key, covers every US address, and is
// run by the people who assign the address ranges. It answers with a point on a
// matched street range rather than a rooftop, which is inside the tolerance here,
// since a 300ft water buffer and a drive time do not turn on thirty feet.
//
// Nominatim is not used. Its terms cap bulk geocoding and OSM's coverage of
// rural NC address points is thinner than TIGER's.
const censusGeocodeURL = "https://geocoding.geo.census.gov/geocoder/locations/onelineaddress"

type Geocoder struct {
	db    *sql.DB
	guard *Guard
}

func NewGeocoder(db *sql.DB) *Geocoder {
	return &Geocoder{
		db: db,
		// One a second. The service has no published rate limit, which is a
		// reason to be careful rather than a licence.
		guard: NewGuard(db, "census-geocoder", GuardOpts{
			MinInterval: 1500 * time.Millisecond,
			Budget:      400,
			Window:      time.Hour,
		}),
	}
}

type censusResponse struct {
	Result struct {
		AddressMatches []struct {
			MatchedAddress string `json:"matchedAddress"`
			Coordinates    struct {
				X float64 `json:"x"`
				Y float64 `json:"y"`
			} `json:"coordinates"`
		} `json:"addressMatches"`
	} `json:"result"`
}

// Geocode resolves one address, cached forever. An address does not move, so a
// cache hit is always correct and the only reason to ask twice is a miss.
func (g *Geocoder) Geocode(ctx context.Context, address string) (lat, lon float64, matched string, err error) {
	key := strings.ToLower(strings.Join(strings.Fields(address), " "))
	if key == "" {
		return 0, 0, "", fmt.Errorf("empty address")
	}

	var clat, clon sql.NullFloat64
	var cmatched sql.NullString
	err = g.db.QueryRowContext(ctx,
		`SELECT lat, lon, matched FROM geocodes WHERE query = ?`, key).Scan(&clat, &clon, &cmatched)
	if err == nil {
		if !clat.Valid {
			// A cached miss. Asking again every refresh for an address the
			// geocoder has already said it cannot place is the loop this avoids.
			return 0, 0, "", fmt.Errorf("no geocode match for %q", address)
		}
		return clat.Float64, clon.Float64, cmatched.String, nil
	}
	if err != sql.ErrNoRows {
		return 0, 0, "", err
	}

	u := censusGeocodeURL + "?" + url.Values{
		"address":   {address},
		"benchmark": {"Public_AR_Current"},
		"format":    {"json"},
	}.Encode()

	var out censusResponse
	if err := g.guard.Get(ctx, u, &out); err != nil {
		// Not cached as a miss: a guarded or failed call is our problem, not an
		// address that cannot be placed.
		return 0, 0, "", err
	}

	now := time.Now().Unix()
	if len(out.Result.AddressMatches) == 0 {
		if _, err := g.db.ExecContext(ctx,
			`INSERT OR REPLACE INTO geocodes (query, lat, lon, matched, source, fetched_at)
			 VALUES (?, NULL, NULL, NULL, 'census', ?)`, key, now); err != nil {
			return 0, 0, "", err
		}
		return 0, 0, "", fmt.Errorf("no geocode match for %q", address)
	}

	m := out.Result.AddressMatches[0]
	lat, lon, matched = m.Coordinates.Y, m.Coordinates.X, m.MatchedAddress
	if _, err := g.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO geocodes (query, lat, lon, matched, source, fetched_at)
		 VALUES (?,?,?,?,'census',?)`, key, lat, lon, matched, now); err != nil {
		return 0, 0, "", err
	}
	return lat, lon, matched, nil
}
