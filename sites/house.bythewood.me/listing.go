package main

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// Listing is the normalized shape every adapter produces. Nothing downstream of
// an adapter knows where a row came from, which is what lets a CSV export stand
// in for an MLS feed that does not exist yet.
type Listing struct {
	MLS          string
	Source       string
	Address      string
	City         string
	State        string
	Zip          string
	County       string
	Lat          float64
	Lon          float64
	Price        int
	Beds         float64
	Baths        float64
	SqFt         int
	Acres        float64
	YearBuilt    int
	Style        string
	PropertyType string
	HOAMonthly   float64
	TaxAnnual    float64
	DaysOnMarket int
	Status       string
	URL          string
	Remarks      string
	Photos       []string
}

// Adapter is a listing source. Three are planned and only the first is real:
// a CSV export an agent sends, a RESO Web API feed once there are credentials,
// and a paid third party API if one turns out to be worth the money.
type Adapter interface {
	Name() string
	Fetch(ctx context.Context) ([]Listing, error)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

var streetAbbrev = map[string]string{
	"street": "st", "road": "rd", "drive": "dr", "lane": "ln", "avenue": "ave",
	"court": "ct", "circle": "cir", "place": "pl", "trail": "trl", "highway": "hwy",
	"boulevard": "blvd", "parkway": "pkwy", "terrace": "ter", "north": "n",
	"south": "s", "east": "e", "west": "w", "northeast": "ne", "northwest": "nw",
	"southeast": "se", "southwest": "sw",
}

// addressKey normalizes an address enough that "1234 Marchbank Road" and
// "1234 Marchbank Rd." are one house. Dedupe has three rungs and this is the
// second, so it only has to be better than string equality, not perfect.
func addressKey(addr, zip string) string {
	words := strings.Fields(strings.ToLower(addr))
	for i, w := range words {
		w = strings.Trim(w, ".,")
		if full, ok := streetAbbrev[w]; ok {
			w = full
		}
		words[i] = w
	}
	key := nonAlnum.ReplaceAllString(strings.Join(words, " "), "")
	return key + "|" + strings.TrimSpace(strings.SplitN(zip, "-", 2)[0])
}

// haversineFeet is the great circle distance in feet. Used for dedupe proximity
// and for water buffers, never for a commute: the roads here do not run straight
// and a straight line through a ridge is not a drive.
func haversineFeet(lat1, lon1, lat2, lon2 float64) float64 {
	const earthFeet = 20902231.0 // mean Earth radius, 6371 km in feet
	p := math.Pi / 180
	dLat := (lat2 - lat1) * p
	dLon := (lon2 - lon1) * p
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*p)*math.Cos(lat2*p)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthFeet * math.Asin(math.Min(1, math.Sqrt(a)))
}

// bearingDeg is the initial compass bearing from one point to another, clockwise
// from true north. The search arc is expressed in these.
func bearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	p := math.Pi / 180
	y := math.Sin((lon2-lon1)*p) * math.Cos(lat2*p)
	x := math.Cos(lat1*p)*math.Sin(lat2*p) -
		math.Sin(lat1*p)*math.Cos(lat2*p)*math.Cos((lon2-lon1)*p)
	return math.Mod(math.Atan2(y, x)/p+360, 360)
}

// The radius inside which two listings with no MLS number in common and
// different looking addresses are still treated as the same house. About 150
// feet, which is tighter than a rural lot and wider than geocoder jitter on the
// same parcel.
const dedupeFeet = 150

