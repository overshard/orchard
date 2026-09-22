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

// What the county already knows about a lot, which is most of what would
// otherwise have to be typed: the deeded acreage, what it is assessed at, and
// which county it is actually in.
//
// One statewide service rather than one per county. The county servers each
// publish their own parcel layer and two of the three nearby are unusable:
// Alexander's declares a world projection while holding state plane coordinates,
// so ArcGIS reprojects it into the Gulf of Guinea and a point query matches
// nothing, and Catawba's publishes geometry with no attributes at all. NC OneMap
// aggregates all hundred counties, declares EPSG 2264 correctly, and names the
// assessor it came from.
const ncParcels = "https://services.nconemap.gov/secure/rest/services/NC1Map_Parcels/MapServer/1/query"

// How far to look around a geocoded point. The Census geocoder interpolates along
// the street centreline, so the coordinate it returns sits in the road rather
// than on the lot, and a point query matches nothing at all. A generous envelope
// and the nearest parcel wins.
const parcelSearchFeet = 220

type ParcelResult struct {
	County    string
	Acres     float64
	AcresFrom string

	// What the assessor has it down as, split the way they split it.
	MarketValue float64
	LandValue   float64
	BuildValue  float64

	// The address the assessor holds against the parcel, which is worth comparing
	// with the one that was pasted in.
	Address string

	// How the parcel is used and when it last changed hands, both straight off the
	// tax roll.
	Use      string
	LastSold string

	// The middle of the lot. Map links are built from this rather than from the
	// geocoded point, which sits in the road and lands the pin at the neighbour's.
	Lat float64
	Lon float64

	Source string
	Found  bool
}

// The owner's name is on this layer and is not read. It is a third
// party's information, it has no bearing on whether a house suits a family, and
// the less of it that ends up in a database on a desktop the better.

type Parcels struct {
	db    *sql.DB
	guard *Guard
}

func NewParcels(db *sql.DB) *Parcels {
	return &Parcels{
		db: db,
		// A state service rather than one county's single machine, so this can be
		// a little brisker than the county guards, but not much.
		guard: NewGuard(db, "nc-parcels", GuardOpts{
			MinInterval: 2 * time.Second,
			Budget:      60,
			Window:      time.Hour,
			Timeout:     45 * time.Second,
		}),
	}
}

var parcelFields = []string{
	"gisacres", "parval", "landval", "improvval",
	"siteadd", "saddno", "saddstr", "saddsttyp", "scity",
	"cntyname", "parusedesc", "saledatetx", "sourceagnt",
}

func (p *Parcels) Lookup(ctx context.Context, lat, lon float64, county, address string) (ParcelResult, error) {
	key := fmt.Sprintf("%.5f,%.5f", lat, lon)

	var payload string
	err := p.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = 'parcel3' AND key = ?`, key).Scan(&payload)
	if err == nil {
		var cached ParcelResult
		if json.Unmarshal([]byte(payload), &cached) == nil {
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return ParcelResult{}, err
	}

	xmin, ymin, xmax, ymax := envelopeAround(lat, lon, parcelSearchFeet)
	v := url.Values{
		"geometry":          {fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", xmin, ymin, xmax, ymax)},
		"geometryType":      {"esriGeometryEnvelope"},
		"inSR":              {"4326"},
		"outSR":             {"4326"},
		"spatialRel":        {"esriSpatialRelIntersects"},
		"outFields":         {strings.Join(parcelFields, ",")},
		"returnGeometry":    {"true"},
		"geometryPrecision": {"7"},
		"f":                 {"json"},
	}

	var res esriQueryResult
	if err := p.guard.Get(ctx, ncParcels+"?"+v.Encode(), &res); err != nil {
		return ParcelResult{}, err
	}

	out := p.pick(res, point{lat, lon}, address)

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := p.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('parcel3',?,?,?)`,
		key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

// pick chooses the parcel for an address. The geocoder puts its point in the road
// rather than on the lot, so the nearest polygon is often the neighbour's and the
// assessed value then belongs to the wrong house. The tax roll carries a site
// address, so when one of the candidates is the address that was typed it wins
// outright and geometry only decides between the rest.
func (p *Parcels) pick(res esriQueryResult, at point, address string) ParcelResult {
	var out ParcelResult
	best := math.Inf(1)
	var chosen *esriFeature

	if strings.TrimSpace(address) != "" {
		want := addressKey(address, "")
		for i := range res.Features {
			site := tidyParcelAddress(res.Features[i].Attributes)
			if site == "" {
				continue
			}
			if addressKey(site, "") == want {
				chosen = &res.Features[i]
				best = 0
				break
			}
		}
	}

	if chosen == nil {
		for i := range res.Features {
			d := res.Features[i].Geometry.distanceFeet(at)
			if d < best {
				best, chosen = d, &res.Features[i]
			}
		}
	}
	if chosen == nil || best > parcelSearchFeet {
		return out
	}

	a := chosen.Attributes
	out.Found = true
	out.County = strings.TrimSpace(attrString(a, "cntyname"))
	out.Source = firstNonEmpty(attrString(a, "sourceagnt"), "NC OneMap parcels")
	out.Use = attrString(a, "parusedesc")

	if acres, ok := attrFloat(a, "gisacres"); ok && acres > 0 {
		out.Acres, out.AcresFrom = acres, "off the county's own parcel map"
	} else if acres := chosen.Geometry.acres(at); acres > 0 {
		out.Acres, out.AcresFrom = acres, "measured off the parcel shape"
	}

	out.MarketValue, _ = attrFloat(a, "parval")
	out.LandValue, _ = attrFloat(a, "landval")
	out.BuildValue, _ = attrFloat(a, "improvval")

	out.Address = tidyParcelAddress(a)
	out.LastSold = tidySaleDate(attrString(a, "saledatetx"))
	if c, ok := chosen.Geometry.centroid(); ok {
		out.Lat, out.Lon = c.lat, c.lon
	}
	return out
}

// The site address arrives both as one padded string and as its parts, and the
// parts are cleaner when they are filled in.
func tidyParcelAddress(a map[string]any) string {
	no := strings.TrimSpace(attrString(a, "saddno"))
	street := strings.TrimSpace(attrString(a, "saddstr"))
	suffix := strings.TrimSpace(attrString(a, "saddsttyp"))
	if no != "" && street != "" {
		return titleAddress(strings.Join([]string{no, street, suffix}, " "))
	}
	if one := strings.Join(strings.Fields(attrString(a, "siteadd")), " "); one != "" {
		return titleAddress(one)
	}
	return ""
}

// The sale date is an integer of the form 20041231, which is a date only once
// somebody writes it out.
func tidySaleDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) != 8 || raw == "00000000" {
		return ""
	}
	t, err := time.Parse("20060102", raw)
	if err != nil {
		return ""
	}
	return t.Format("January 2006")
}
