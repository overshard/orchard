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

// Owner occupancy on the actual street, which is what most people are pointing
// at when they say a neighbourhood is good. A street that is eighty percent
// owner occupied behaves differently from one that is half rentals.
//
// It is in the tax records and needs no survey: every parcel carries both where
// the property is and where the owner's post goes. Post going three towns over,
// to a PO box or to a company means the house is let.
//
// This counts the houses you would actually see from the drive. The census
// figures in area.json stay as the fallback for when the parcels cannot be read.
const streetRadiusFeet = 900

type StreetResult struct {
	Parcels       int     `json:"parcels"`
	OwnerOccupied int     `json:"owner_occupied"`
	Percent       float64 `json:"percent"`

	// Houses with nobody's post going to them, which is a second sort of empty:
	// no mailing address at all on the tax roll.
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
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('street',?,?,?)`,
		key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

// tallyStreet is split out so the comparison can be tested without a network
// call, which matters because the whole thing turns on one fuzzy string match.
func tallyStreet(res esriQueryResult) StreetResult {
	var out StreetResult

	for _, f := range res.Features {
		a := f.Attributes

		site := siteAddressOf(a)
		if site == "" {
			continue
		}
		// Only houses. A church, a school and a stretch of woodland are all
		// parcels and none of them is a neighbour.
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

	// Under about eight houses it is not a street, it is a few neighbours, and a
	// percentage of six is noise rather than a measurement.
	if out.Parcels >= 8 {
		out.Found = true
		counted := out.Parcels - out.Unknown
		if counted > 0 {
			out.Percent = float64(out.OwnerOccupied) / float64(counted) * 100
		}
	}
	return out
}

// A parcel use that is somebody's home. The tax roll spells this a dozen ways and
// leaves it blank often enough that blank has to count as residential, since most
// parcels on a residential street are houses.
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

// sameAddress decides whether the owner's post goes to the house itself. The two
// strings are written by different clerks in different decades, so this compares
// the house number and the street name rather than the whole line, and a PO box
// is never the house.
func sameAddress(mail, mailZip, site, siteZip string, a map[string]any) bool {
	if isPOBox(mail) {
		return false
	}
	// A different postcode is a different place, and that alone settles most of
	// the lets.
	mz, sz := zipOf(mailZip), zipOf(siteZip)
	if mz != "" && sz != "" && mz != sz {
		return false
	}
	return sameHouse(mail, site)
}

// sameHouse compares the house number and the street name and stops there. The
// tax roll writes the site address from its own columns and leaves the street
// type out about half the time, so "118 Marchbank" and "118 MARCHBANK RD" are
// the same house, where a whole-string compare calls them different.
func sameHouse(a, b string) bool {
	na, sa := houseParts(a)
	nb, sb := houseParts(b)
	return na != "" && na == nb && sa != "" && sa == sb
}

// houseParts pulls the number and the first real word of the street name out of
// an address line, dropping a leading directional so "118 N Main" and "118 Main"
// do not disagree.
func houseParts(addr string) (number, street string) {
	fields := strings.Fields(strings.ToLower(addr))
	for i, f := range fields {
		f = strings.Trim(f, ".,#")
		if f == "" {
			continue
		}
		if number == "" {
			// The first thing that is a number is the house number, and anything
			// before it is a unit or a name and is not wanted.
			if num(f) != 0 && !strings.ContainsAny(f, "abcdefghijklmnopqrstuvwxyz") {
				number = f
			}
			continue
		}
		if directions[f] {
			continue
		}
		// The first word after the number that is not a direction is the street.
		street = nonAlnum.ReplaceAllString(f, "")
		if street != "" {
			return number, street
		}
		_ = i
	}
	return number, street
}

// A mailing address that is a box at the post office is never the house. It
// lives here because this is the only thing left that reads an address written
// by a county clerk.
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
