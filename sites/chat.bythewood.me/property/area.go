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

// What the area around a house is like, which after schools is the thing that
// decides whether a family is happy where they land.
//
// Crime and the settled neighbourhood measures are scored: how many neighbours
// own their homes rather than pass through, how many houses sit empty, and how
// many households have children in them. The published race, ethnicity and
// religion figures are shown and never scored, since a tool that ranks houses
// by who lives nearby is a redlining machine whatever it was built for.
type AreaProfile struct {
	Name   string `json:"name"`   // what this describes, a county or a tract
	Level  string `json:"level"`  // county or tract
	Source string `json:"source"` // who published it, and when

	Population   int     `json:"population"`
	MedianAge    float64 `json:"median_age"`
	MedianIncome float64 `json:"median_income"`

	// The settled neighbourhood measures. Owned rather than rented and lived in
	// rather than empty are what people mean by whether a street is settled.
	OwnerOccupiedPct float64 `json:"owner_occupied_pct"`
	VacancyPct       float64 `json:"vacancy_pct"`
	AvgHouseholdSize float64 `json:"avg_household_size"`

	// What the county's houses sell for, which is the only honest check on
	// whether an asking price is sane for where it is.
	MedianHomeValue float64 `json:"median_home_value"`

	// Reported offences per thousand residents per year, and where it came from.
	// Violent and property are kept apart because they weigh differently to a
	// family and averaging them hides which one a place has.
	ViolentPer1k  float64 `json:"violent_per_1k"`
	PropertyPer1k float64 `json:"property_per_1k"`
	CrimeSource   string  `json:"crime_source"`
	CrimeYear     int     `json:"crime_year"`

	// Published and never scored. Kept as a free-form list so the report can show
	// whatever the source actually published rather than a fixed set of buckets.
	Composition []AreaShare `json:"composition"`
	Religion    []AreaShare `json:"religion"`

	// Serialized, not derived. This was tagged json:"-" and the profile is cached
	// as JSON on the facts row, so it came back false every time and the whole
	// area panel silently never rendered.
	Found bool `json:"found"`
}

type AreaShare struct {
	Label   string  `json:"label"`
	Percent float64 `json:"percent"`
}

// NC state averages for 2023, which is what a county is measured against so a
// number reads as better or worse than around here rather than as a bare figure.
// From the FBI's Crime Data Explorer state tables.
const (
	ncViolentPer1k  = 3.8
	ncPropertyPer1k = 19.4
)

// CrimeRaw scores violent crime heavily and property crime lightly, because they
// are different worries: one is about whether the family is safe and the other is
// about whether the shed gets emptied.
func (a AreaProfile) CrimeRaw() float64 {
	if !a.Found || a.CrimeSource == "" {
		return 0.5
	}
	// Half the state average scores full marks and twice it scores nothing.
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

// CrimeWhyShort is the one line a card has room for.
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

// The census figures that are still published without a key, through the ArcGIS
// services the Census and Esri put the ACS tables on. The Census Bureau's own API
// started refusing keyless requests, and this is the same data by another road.
//
// Four services rather than one, because Esri splits the ACS tables the way the
// Census publishes them and no single layer carries age, income, tenure and
// composition together. Layer 1 is the county layer on all four, and they share
// the NAME and State columns, so one where clause queries all of them.
const acsHost = "https://services.arcgis.com/P3ePLMYs2RVChkJx/arcgis/rest/services/"

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
			Budget:      200,
			Window:      time.Hour,
			Timeout:     40 * time.Second,
		}),
	}
}

// The published shares, in the order the census tabulates them. Kept as a list
// rather than a struct so the report shows what was actually published.
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

// County fills in population and composition for one county, cached by name
// because a five year survey does not move between refreshes.
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
		`SELECT payload FROM lookups WHERE kind = 'acs' AND key = ?`, key).Scan(&payload)
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

	// The Census Bureau's own API started refusing keyless requests, and these
	// layers are the same tables by another road. One where clause, four
	// services, run together since each has its own pace to keep.
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
		`INSERT OR REPLACE INTO lookups (kind, key, payload, fetched_at) VALUES ('acs',?,?,?)`,
		key, string(raw), time.Now().Unix()); err != nil {
		return out, err
	}
	return out, nil
}

// esc is the whole of the SQL escaping an ArcGIS where clause needs here, since
// the only values interpolated are a state and a county name.
func esc(s string) string { return strings.ReplaceAll(s, "'", "''") }
