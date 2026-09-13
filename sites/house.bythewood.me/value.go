package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// What the house is worth now, rather than what the county had it down as at the
// last revaluation.
//
// A tax assessment is the only free appraisal there is, and in a rising market it
// is stale by however long ago the county last reappraised. NC counties run four
// to eight years between general reappraisals, so an assessment can be most of a
// cycle behind and every listing then looks overpriced by the same amount.
//
// The FHFA publishes a house price index by metro going back to the 1970s, free
// and with no key, so the assessment can be carried forward from the year it was
// set. It is an index over a whole metro and not a comparable sale, so this
// produces a range and says what it was built from.
const hpiMaster = "https://www.fhfa.gov/hpi/download/monthly/hpi_master.csv"

// Which index covers which county. Hickory-Lenoir-Morganton is Alexander, Burke,
// Caldwell and Catawba, and Iredell is counted into Charlotte. Anything else
// falls back to the state series.
var countyIndex = map[string]string{
	"alexander": "25860",
	"burke":     "25860",
	"caldwell":  "25860",
	"catawba":   "25860",
	"iredell":   "16740",
	"lincoln":   "16740",
}

var indexNames = map[string]string{
	"25860": "the Hickory area",
	"16740": "the Charlotte area",
	"NC":    "North Carolina",
}

type HPI struct {
	db    *sql.DB
	guard *Guard
}

func NewHPI(db *sql.DB) *HPI {
	return &HPI{
		db: db,
		// The whole national file in one request, which is seventeen megabytes, so
		// it is fetched rarely and kept for a quarter. The index only moves once a
		// quarter anyway.
		guard: NewGuard(db, "fhfa-hpi", GuardOpts{
			MinInterval: 30 * time.Second,
			Budget:      8,
			Window:      24 * time.Hour,
			Timeout:     3 * time.Minute,
			UserAgent:   clientUA,
		}),
	}
}

// series is one place's index, by year and quarter, kept as the small part of a
// very large file that is actually wanted.
type series map[string]float64 // "2023-4" -> index

type hpiTable struct {
	Places map[string]series `json:"places"`
	Latest map[string]string `json:"latest"` // place -> "2026-2"
}

