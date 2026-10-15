package property

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The Census geocoder needs no key and answers with a point interpolated on the
// street, not a rooftop. Nominatim's terms cap bulk geocoding and OSM's rural NC
// address points are thinner than TIGER's.
const censusGeocodeURL = "https://geocoding.geo.census.gov/geocoder/locations/onelineaddress"

type Geocoder struct {
	db    *sql.DB
	guard *Guard
}

func NewGeocoder(db *sql.DB) *Geocoder {
	return &Geocoder{
		db: db,
		// The service publishes no rate limit, which is a reason to be careful
		// and not a licence.
		guard: NewGuard(db, "census-geocoder", GuardOpts{
			MinInterval: 1500 * time.Millisecond,
			Budget:      60,
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

// Geocode is cached forever, misses included, since an address doesn't move.
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
		// Not cached as a miss, since a guarded or failed call says nothing
		// about the address.
		return 0, 0, "", err
	}

	now := time.Now().Unix()
	if len(out.Result.AddressMatches) == 0 {
		if _, err := keep(ctx, g.db,
			`INSERT OR REPLACE INTO geocodes (query, lat, lon, matched, source, fetched_at)
			 VALUES (?, NULL, NULL, NULL, 'census', ?)`, key, now); err != nil {
			return 0, 0, "", err
		}
		return 0, 0, "", fmt.Errorf("no geocode match for %q", address)
	}

	m := out.Result.AddressMatches[0]
	lat, lon, matched = m.Coordinates.Y, m.Coordinates.X, m.MatchedAddress
	if _, err := keep(ctx, g.db,
		`INSERT OR REPLACE INTO geocodes (query, lat, lon, matched, source, fetched_at)
		 VALUES (?,?,?,?,'census',?)`, key, lat, lon, matched, now); err != nil {
		return 0, 0, "", err
	}
	return lat, lon, matched, nil
}
