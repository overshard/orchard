package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The guaranteed path. Neither Zillow nor Realtor.com has a free sanctioned
// listing API, and scraping either gets blocked and breaks their terms, so the
// first adapter is a file a buyer's agent exports from the MLS and drops in.
// Everything downstream works the same whether the rows came from here or from a
// feed, which is the point of normalizing at the edge.
type CSVAdapter struct {
	dir string
}

func NewCSVAdapter(dir string) *CSVAdapter { return &CSVAdapter{dir: dir} }

func (a *CSVAdapter) Name() string { return "csv" }

// Column names differ by MLS and by export template, so every field has a list of
// spellings. Matching is on a squashed lowercase form, which makes "List Price",
// "list_price" and "ListPrice" the same header.
//
// Redfin's own Download All export is in here alongside the MLS ones, because it
// is the shortest path to real listings: it is a button on a search page, it needs
// no account and no key, and it carries a coordinate, an MLS number and a link
// back to the listing. Its own spellings are odd enough to be worth naming, in
// particular "ZIP OR POSTAL CODE", "HOA/MONTH" and a URL column whose header
// carries a sentence of explanation after it.
var csvFields = map[string][]string{
	"mls":     {"mls", "mlsnumber", "mlsid", "listingid", "listid", "mls#"},
	"address": {"address", "streetaddress", "fulladdress", "unparsedaddress", "propertyaddress", "street"},
	"city":    {"city", "cityname"},
	"state":   {"state", "stateorprovince"},
	"zip":     {"zip", "zipcode", "postalcode", "ziporpostalcode"},
	"county":  {"county", "countyorparish"},
	"lat":     {"lat", "latitude"},
	"lon":     {"lon", "lng", "long", "longitude"},
	"price":   {"price", "listprice", "currentprice", "askingprice", "lp"},
	"beds":    {"beds", "bedrooms", "bedroomstotal", "br"},
	"baths":   {"baths", "bathrooms", "bathroomstotal", "bathroomsfull", "ba", "bathstotal"},
	"sqft":    {"sqft", "squarefeet", "livingarea", "heatedsqft", "buildingareatotal", "totalsqft"},
	"acres":   {"acres", "lotsizeacres", "lotacres", "lotsizearea"},
	// Redfin reports the lot in square feet under a header that does not say so.
	"lotsqft":  {"lotsize", "lotsizesqft", "lotsizesquarefeet", "lotsf"},
	"year":     {"yearbuilt", "year", "builtyear"},
	"style":    {"style", "architecturalstyle", "housestyle", "construction", "structuretype"},
	"type":     {"propertytype", "propertysubtype", "type", "subtype"},
	"source":   {"source"},
	"hoa":      {"hoafee", "hoa", "associationfee", "hoamonthly", "hoamonth"},
	"tax":      {"taxannual", "taxes", "annualtax", "taxannualamount", "propertytax"},
	"dom":      {"dom", "daysonmarket", "cdom", "days"},
	"status":   {"status", "standardstatus", "mlsstatus", "listingstatus", "saletype"},
	"url":      {"url", "listingurl", "link", "weburl", "virtualtoururl"},
	"remarks":  {"remarks", "publicremarks", "description", "comments"},
	"photos":   {"photos", "photourls", "images", "media", "photo"},
	"photo1":   {"photourl", "primaryphoto", "mainphoto", "image", "picture", "photo1"},
	"listdate": {"listdate", "listingcontractdate", "onmarketdate", "datelisted"},
}

func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '#' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Fetch reads every .csv in the import directory, newest last so a later export
// wins on a field the earlier one left blank.
func (a *CSVAdapter) Fetch(ctx context.Context) ([]Listing, error) {
	paths, err := filepath.Glob(filepath.Join(a.dir, "*.csv"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	var out []Listing
	for _, p := range paths {
		rows, err := a.readFile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		out = append(out, rows...)
	}
	return out, nil
}

func (a *CSVAdapter) readFile(path string) ([]Listing, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	// Exports vary in column count between header and body often enough that a
	// strict reader rejects files that are otherwise fine.
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	header, err := r.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// column name -> index, resolved once per file. Exact match first across the
	// whole header, then a prefix pass for the one export that writes a sentence
	// into a column name: Redfin's URL header carries a note about pricing after
	// the word URL.
	index := map[string]int{}
	for _, exact := range []bool{true, false} {
		for i, h := range header {
			sq := squash(h)
			for field, names := range csvFields {
				if _, taken := index[field]; taken {
					continue
				}
				for _, n := range names {
					if (exact && sq == n) || (!exact && len(sq) > len(n) && strings.HasPrefix(sq, n)) {
						index[field] = i
						break
					}
				}
			}
		}
	}
	if _, ok := index["address"]; !ok {
		return nil, fmt.Errorf("no address column, found: %s", strings.Join(header, ", "))
	}

	source := "csv:" + filepath.Base(path)
	var out []Listing

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(field string) string {
			i, ok := index[field]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}

		l := Listing{
			MLS:          get("mls"),
			Source:       source,
			Address:      get("address"),
			City:         get("city"),
			State:        get("state"),
			Zip:          get("zip"),
			County:       strings.TrimSuffix(strings.TrimSpace(get("county")), " County"),
			Lat:          num(get("lat")),
			Lon:          num(get("lon")),
			Price:        int(num(get("price"))),
			Beds:         num(get("beds")),
			Baths:        num(get("baths")),
			SqFt:         int(num(get("sqft"))),
			Acres:        num(get("acres")),
			YearBuilt:    int(num(get("year"))),
			Style:        get("style"),
			PropertyType: get("type"),
			HOAMonthly:   num(get("hoa")),
			TaxAnnual:    num(get("tax")),
			DaysOnMarket: int(num(get("dom"))),
			Status:       get("status"),
			URL:          get("url"),
			Remarks:      get("remarks"),
		}
		// A lot reported in square feet, which is what Redfin exports and what an
		// MLS export sometimes does under a header that does not say the unit.
		// Anything over five hundred is square feet: nobody has a five hundred
		// acre listing in this price band and nobody has a half acre expressed as
		// 0.5 square feet.
		if l.Acres == 0 {
			if sf := num(get("lotsqft")); sf > 0 {
				if sf > 500 {
					l.Acres = sf / sqFtPerAcre
				} else {
					l.Acres = sf
				}
			}
		}
		// Redfin names the originating MLS in its own column, which is worth
		// keeping when the remarks are empty.
		if l.Remarks == "" && get("source") != "" {
			l.Remarks = "Listed on " + get("source") + "."
		}

		if l.Address == "" || l.Price == 0 {
			continue
		}
		l.Photos = photoList(get("photos"), get("photo1"))
		out = append(out, l)
	}
	return out, nil
}

// num parses a number out of whatever an export put in the cell: $250,000 and
// "1.25 ac" and a bare float all have to come back as one.
func num(s string) float64 {
	var b strings.Builder
	seenDot := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' && !seenDot:
			seenDot = true
			b.WriteRune(r)
		case r == '-' && b.Len() == 0:
			b.WriteRune(r)
		}
	}
	v, err := strconv.ParseFloat(b.String(), 64)
	if err != nil {
		return 0
	}
	return v
}

// photoList splits whichever photo column the export had. Separators vary, so
// all three common ones are honoured.
func photoList(multi, single string) []string {
	raw := multi
	if raw == "" {
		raw = single
	}
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == '\n' || r == ' '
	})
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "http") {
			out = append(out, p)
		}
	}
	return out
}
