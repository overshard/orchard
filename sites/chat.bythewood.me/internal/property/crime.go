package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// The FBI's Crime Data Explorer wants a key, free at api.data.gov, since the NC
// SBI only publishes a yearly PDF. There's no county endpoint, so a county is the
// sum of the agencies that police it.
const (
	cdeAgencies   = "https://api.usa.gov/crime/fbi/cde/agency/byStateAbbr/NC"
	cdeSummarized = "https://api.usa.gov/crime/fbi/cde/summarized/agency"
)

type Crime struct {
	db    *sql.DB
	guard *Guard
	key   string
}

func NewCrime(db *sql.DB) *Crime {
	return &Crime{
		db: db,
		guard: NewGuard(db, "fbi-cde", GuardOpts{
			MinInterval: 2 * time.Second,
			// Two calls per agency and a county is cached for months, so this is
			// generous for a first run and idle after.
			Budget:    150,
			Window:    24 * time.Hour,
			Timeout:   45 * time.Second,
			UserAgent: clientUA,
		}),
		key: os.Getenv("FBI_API_KEY"),
	}
}

func (c *Crime) Configured() bool { return c.key != "" }

func (c *Crime) withKey(u string) string {
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	return u + sep + "API_KEY=" + url.QueryEscape(c.key)
}

type cdeAgency struct {
	ORI  string `json:"ori"`
	Name string `json:"agency_name"`
	Type string `json:"agency_type_name"`
}

// agencies reads one response that covers the whole state, keyed by upper case
// county name, so it's fetched once and kept.
func (c *Crime) agencies(ctx context.Context, county string) ([]cdeAgency, error) {
	var payload string
	var fetched int64
	err := c.db.QueryRowContext(ctx,
		`SELECT payload, fetched_at FROM lookups WHERE kind = 'cde-agencies' AND key = 'NC'`).
		Scan(&payload, &fetched)

	byCounty := map[string][]cdeAgency{}
	fresh := err == nil && time.Since(time.Unix(fetched, 0)) < 90*24*time.Hour
	if fresh {
		if json.Unmarshal([]byte(payload), &byCounty) != nil {
			fresh = false
		}
	} else if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	if !fresh {
		if err := c.guard.Get(ctx, c.withKey(cdeAgencies), &byCounty); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(byCounty)
		if err != nil {
			return nil, err
		}
		if _, err := c.db.ExecContext(ctx,
			`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('cde-agencies','NC',?,?)`,
			string(raw), time.Now().Unix()); err != nil {
			return nil, err
		}
	}

	want := strings.ToUpper(strings.TrimSpace(strings.TrimSuffix(
		strings.TrimSpace(county), " County")))
	return byCounty[want], nil
}

type cdeSummary struct {
	Offenses struct {
		Actuals map[string]map[string]float64 `json:"actuals"`
	} `json:"offenses"`
	// Population, then entity, then month. The entities include the nation and
	// the state, so the agency has to be picked out by name.
	Populations struct {
		Population map[string]map[string]float64 `json:"population"`
	} `json:"populations"`
}

func (c *Crime) County(ctx context.Context, county string, _ int) (AreaProfile, error) {
	var out AreaProfile
	if !c.Configured() || strings.TrimSpace(county) == "" {
		return out, nil
	}

	key := strings.ToLower(strings.TrimSpace(county))
	var payload string
	var fetched int64
	err := c.db.QueryRowContext(ctx,
		`SELECT payload, fetched_at FROM lookups WHERE kind = 'crime' AND key = ?`, key).
		Scan(&payload, &fetched)
	if err == nil && time.Since(time.Unix(fetched, 0)) < 180*24*time.Hour {
		if json.Unmarshal([]byte(payload), &out) == nil {
			return out, nil
		}
	} else if err != nil && err != sql.ErrNoRows {
		return out, err
	}

	list, err := c.agencies(ctx, county)
	if err != nil {
		return out, err
	}
	if len(list) == 0 {
		return out, nil
	}

	// The current year is always partial and would read as a town that had
	// suddenly become very safe.
	year := time.Now().Year() - 1
	window := fmt.Sprintf("from=01-%d&to=12-%d", year, year)

	var violent, property, covered float64
	for _, a := range list {
		for _, offence := range []string{"violent-crime", "property-crime"} {
			u := fmt.Sprintf("%s/%s/%s?%s", cdeSummarized, url.PathEscape(a.ORI), offence, window)

			var res cdeSummary
			if err := c.guard.Get(ctx, c.withKey(u), &res); err != nil {
				continue
			}

			total := sumSeries(res.Offenses.Actuals, a.Name+" Offenses")
			switch offence {
			case "violent-crime":
				violent += total
				// Both offences report the same population, so it's counted once.
				covered += peakPopulation(res.Populations.Population, a.Name)
			case "property-crime":
				property += total
			}
		}
	}

	if covered <= 0 || (violent == 0 && property == 0) {
		return out, nil
	}

	out.Found = true
	out.CrimeYear = year
	out.CrimeSource = fmt.Sprintf("FBI Crime Data Explorer, %d, %d agencies", year, len(list))
	out.ViolentPer1k = violent / covered * 1000
	out.PropertyPer1k = property / covered * 1000

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := c.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('crime',?,?,?)`,
		key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

// sumSeries matches the exact agency name since the response also holds national
// and state series that mustn't be added in.
func sumSeries(actuals map[string]map[string]float64, want string) float64 {
	var total float64
	for name, months := range actuals {
		if !strings.EqualFold(name, want) {
			continue
		}
		for _, v := range months {
			total += v
		}
	}
	return total
}

// peakPopulation takes the largest monthly figure, since an agency that missed a
// month reports zero and an average would inflate the rate.
func peakPopulation(pops map[string]map[string]float64, agency string) float64 {
	var best float64
	for name, months := range pops {
		if !strings.EqualFold(name, agency) {
			continue
		}
		for _, v := range months {
			if v > best {
				best = v
			}
		}
	}
	return best
}
