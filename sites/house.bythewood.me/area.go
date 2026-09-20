package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
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

	// The settled neighbourhood measures, all percentages.
	OwnerOccupiedPct float64 `json:"owner_occupied_pct"`
	VacancyPct       float64 `json:"vacancy_pct"`
	WithChildrenPct  float64 `json:"with_children_pct"`

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

// CommunityRaw is three measures of whether a place is settled: owned rather than
// rented, lived in rather than empty, and with children in it rather than without.
func (a AreaProfile) CommunityRaw() float64 {
	if !a.Found || a.OwnerOccupiedPct == 0 {
		return 0.5
	}
	owner := band(a.OwnerOccupiedPct, 85, 45)
	// Some vacancy is normal and a lot of it is a street with boarded houses on it.
	empty := band(a.VacancyPct, 4, 20)
	kids := band(a.WithChildrenPct, 40, 12)
	return owner*0.45 + empty*0.3 + kids*0.25
}

func (a AreaProfile) CommunityWhy() string {
	if !a.Found || a.OwnerOccupiedPct == 0 {
		return "no neighbourhood figures loaded for this area yet"
	}
	return fmt.Sprintf("%.0f%% own their homes, %.0f%% of homes are empty, %.0f%% of households have children",
		a.OwnerOccupiedPct, a.VacancyPct, a.WithChildrenPct)
}

// AreaTable is the whole file, keyed by lowercase county name. County level is
// what a keyless build gets, because the Census stopped answering without a key
// and the figures that are still freely published are county wide.
type AreaTable map[string]AreaProfile

// LoadAreaTable reads data/area.json. A missing file is not an error: the two
// factors that read it say they have no figures rather than inventing one, which
// is the whole reason they are separate factors with their own wording.
func LoadAreaTable(path string) (AreaTable, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return AreaTable{}, nil
	}
	if err != nil {
		return nil, err
	}
	var t AreaTable
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("parse area table: %w", err)
	}
	out := make(AreaTable, len(t))
	for k, v := range t {
		v.Found = true
		if v.Level == "" {
			v.Level = "county"
		}
		out[countyKey(k)] = v
	}
	return out, nil
}

func (t AreaTable) For(county string) AreaProfile {
	if p, ok := t[countyKey(county)]; ok {
		return p
	}
	return AreaProfile{}
}

// The census figures that are still published without a key, through the ArcGIS
// services the Census and Esri put the ACS tables on. The Census Bureau's own API
// started refusing keyless requests, and this is the same data by another road.
//
// Only population and composition come from here. Crime and the settled
// neighbourhood measures are not on this service and come out of data/area.json,
// which is why they say so plainly when it is empty.
const acsRaceCounties = "https://services.arcgis.com/P3ePLMYs2RVChkJx/arcgis/rest/services/" +
	"ACS_Population_by_Race_and_Hispanic_Origin_Boundaries/FeatureServer/1/query"

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

	fields := []string{"NAME", "B03002_001E"}
	for _, s := range acsShares {
		fields = append(fields, s.field)
	}

	// The county layer names its column NAME and keeps the state in State. Getting
	// either wrong comes back as "outFields parameter is invalid" rather than as
	// an empty result, which is at least loud.
	v := url.Values{
		"where":          {fmt.Sprintf("State='%s' AND NAME='%s'", esc(state), esc(name))},
		"outFields":      {strings.Join(fields, ",")},
		"returnGeometry": {"false"},
		"f":              {"json"},
	}

	var res esriQueryResult
	if err := c.guard.Get(ctx, acsRaceCounties+"?"+v.Encode(), &res); err != nil {
		return AreaProfile{}, err
	}
	if len(res.Features) == 0 {
		return AreaProfile{}, nil
	}

	attrs := res.Features[0].Attributes
	out := AreaProfile{
		Name:   attrString(attrs, "NAME"),
		Level:  "county",
		Source: "US Census ACS 5 year",
		Found:  true,
	}
	if pop, ok := attrFloat(attrs, "B03002_001E"); ok {
		out.Population = int(pop)
	}
	for _, s := range acsShares {
		if pct, ok := attrFloat(attrs, s.field); ok && pct > 0 {
			out.Composition = append(out.Composition, AreaShare{Label: s.label, Percent: pct})
		}
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

// Merge lays the hand-entered table over the fetched census figures. The table
// wins where it has a value, since it is the only source for the scored measures.
func (a AreaProfile) Merge(table AreaProfile) AreaProfile {
	if !table.Found {
		return a
	}
	out := a
	out.Found = true
	if table.Name != "" {
		out.Name = table.Name
	}
	if table.MedianAge > 0 {
		out.MedianAge = table.MedianAge
	}
	if table.MedianIncome > 0 {
		out.MedianIncome = table.MedianIncome
	}
	if table.OwnerOccupiedPct > 0 {
		out.OwnerOccupiedPct = table.OwnerOccupiedPct
	}
	if table.VacancyPct > 0 {
		out.VacancyPct = table.VacancyPct
	}
	if table.WithChildrenPct > 0 {
		out.WithChildrenPct = table.WithChildrenPct
	}
	if table.CrimeSource != "" {
		out.ViolentPer1k = table.ViolentPer1k
		out.PropertyPer1k = table.PropertyPer1k
		out.CrimeSource = table.CrimeSource
		out.CrimeYear = table.CrimeYear
	}
	if len(table.Religion) > 0 {
		out.Religion = table.Religion
	}
	if len(table.Composition) > 0 && len(out.Composition) == 0 {
		out.Composition = table.Composition
	}
	return out
}

// Rounded is what the report prints, so a share of a percent does not render as
// fourteen decimal places.
func (s AreaShare) Rounded() float64 { return math.Round(s.Percent*10) / 10 }

// Title cases a label for display without touching an acronym.
func (s AreaShare) Title() string {
	if s.Label == strings.ToUpper(s.Label) {
		return s.Label
	}
	return s.Label
}