// Upsert writes a listing and returns its id, whether it is new, and whether the
// price moved. Dedupe runs MLS number, then normalized address, then proximity,
// since the same house arriving three times from three sources is a failure.
func Upsert(ctx context.Context, db *sql.DB, l Listing, now time.Time) (id int64, isNew bool, priceMoved bool, err error) {
	ts := now.Unix()

	id, err = findExisting(ctx, db, l)
	if err != nil {
		return 0, false, false, err
	}

	if id == 0 {
		pid, err := newPublicID()
		if err != nil {
			return 0, false, false, err
		}
		res, err := db.ExecContext(ctx, `
			INSERT INTO listings (mls, source, address, city, state, zip, county, lat, lon,
				price, beds, baths, sqft, acres, year_built, style, property_type,
				hoa_monthly, tax_annual, days_on_market, status, listing_url, remarks,
				first_seen, last_seen, public_id)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			l.MLS, l.Source, l.Address, l.City, l.State, l.Zip, l.County, nullF(l.Lat), nullF(l.Lon),
			l.Price, nullF(l.Beds), nullF(l.Baths), nullI(l.SqFt), nullF(l.Acres), nullI(l.YearBuilt),
			l.Style, l.PropertyType, l.HOAMonthly, l.TaxAnnual, l.DaysOnMarket, l.Status, l.URL, l.Remarks,
			ts, ts, pid)
		if err != nil {
			return 0, false, false, fmt.Errorf("insert listing: %w", err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return 0, false, false, err
		}
		isNew = true
	} else {
		var oldPrice int
		if err := db.QueryRowContext(ctx, `SELECT price FROM listings WHERE id = ?`, id).Scan(&oldPrice); err != nil {
			return 0, false, false, err
		}
		priceMoved = oldPrice != l.Price

		// A refresh updates what a listing reports and never touches first_seen,
		// which is the only thing here that says how long a house has been
		// sitting.
		//
		// A field the incoming row does not carry keeps what is already there.
		// The API has no photographs or remarks and an agent's export has no
		// coordinate, so a blank from one must not wipe a real value from the
		// other. Price, status and days on market are the exceptions.
		if _, err := db.ExecContext(ctx, `
			UPDATE listings SET
				mls = COALESCE(NULLIF(?, ''), mls),
				source = ?,
				address = ?, city = ?, state = ?, zip = ?,
				county = COALESCE(NULLIF(?, ''), county),
				lat = COALESCE(?, lat), lon = COALESCE(?, lon),
				price = ?,
				beds = COALESCE(?, beds), baths = COALESCE(?, baths),
				sqft = COALESCE(?, sqft), acres = COALESCE(?, acres),
				year_built = COALESCE(?, year_built),
				style = COALESCE(NULLIF(?, ''), style),
				property_type = COALESCE(NULLIF(?, ''), property_type),
				hoa_monthly = COALESCE(NULLIF(?, 0), hoa_monthly),
				tax_annual = COALESCE(NULLIF(?, 0), tax_annual),
				days_on_market = ?, status = ?,
				listing_url = COALESCE(NULLIF(?, ''), listing_url),
				remarks = COALESCE(NULLIF(?, ''), remarks),
				last_seen = ?, gone_at = NULL
			WHERE id = ?`,
			l.MLS, l.Source, l.Address, l.City, l.State, l.Zip, l.County,
			nullF(l.Lat), nullF(l.Lon), l.Price, nullF(l.Beds), nullF(l.Baths),
			nullI(l.SqFt), nullF(l.Acres), nullI(l.YearBuilt), l.Style, l.PropertyType,
			l.HOAMonthly, l.TaxAnnual, l.DaysOnMarket, l.Status, l.URL, l.Remarks, ts, id); err != nil {
			return 0, false, false, fmt.Errorf("update listing: %w", err)
		}
	}

	if _, err := db.ExecContext(ctx,
		`INSERT OR REPLACE INTO price_history (listing_id, price, seen_at) VALUES (?,?,?)`,
		id, l.Price, ts); err != nil {
		return 0, false, false, err
	}

	if len(l.Photos) > 0 {
		if _, err := db.ExecContext(ctx, `DELETE FROM photos WHERE listing_id = ?`, id); err != nil {
			return 0, false, false, err
		}
		for i, u := range l.Photos {
			if _, err := db.ExecContext(ctx,
				`INSERT OR REPLACE INTO photos (listing_id, idx, url) VALUES (?,?,?)`, id, i, u); err != nil {
				return 0, false, false, err
			}
		}
	}

	return id, isNew, priceMoved, nil
}

func findExisting(ctx context.Context, db *sql.DB, l Listing) (int64, error) {
	if l.MLS != "" {
		var id int64
		err := db.QueryRowContext(ctx, `SELECT id FROM listings WHERE mls = ?`, l.MLS).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}

	want := addressKey(l.Address, l.Zip)
	rows, err := db.QueryContext(ctx, `SELECT id, address, zip, lat, lon FROM listings`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var proximity int64
	for rows.Next() {
		var id int64
		var addr, zip string
		var lat, lon sql.NullFloat64
		if err := rows.Scan(&id, &addr, &zip, &lat, &lon); err != nil {
			return 0, err
		}
		if addressKey(addr, zip) == want {
			return id, nil
		}
		if proximity == 0 && l.Lat != 0 && lat.Valid && lon.Valid &&
			haversineFeet(l.Lat, l.Lon, lat.Float64, lon.Float64) < dedupeFeet {
			proximity = id
		}
	}
	return proximity, rows.Err()
}

func nullF(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullI(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
