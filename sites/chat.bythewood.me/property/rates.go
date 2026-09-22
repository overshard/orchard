package property

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The rate a payment is worked out at, from Freddie Mac's Primary Mortgage Market
// Survey. It is the one free, keyless, published mortgage rate in the country:
// every lender rate table is behind a lead form and every aggregator wants a key.
//
// PMMS is a survey of what lenders quoted that week to a borrower with good
// credit putting 20% down on a conforming conventional loan, so it is the anchor
// and not the answer. What FHA, VA and USDA go out at is a spread off it, and a
// real quote from a real lender will differ by a quarter either way.
const pmmsURL = "https://www.freddiemac.com/pmms/docs/PMMS_history.csv"

// The survey publishes on a Thursday, so a day is plenty and nothing here ever
// asks Freddie Mac twice in a morning.
const pmmsTTL = 12 * time.Hour

type Rates struct {
	db    *sql.DB
	guard *Guard
}

func NewRates(db *sql.DB) *Rates {
	return &Rates{
		db: db,
		guard: NewGuard(db, "freddiemac", GuardOpts{
			MinInterval: 5 * time.Second,
			Budget:      20,
			Window:      24 * time.Hour,
			Timeout:     30 * time.Second,
			UserAgent:   clientUA,
		}),
	}
}

// Market is the published survey, and the week it is for.
type Market struct {
	Thirty float64   `json:"thirty_year_pct"`
	Deuce  float64   `json:"fifteen_year_pct"`
	Week   string    `json:"week"`
	Source string    `json:"source"`
	AsOf   time.Time `json:"-"`
	Found  bool      `json:"found"`
}

// Current returns this week's survey. A failure is not an error the caller has to
// handle: the payment is still worth working out at a stated fallback, and the
// report says the rate is an assumption rather than a reading.
func (r *Rates) Current(ctx context.Context) (Market, error) {
	var payload string
	var fetched int64
	err := r.db.QueryRowContext(ctx,
		`SELECT payload, fetched_at FROM lookups WHERE kind = 'pmms' AND key = 'survey'`).
		Scan(&payload, &fetched)
	if err == nil && time.Since(time.Unix(fetched, 0)) < pmmsTTL {
		var m Market
		if json.Unmarshal([]byte(payload), &m) == nil && m.Found {
			return m, nil
		}
	}

	body, err := r.guard.GetRaw(ctx, pmmsURL)
	if err != nil {
		return Market{}, err
	}

	m, err := parsePMMS(string(body))
	if err != nil {
		return Market{}, err
	}

	if _, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('pmms','survey',?,?)`,
		mustJSON(m), time.Now().Unix()); err != nil {
		return m, nil
	}
	return m, nil
}

// parsePMMS reads the last row carrying a 30 year figure. The file runs back to
// 1971 and the recent rows leave the ARM columns empty, so the last line is not
// reliably the last complete one.
func parsePMMS(body string) (Market, error) {
	rd := csv.NewReader(strings.NewReader(body))
	rd.FieldsPerRecord = -1
	rows, err := rd.ReadAll()
	if err != nil {
		return Market{}, fmt.Errorf("read pmms: %w", err)
	}
	if len(rows) < 2 {
		return Market{}, fmt.Errorf("pmms came back with no rows")
	}

	for i := len(rows) - 1; i > 0; i-- {
		row := rows[i]
		if len(row) < 4 {
			continue
		}
		thirty := num(row[1])
		if thirty <= 0 {
			continue
		}
		when, _ := time.Parse("1/2/2006", strings.TrimSpace(row[0]))
		return Market{
			Thirty: thirty,
			Deuce:  num(row[3]),
			Week:   strings.TrimSpace(row[0]),
			Source: "Freddie Mac PMMS",
			AsOf:   when,
			Found:  true,
		}, nil
	}
	return Market{}, fmt.Errorf("pmms carried no 30 year rate")
}

// What each programme goes out at, relative to the conventional survey rate.
// Government backed loans price below conventional because the guarantee carries
// the credit risk, and the borrower pays for that in the insurance instead, which
// is why comparing note rates alone gets the answer wrong.
//
// These are the spreads that hold most weeks. A lender's sheet on the day is the
// real number and the report says so.
var loanSpread = map[string]float64{
	"conventional": 0,
	"fha":          -0.375,
	"va":           -0.375,
	"usda":         -0.25,
}

// creditAdjustment is what Fannie and Freddie's loan level price adjustments cost
// a conventional borrower, expressed as rate rather than as points. FHA, VA and
// USDA do not price this way, so it applies to conventional alone.
func creditAdjustment(score int) float64 {
	switch {
	case score == 0:
		return 0
	case score >= 780:
		return -0.125
	case score >= 740:
		return 0
	case score >= 720:
		return 0.125
	case score >= 700:
		return 0.25
	case score >= 680:
		return 0.5
	case score >= 660:
		return 0.75
	case score >= 640:
		return 1.0
	default:
		return 1.5
	}
}

// noteRate is what the loan itself charges, before any insurance.
func noteRate(base float64, loan string, score int) float64 {
	r := base + loanSpread[loan]
	if loan == "conventional" {
		r += creditAdjustment(score)
	}
	return round2(r)
}
