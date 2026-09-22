package property

import (
	"encoding/json"
	"math"
	"os"
	"strings"
)

// Everything personal lives in data/property.json, which is gitignored along with
// the rest of the data directory. This repository is public, so the committed
// half is property.example.json and carries placeholders: an address, a budget
// and a child's school run are not facts to publish.
//
// A missing config is not fatal. The engine runs on these defaults and every
// report says so, because a tool that refuses to answer is harder to fix than one
// that tells you what it is missing.

// Person is somebody in the household, so a question that names one of them
// reaches this tool rather than being read as a subject to look up. house used
// pseudonyms because its pages rendered on a phone somebody could glance at, and
// this is a private conversation, so the real names belong here.
type Person struct {
	Name string `json:"name"`
	Role string `json:"role"` // what they do, which is what "work nearby" means for them
	Note string `json:"note,omitempty"`
}

// Place is somewhere a drive is measured to, every time, for whoever drives it.
type Place struct {
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Kind  string  `json:"kind"` // work, school, errand
	Who   string  `json:"who"`  // whose drive this is
	Addr  string  `json:"addr"`
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
	Daily bool    `json:"daily"`
}

// Geography is the one point everything else is measured from, which is the
// office, plus the drive time band a house has to land inside to be worth talking
// about.
type Geography struct {
	OriginName string  `json:"origin_name"`
	OriginAddr string  `json:"origin_addr"`
	OriginLat  float64 `json:"origin_lat"`
	OriginLon  float64 `json:"origin_lon"`

	IdealMinutes float64 `json:"ideal_minutes"`
	MaxMinutes   float64 `json:"max_minutes"`

	Counties []string `json:"counties"`
}

// Thresholds are what a lookup needs to sort a measurement into good or bad. They
// are here rather than in the code because which distance is close enough moves
// as a person sees houses.
type Thresholds struct {
	FloodZones []string `json:"flood_zones"`

	// One buffer per kind of water. NHD maps every wet-weather ditch in these
	// counties and nearly every rural parcel has one inside 300ft, so buffering
	// all three alike flags most of the county without finding any flood risk.
	WaterBufferFeet        float64 `json:"water_buffer_feet"`
	IntermittentBufferFeet float64 `json:"intermittent_buffer_feet"`
	DitchBufferFeet        float64 `json:"ditch_buffer_feet"`

	AADTCutoff   int      `json:"aadt_cutoff"`
	AADTRadiusFt float64  `json:"aadt_radius_ft"`
	RoadClasses  []string `json:"road_classes"`

	CornerFeet          float64 `json:"corner_feet"`
	HighwayTooCloseFeet float64 `json:"highway_too_close_feet"`
	HighwayComfyFeet    float64 `json:"highway_comfy_feet"`
}

// Money is everything the payment needs that is not the price or the loan. The
// loan terms themselves are in loans.go, since they are published figures rather
// than preferences.
type Money struct {
	DownPaymentPercent float64 `json:"down_payment_percent"`
	TermYears          int     `json:"term_years"`
	CreditScore        int     `json:"credit_score"`
	InsuranceAnnual    float64 `json:"insurance_annual"`
	UtilitiesMonthly   float64 `json:"utilities_monthly"`
	InternetMonthly    float64 `json:"internet_monthly"`
	MonthlyTarget      float64 `json:"monthly_target"`
	MonthlyCeiling     float64 `json:"monthly_ceiling"`
	DPAAmount          float64 `json:"dpa_amount"`

	// Household income and monthly debts, which USDA and every debt to income
	// answer needs. Left at zero the loan comparison still works and the
	// eligibility answers say what they could not check.
	AnnualIncome    float64            `json:"annual_income"`
	HouseholdSize   int                `json:"household_size"`
	MonthlyDebts    float64            `json:"monthly_debts"`
	VAEligible      bool               `json:"va_eligible"`
	VAFirstUse      bool               `json:"va_first_use"`
	VAExemptFundFee bool               `json:"va_exempt_funding_fee"`
	FirstTimeBuyer  bool               `json:"first_time_buyer"`
	CountyTaxPer100 map[string]float64 `json:"county_tax_per_100"`
	RevaluedYear    map[string]int     `json:"revalued_year"`

	// USDA's guaranteed programme income cap per county, for a household of one
	// to four. It is config because USDA publishes it as a PDF map and it moves
	// every year, and an unconfigured county says it could not check rather than
	// guessing.
	USDAIncomeLimit map[string]float64 `json:"usda_income_limit"`
}

func (m Money) usdaLimits() map[string]float64 { return m.USDAIncomeLimit }