func (h *HPI) table(ctx context.Context) (hpiTable, error) {
	var out hpiTable

	var payload string
	var fetched int64
	err := h.db.QueryRowContext(ctx,
		`SELECT payload, fetched_at FROM lookups WHERE kind = 'hpi' AND key = 'master'`).
		Scan(&payload, &fetched)
	if err == nil && time.Since(time.Unix(fetched, 0)) < 90*24*time.Hour {
		if json.Unmarshal([]byte(payload), &out) == nil && len(out.Places) > 0 {
			return out, nil
		}
	} else if err != nil && err != sql.ErrNoRows {
		return out, err
	}

	out, err = h.fetch(ctx)
	if err != nil {
		return out, err
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	_, err = h.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('hpi','master',?,?)`,
		string(raw), time.Now().Unix())
	return out, err
}

// fetch streams the national file and keeps only the handful of places this
// search covers. Decoding all of it would be a hundred thousand rows held in
// memory to answer a question about three of them.
func (h *HPI) fetch(ctx context.Context) (hpiTable, error) {
	out := hpiTable{Places: map[string]series{}, Latest: map[string]string{}}

	want := map[string]bool{"NC": true}
	for _, id := range countyIndex {
		want[id] = true
	}

	err := h.guard.Stream(ctx, hpiMaster, func(body io.Reader) error {
		rd := csv.NewReader(body)
		rd.FieldsPerRecord = -1
		rd.ReuseRecord = true

		head, err := rd.Read()
		if err != nil {
			return err
		}
		col := map[string]int{}
		for i, name := range head {
			col[strings.TrimSpace(name)] = i
		}
		for _, needed := range []string{"hpi_flavor", "frequency", "place_id", "yr", "period", "index_nsa"} {
			if _, ok := col[needed]; !ok {
				return fmt.Errorf("hpi: no %s column", needed)
			}
		}

		// Best first. A metro carries all-transactions and expanded-data but not
		// always purchase-only, and taking whichever arrives last would mix two
		// indexes with different bases into one series.
		rank := map[string]int{"expanded-data": 3, "all-transactions": 2, "purchase-only": 1}
		best := map[string]int{}

		for {
			rec, err := rd.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if len(rec) <= col["index_nsa"] {
				continue
			}
			place := rec[col["place_id"]]
			if !want[place] || rec[col["frequency"]] != "quarterly" {
				continue
			}
			r := rank[rec[col["hpi_flavor"]]]
			if r == 0 || r < best[place] {
				continue
			}
			if r > best[place] {
				best[place] = r
				out.Places[place] = series{}
				delete(out.Latest, place)
			}

			v, err := strconv.ParseFloat(rec[col["index_nsa"]], 64)
			if err != nil || v <= 0 {
				continue
			}
			at := rec[col["yr"]] + "-" + rec[col["period"]]
			out.Places[place][at] = v
			if later(at, out.Latest[place]) {
				out.Latest[place] = at
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if len(out.Places) == 0 {
		return out, fmt.Errorf("hpi: nothing for any place we asked about")
	}
	return out, nil
}

// later compares two "year-quarter" stamps without parsing dates, since the
// quarter is a single digit and the year is always four.
func later(a, b string) bool {
	if b == "" {
		return true
	}
	ay, aq, ok1 := splitStamp(a)
	by, bq, ok2 := splitStamp(b)
	if !ok1 || !ok2 {
		return false
	}
	if ay != by {
		return ay > by
	}
	return aq > bq
}

func splitStamp(s string) (year, quarter int, ok bool) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	y, err1 := strconv.Atoi(parts[0])
	q, err2 := strconv.Atoi(parts[1])
	return y, q, err1 == nil && err2 == nil
}

// ValueEstimate is the assessment carried forward to today.
type ValueEstimate struct {
	// What the county said, and when they said it.
	Assessed float64
	BaseYear int

	// What that is worth at today's index, and the range either side of it. The
	// range is the honest part: an index over a metro is not an appraisal of a
	// house.
	Estimate float64
	Low      float64
	High     float64

	// How much the index moved over the period, as a percentage, and which index.
	MovedPct float64
	Basis    string
	AsOf     string

	Found bool
}

// Estimate carries an assessed value forward from the year it was set.
func (h *HPI) Estimate(ctx context.Context, county string, assessed float64, baseYear int) (ValueEstimate, error) {
	var out ValueEstimate
	if assessed <= 0 || baseYear <= 0 {
		return out, nil
	}

	table, err := h.table(ctx)
	if err != nil {
		return out, err
	}
	return estimateFrom(table, county, assessed, baseYear), nil
}

// estimateFrom is the arithmetic on its own, with no database and no network in
// front of it.
func estimateFrom(table hpiTable, county string, assessed float64, baseYear int) ValueEstimate {
	var out ValueEstimate
	if assessed <= 0 || baseYear <= 0 {
		return out
	}

	place := countyIndex[strings.ToLower(strings.TrimSpace(strings.TrimSuffix(
		strings.TrimSpace(county), " County")))]
	if place == "" || table.Places[place] == nil {
		place = "NC"
	}
	s, ok := table.Places[place]
	if !ok {
		return out
	}

	// A revaluation is effective on 1 January, so the index that matches it is the
	// last quarter of the year before.
	from, ok := quarterNear(s, baseYear-1, 4)
	if !ok {
		return out
	}
	now, ok := s[table.Latest[place]]
	if !ok || now <= 0 {
		return out
	}

	out.Found = true
	out.Assessed = assessed
	out.BaseYear = baseYear
	out.MovedPct = (now/from - 1) * 100
	out.Estimate = assessed * now / from
	// Fifteen per cent either side. An index says what the metro did and not what
	// this house did, and a single figure would be read as an appraisal.
	out.Low = out.Estimate * 0.85
	out.High = out.Estimate * 1.15
	out.Basis = indexNames[place]
	if out.Basis == "" {
		out.Basis = "North Carolina"
	}
	out.AsOf = prettyStamp(table.Latest[place])
	return out
}

// quarterNear takes the asked-for quarter, or the closest one in that year, since
// a series can start or stop mid-year.
func quarterNear(s series, year, quarter int) (float64, bool) {
	if v, ok := s[fmt.Sprintf("%d-%d", year, quarter)]; ok && v > 0 {
		return v, true
	}
	for q := 4; q >= 1; q-- {
		if v, ok := s[fmt.Sprintf("%d-%d", year, q)]; ok && v > 0 {
			return v, true
		}
	}
	return 0, false
}

func prettyStamp(s string) string {
	y, q, ok := splitStamp(s)
	if !ok {
		return s
	}
	return fmt.Sprintf("Q%d %d", q, y)
}
