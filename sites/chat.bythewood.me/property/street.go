package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Owner occupancy on the street comes from the tax roll, where every parcel has
// both its site address and where the owner's post goes. Post going elsewhere
// means the house is let.
const streetRadiusFeet = 900

type StreetResult struct {
	Parcels       int     `json:"parcels"`
	OwnerOccupied int     `json:"owner_occupied"`
	Percent       float64 `json:"percent"`

	// No mailing address at all on the tax roll.
	Unknown int  `json:"unknown"`
	Found   bool `json:"found"`
}

type Street struct {
	db      *sql.DB
	parcels *Parcels
}

func NewStreet(db *sql.DB, p *Parcels) *Street { return &Street{db: db, parcels: p} }

var streetFields = []string{"mailadd", "mcity", "mstate", "mzip", "siteadd", "scity", "szip",
	"saddno", "saddstr", "saddsttyp", "parusedesc"}

func (s *Street) Lookup(ctx context.Context, lat, lon float64) (StreetResult, error) {
	key := fmt.Sprintf("%.4f,%.4f", lat, lon)

	var payload string
	err := s.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = 'street' AND key = ?`, key).Scan(&payload)
	if err == nil {
		var cached StreetResult
		if json.Unmarshal([]byte(payload), &cached) == nil {
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return StreetResult{}, err
	}

	xmin, ymin, xmax, ymax := envelopeAround(lat, lon, streetRadiusFeet)
	v := url.Values{
		"geometry":       {fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", xmin, ymin, xmax, ymax)},
		"geometryType":   {"esriGeometryEnvelope"},
		"inSR":           {"4326"},
		"spatialRel":     {"esriSpatialRelIntersects"},
		"outFields":      {strings.Join(streetFields, ",")},
		"returnGeometry": {"false"},
		"f":              {"json"},
	}

	var res esriQueryResult
	if err := s.parcels.guard.Get(ctx, ncParcels+"?"+v.Encode(), &res); err != nil {
		return StreetResult{}, err
	}

	out := tallyStreet(res)

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := keep(ctx, s.db,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('street',?,?,?)`,
		key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

func tallyStreet(res esriQueryResult) StreetResult {
	var out StreetResult

	for _, f := range res.Features {
		a := f.Attributes

		site := siteAddressOf(a)
		if site == "" {
			continue
		}
		if !residential(attrString(a, "parusedesc")) {
			continue
		}
		out.Parcels++

		mail := strings.TrimSpace(attrString(a, "mailadd"))
		if mail == "" {
			out.Unknown++
			continue
		}
		if sameAddress(mail, attrString(a, "mzip"), site, attrString(a, "szip"), a) {
			out.OwnerOccupied++
		}
	}

	// A percentage of six houses is noise.
	if out.Parcels >= 8 {
		out.Found = true
		counted := out.Parcels - out.Unknown
		if counted > 0 {
			out.Percent = float64(out.OwnerOccupied) / float64(counted) * 100
		}
	}
	return out
}

// The tax roll spells this a dozen ways and leaves it blank often enough that
// blank counts as residential.
func residential(use string) bool {
	u := strings.ToLower(use)
	if u == "" {
		return true
	}
	for _, no := range []string{"church", "school", "commercial", "industrial", "utility",
		"cemetery", "government", "institutional", "agricultur", "vacant", "forest"} {
		if strings.Contains(u, no) {
			return false
		}
	}
	return true
}

func siteAddressOf(a map[string]any) string {
	no := strings.TrimSpace(attrString(a, "saddno"))
	street := strings.TrimSpace(attrString(a, "saddstr"))
	if no != "" && street != "" {
		return no + " " + street + " " + strings.TrimSpace(attrString(a, "saddsttyp"))
	}
	return strings.Join(strings.Fields(attrString(a, "siteadd")), " ")
}

// sameAddress compares the house number and street name instead of the whole
// line, since different clerks wrote the two strings in different decades.
func sameAddress(mail, mailZip, site, siteZip string, a map[string]any) bool {
	if isPOBox(mail) {
		return false
	}
	// A different postcode alone settles most of the lets.
	mz, sz := zipOf(mailZip), zipOf(siteZip)
	if mz != "" && sz != "" && mz != sz {
		return false
	}
	return sameHouse(mail, site)
}

// The tax roll leaves the street type out about half the time, so "402 Sample"
// and "402 SAMPLE RD" have to be the same house.
func sameHouse(a, b string) bool {
	na, sa := houseParts(a)
	nb, sb := houseParts(b)
	return na != "" && na == nb && sa != "" && sa == sb
}

// houseParts drops a leading directional so "118 N Main" and "118 Main" agree.
func houseParts(addr string) (number, street string) {
	fields := strings.Fields(strings.ToLower(addr))
	for i, f := range fields {
		f = strings.Trim(f, ".,#")
		if f == "" {
			continue
		}
		if number == "" {
			// Anything before the house number is a unit or a name.
			if num(f) != 0 && !strings.ContainsAny(f, "abcdefghijklmnopqrstuvwxyz") {
				number = f
			}
			continue
		}
		if directions[f] {
			continue
		}
		street = nonAlnum.ReplaceAllString(f, "")
		if street != "" {
			return number, street
		}
		_ = i
	}
	return number, street
}

func isPOBox(addr string) bool {
	a := strings.ToLower(strings.ReplaceAll(addr, ".", ""))
	return strings.Contains(a, "po box") || strings.Contains(a, "p o box") ||
		strings.HasPrefix(a, "box ")
}

func zipOf(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 5 {
		return s[:5]
	}
	return ""
}

func (s StreetResult) SpaceWhy() string {
	if !s.Found {
		return "the county has too few records around here to count the neighbours"
	}
	switch {
	case s.Parcels <= 8:
		return fmt.Sprintf("%d houses within a few hundred yards, so barely a neighbour in sight", s.Parcels)
	case s.Parcels <= 20:
		return fmt.Sprintf("%d houses within a few hundred yards, a lane rather than a street", s.Parcels)
	default:
		return fmt.Sprintf("%d houses within a few hundred yards, which is a proper street", s.Parcels)
	}
}
