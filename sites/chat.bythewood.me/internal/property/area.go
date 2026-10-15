package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AreaProfile is the county around a house. Race, ethnicity and religion are
// shown and never scored, since a tool that ranks houses by who lives nearby is
// a redlining machine whatever it was built for.
type AreaProfile struct {
	Name   string `json:"name"`   // what this describes, a county or a tract
	Level  string `json:"level"`  // county or tract
	Source string `json:"source"` // who published it, and when

	Population   int     `json:"population"`
	MedianAge    float64 `json:"median_age"`
	MedianIncome float64 `json:"median_income"`

	OwnerOccupiedPct float64 `json:"owner_occupied_pct"`
	VacancyPct       float64 `json:"vacancy_pct"`
	AvgHouseholdSize float64 `json:"avg_household_size"`

	// The county median is the only check on whether an asking price is sane
	// for where it is.
	MedianHomeValue float64 `json:"median_home_value"`

	// Offences per thousand residents a year. Violent and property weigh
	// differently to a family and averaging them hides which one a place has.
	ViolentPer1k  float64 `json:"violent_per_1k"`
	PropertyPer1k float64 `json:"property_per_1k"`
	CrimeSource   string  `json:"crime_source"`
	CrimeYear     int     `json:"crime_year"`

	// Published and never scored, as a list so the report shows whatever the
	// source published instead of a fixed set of buckets.
	Composition []AreaShare `json:"composition"`
	Religion    []AreaShare `json:"religion"`

	// Serialized since the profile is cached as JSON, and a json:"-" tag would
	// read back false every time.
	Found bool `json:"found"`
}

type AreaShare struct {
	Label   string  `json:"label"`
	Percent float64 `json:"percent"`
}

// NC state averages for 2023 from the FBI's Crime Data Explorer, so a county
// reads as better or worse than around here instead of as a bare figure.
const (
	ncViolentPer1k  = 3.8
	ncPropertyPer1k = 19.4
)

// CrimeRaw weighs violent crime heavily and property crime lightly, since one is
// whether the family is safe and the other is whether the shed gets emptied.
func (a AreaProfile) CrimeRaw() float64 {
	if !a.Found || a.CrimeSource == "" {
		return 0.5
	}
	// 40% of the state average scores full marks and twice it scores nothing.
	violent := band(a.ViolentPer1k, ncViolentPer1k*0.4, ncViolentPer1k*2)
	property := band(a.PropertyPer1k, ncPropertyPer1k*0.4, ncPropertyPer1k*2)
	return violent*0.7 + property*0.3
}

func (a AreaProfile) CrimeWhy() string {
	if !a.Found || a.CrimeSource == "" {
		return "no crime figures loaded for this area yet"
	}
	return fmt.Sprintf("%.1f violent and %.0f property offences per 1,000 a year in %s, against %.1f and %.0f statewide",
		a.ViolentPer1k, a.PropertyPer1k, a.Name, ncViolentPer1k, ncPropertyPer1k)
}

func (a AreaProfile) CrimeWhyShort() string {
	if !a.Found || a.CrimeSource == "" {
		return "crime figures not loaded"
	}
	switch r := a.CrimeRaw(); {
	case r >= 0.7:
		return "quieter than most of the state"
	case r >= 0.45:
		return "about average for the state"
	default:
		return "busier than most of the state"
	}
}

// The Census Bureau's own API refuses keyless requests, so the ACS tables come
// from Esri's layers, where no single one carries age, income, tenure and
// composition. Layer 1 is the county on all four, so one where clause fits all.
const acsHost = "https://services.arcgis.com/P3ePLMYs2RVChkJx/arcgis/rest/services/"

// Bump this whenever AreaProfile gains a field, since an older row decodes
// with zeroes that read as claims about the county.
const acsCacheKind = "acs2"

var acsLayers = []struct {
	service string
	fields  []string
}{
	{"ACS_Population_by_Race_and_Hispanic_Origin_Boundaries", nil}, // filled from acsShares below
	{"ACS_Median_Age_View_Boundaries", []string{"B01002_001E"}},
	{"ACS_Median_Household_Income_View_Boundaries", []string{"B19049_001E"}},
	{"ACS_Housing_Units_Occupancy_View_Boundaries", []string{
		"B25003_calc_pctOwnE", "B25002_calc_pctVacE", "B25077_001E"}},
}

type Census struct {
	db    *sql.DB
	guard *Guard
}

