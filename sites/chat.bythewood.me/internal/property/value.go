package property

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

// NC counties run four to eight years between reappraisals, so an assessment is
// carried forward on FHFA's keyless metro price index. That's an index and not a
// comparable sale, so it gives a range.
const hpiMaster = "https://www.fhfa.gov/hpi/download/monthly/hpi_master.csv"

// Anything not listed falls back to the state series.
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
		// The whole national file is tens of megabytes, and the index only moves
		// once a quarter.
		guard: NewGuard(db, "fhfa-hpi", GuardOpts{
			MinInterval: 30 * time.Second,
			Budget:      8,
			Window:      24 * time.Hour,
			Timeout:     3 * time.Minute,
			UserAgent:   clientUA,
		}),
	}
}

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

// fetch streams the national file and keeps only the places this search covers.
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

		// Taking whichever flavor arrives last would mix two indexes with
		// different bases into one series.
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

type ValueEstimate struct {
	Assessed float64
	BaseYear int

	// The range matters since an index over a metro isn't an appraisal of a
	// house.
	Estimate float64
	Low      float64
	High     float64

	MovedPct float64
	Basis    string
	AsOf     string

	Found bool
}

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

	// A revaluation takes effect on 1 January, so it matches the last quarter of
	// the year before.
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
	// A single figure would be read as an appraisal.
	out.Low = out.Estimate * 0.85
	out.High = out.Estimate * 1.15
	out.Basis = indexNames[place]
	if out.Basis == "" {
		out.Basis = "North Carolina"
	}
	out.AsOf = prettyStamp(table.Latest[place])
	return out
}

// A series can start or stop mid-year.
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