// Household is the family the drives are measured for.
type Household struct {
	DropoffGreatMin float64 `json:"dropoff_great_min"`
	DropoffOkMin    float64 `json:"dropoff_ok_min"`
	DropoffBadMin   float64 `json:"dropoff_bad_min"`
	LotIdealAcres   float64 `json:"lot_ideal_acres"`
}

type Config struct {
	Label        string     `json:"label"`
	Geography    Geography  `json:"geography"`
	Thresholds   Thresholds `json:"thresholds"`
	Money        Money      `json:"money"`
	Household    Household  `json:"household"`
	People       []Person   `json:"people"`
	Destinations []Place    `json:"destinations"`

	// Set when data/property.json was not there, so a report can say the numbers
	// in it are defaults and not his.
	Defaulted bool `json:"-"`
}

func (c Config) HasOrigin() bool { return c.Geography.OriginLat != 0 || c.Geography.OriginLon != 0 }

// RevaluationYear is the year a county's last general reappraisal took effect,
// which is what an assessed value is as of. NC counties run four to eight years
// between them, so a wrong year here is a whole cycle of drift.
func (m Money) RevaluationYear(county string) int {
	key := countyKey(county)
	if y, ok := m.RevaluedYear[key]; ok {
		return y
	}
	return 0
}

// rateFor is the combined county plus municipal rate per $100 of assessed value.
// Neighbouring counties differ enough to move a payment by $40 a month on the
// same price, so a single average rate would be wrong in both directions at once.
func (m Money) rateFor(county, city string) (float64, string) {
	key := countyKey(county)

	// A city rate is looked up first, as "county:city", since a house inside town
	// limits pays both.
	if city != "" {
		ck := key + ":" + strings.ToLower(strings.TrimSpace(city))
		if r, ok := m.CountyTaxPer100[ck]; ok {
			return r, county + " plus " + city
		}
	}
	if r, ok := m.CountyTaxPer100[key]; ok {
		return r, county + " county rate"
	}
	if r, ok := m.CountyTaxPer100["default"]; ok {
		if county == "" {
			return r, "default rate, county unknown"
		}
		return r, "default rate, no entry for " + county
	}
	return 0, "no tax rate configured"
}

func defaultConfig() Config {
	return Config{
		Label: "defaults, no data/property.json",
		Geography: Geography{
			OriginName: "Work",
			// Left empty, since a fake origin would compute a page of drive times
			// to the wrong place and look like it worked.
			IdealMinutes: 25,
			MaxMinutes:   45,
		},
		Thresholds: Thresholds{
			FloodZones:             []string{"A", "AE", "AO", "AH", "AR", "A99", "V", "VE"},
			WaterBufferFeet:        300,
			IntermittentBufferFeet: 150,
			DitchBufferFeet:        0,
			AADTCutoff:             4000,
			AADTRadiusFt:           250,
			RoadClasses:            []string{"motorway", "trunk", "primary", "secondary"},
			CornerFeet:             150,
			HighwayTooCloseFeet:    2000,
			HighwayComfyFeet:       5280,
		},
		Money: Money{
			DownPaymentPercent: 3,
			TermYears:          30,
			CreditScore:        740,
			InsuranceAnnual:    1500,
			UtilitiesMonthly:   300,
			InternetMonthly:    70,
			HouseholdSize:      3,
			CountyTaxPer100:    map[string]float64{"default": 0.7},
			RevaluedYear:       map[string]int{},
		},
		Household: Household{
			DropoffGreatMin: 8,
			DropoffOkMin:    15,
			DropoffBadMin:   20,
			LotIdealAcres:   1,
		},
		Defaulted: true,
	}
}

// LoadConfig reads data/property.json. A missing file is not an error, and a
// malformed one is, because silently running on defaults when a file exists hides
// a typo behind numbers that look plausible.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultConfig(), nil
	}
	if err != nil {
		return defaultConfig(), err
	}

	cfg := defaultConfig()
	cfg.Defaulted = false
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return defaultConfig(), err
	}
	if cfg.Money.CountyTaxPer100 == nil {
		cfg.Money.CountyTaxPer100 = map[string]float64{"default": 0.7}
	}
	if cfg.Money.RevaluedYear == nil {
		cfg.Money.RevaluedYear = map[string]int{}
	}
	if cfg.Money.TermYears == 0 {
		cfg.Money.TermYears = 30
	}
	if cfg.Money.CreditScore == 0 {
		cfg.Money.CreditScore = 740
	}
	if math.IsNaN(cfg.Money.InsuranceAnnual) || cfg.Money.InsuranceAnnual <= 0 {
		cfg.Money.InsuranceAnnual = 1500
	}
	return cfg, nil
}