func NewCensus(db *sql.DB) *Census {
	return &Census{
		db: db,
		guard: NewGuard(db, "acs-census", GuardOpts{
			MinInterval: 2 * time.Second,
			Budget:      60,
			Window:      time.Hour,
			Timeout:     40 * time.Second,
		}),
	}
}

// In the order the census tabulates them.
var acsShares = []struct{ field, label string }{
	{"B03002_calc_pctNHWhiteE", "White"},
	{"B03002_calc_pctBlackE", "Black"},
	{"B03002_calc_pctHispLatE", "Hispanic or Latino"},
	{"B03002_calc_pctAsianE", "Asian"},
	{"B03002_calc_pct2OrMoreE", "Two or more races"},
	{"B03002_calc_pctAIANE", "American Indian or Alaska Native"},
	{"B03002_calc_pctNHOPIE", "Native Hawaiian or Pacific Islander"},
	{"B03002_calc_pctOtherE", "Other"},
}

// County is cached by name because a five year survey doesn't move between
// refreshes.
func (c *Census) County(ctx context.Context, state, county string) (AreaProfile, error) {
	name := strings.TrimSpace(county)
	if name == "" {
		return AreaProfile{}, fmt.Errorf("no county")
	}
	if !strings.HasSuffix(strings.ToLower(name), " county") {
		name += " County"
	}
	key := strings.ToLower(state + "|" + name)

	var payload string
	err := c.db.QueryRowContext(ctx,
		`SELECT payload FROM lookups WHERE kind = ? AND key = ?`, acsCacheKind, key).Scan(&payload)
	if err == nil {
		var cached AreaProfile
		if json.Unmarshal([]byte(payload), &cached) == nil {
			cached.Found = true
			return cached, nil
		}
	} else if err != sql.ErrNoRows {
		return AreaProfile{}, err
	}

	out := AreaProfile{
		Name:   name,
		Level:  "county",
		Source: "US Census ACS 5 year, through the Esri Living Atlas layers",
		Found:  true,
	}

	where := fmt.Sprintf("State='%s' AND NAME='%s'", esc(state), esc(name))
	attrs := make([]map[string]any, len(acsLayers))
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex
	for i, layer := range acsLayers {
		fields := append([]string{"NAME"}, layer.fields...)
		if layer.fields == nil {
			fields = append(fields, "B03002_001E")
			for _, sh := range acsShares {
				fields = append(fields, sh.field)
			}
		}
		wg.Add(1)
		go func(i int, service string, fields []string) {
			defer wg.Done()
			v := url.Values{
				"where":          {where},
				"outFields":      {strings.Join(fields, ",")},
				"returnGeometry": {"false"},
				"f":              {"json"},
			}
			var res esriQueryResult
			if err := c.guard.Get(ctx, acsHost+service+"/FeatureServer/1/query?"+v.Encode(), &res); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			if len(res.Features) == 0 {
				return
			}
			mu.Lock()
			attrs[i] = res.Features[0].Attributes
			mu.Unlock()
		}(i, layer.service, fields)
	}
	wg.Wait()

	if attrs[0] == nil && firstErr != nil {
		return AreaProfile{}, firstErr
	}

	if a := attrs[0]; a != nil {
		if n := attrString(a, "NAME"); n != "" {
			out.Name = n
		}
		if pop, ok := attrFloat(a, "B03002_001E"); ok {
			out.Population = int(pop)
		}
		for _, sh := range acsShares {
			if pct, ok := attrFloat(a, sh.field); ok && pct > 0 {
				out.Composition = append(out.Composition, AreaShare{Label: sh.label, Percent: round1(pct)})
			}
		}
	}
	if a := attrs[1]; a != nil {
		out.MedianAge, _ = attrFloat(a, "B01002_001E")
	}
	if a := attrs[2]; a != nil {
		out.MedianIncome, _ = attrFloat(a, "B19049_001E")
	}
	if a := attrs[3]; a != nil {
		out.OwnerOccupiedPct, _ = attrFloat(a, "B25003_calc_pctOwnE")
		out.VacancyPct, _ = attrFloat(a, "B25002_calc_pctVacE")
		out.MedianHomeValue, _ = attrFloat(a, "B25077_001E")
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	if _, err := c.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES (?,?,?,?)`,
		acsCacheKind, key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

// esc is the whole of the SQL escaping an ArcGIS where clause needs here, since
// the only values interpolated are a state and a county name.
func esc(s string) string { return strings.ReplaceAll(s, "'", "''") }
