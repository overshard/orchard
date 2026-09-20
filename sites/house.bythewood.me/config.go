package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
)

// Everything personal lives in data/config.json, which is gitignored along with
// the rest of the data directory. This repository is public, so the committed
// half is config.example.json and carries placeholders: an address, a budget and
// a child's school run are not facts to publish.
//
// A missing config is not fatal. The site starts on these defaults and says so
// on every page, because a dashboard that refuses to boot is harder to fix than
// one that tells you what it is missing.

// Place is somewhere a drive is measured to.
type Place struct {
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Kind  string  `json:"kind"` // work, school, employer, errand
	Who   string  `json:"who"`  // whose drive this is
	Addr  string  `json:"addr"`
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
	Daily bool    `json:"daily"` // driven most days, which weights the commute score
}

type Geography struct {
	// The office. Every drive-time band is measured from here.
	OriginName string  `json:"origin_name"`
	OriginAddr string  `json:"origin_addr"`
	OriginLat  float64 `json:"origin_lat"`
	OriginLon  float64 `json:"origin_lon"`

	// The search envelope is a drive-time band rather than a lat/lon box,
	// because rural roads do not run straight and a box includes places you
	// cannot get to in the time.
	IdealMinutes   float64 `json:"ideal_minutes"`
	MaxMinutes     float64 `json:"max_minutes"`
	StretchMinutes float64 `json:"stretch_minutes"`

	// The stated north bound. A listing above this latitude is out whatever its
	// drive time says.
	NorthLatCap float64 `json:"north_lat_cap"`

	// The arc of the compass to search, in degrees clockwise from true north as
	// seen from the office. South through west is roughly 150 to 310, which is
	// what answers the east bound question: anything east of 150 is out.
	BearingFrom float64 `json:"bearing_from"`
	BearingTo   float64 `json:"bearing_to"`

	Counties []string `json:"counties"`
}

type Filters struct {
	MaxPrice       int `json:"max_price"`
	PreferredPrice int `json:"preferred_price"`
	MinPrice       int `json:"min_price"`

	// FEMA zone letters that are a Special Flood Hazard Area.
	FloodZones []string `json:"flood_zones"`

	// One buffer per kind of water, because they are not the same risk. NHD maps
	// every wet-weather ditch in these counties and nearly every rural parcel has
	// one inside 300ft, so buffering all three the same excludes most of the
	// county without finding any flood risk. A zero means that kind never
	// excludes and only moves the score.
	WaterBufferFeet        float64 `json:"water_buffer_feet"`
	IntermittentBufferFeet float64 `json:"intermittent_buffer_feet"`
	DitchBufferFeet        float64 `json:"ditch_buffer_feet"`

	// Annual average daily traffic on the nearest counted segment. NCDOT
	// publishes this per segment, so it is a measured number rather than a
	// judgement about whether a road looks busy.
	AADTCutoff   int      `json:"aadt_cutoff"`
	AADTRadiusFt float64  `json:"aadt_radius_ft"`
	RoadClasses  []string `json:"road_classes"`

	// An old house with good bones is fine and an old manufactured one is not, so
	// the test is on construction and age together rather than on age alone.
	ManufacturedStyles  []string `json:"manufactured_styles"`
	ManufacturedMinYear int      `json:"manufactured_min_year"`

	// How close a second road has to be to count as road on another side of the
	// house, which is the corner lot.
	CornerFeet float64 `json:"corner_feet"`

	// Nobody wants an interstate at the bottom of the garden, and everybody wants
	// to be able to reach one. Under the first number is close enough to hear it,
	// past the second is far enough not to think about it, and being able to get
	// on it barely registers next to either.
	HighwayTooCloseFeet float64 `json:"highway_too_close_feet"`
	HighwayComfyFeet    float64 `json:"highway_comfy_feet"`

	// Per rule, "exclude" or "penalty", and the points a penalty costs. Which of
	// these is a dealbreaker is the reader's call and it moves as they see houses,
	// so it is config rather than a switch in the code.
	Severity  map[string]string  `json:"severity"`
	Penalties map[string]float64 `json:"penalties"`
}

type Money struct {
	DownPaymentPercent float64 `json:"down_payment_percent"`
	RatePercent        float64 `json:"rate_percent"`
	TermYears          int     `json:"term_years"`
	PMIAnnualPercent   float64 `json:"pmi_annual_percent"`
	InsuranceAnnual    float64 `json:"insurance_annual"`
	UtilitiesMonthly   float64 `json:"utilities_monthly"`
	InternetMonthly    float64 `json:"internet_monthly"`
	// The year each county's last general reappraisal took effect, which is what
	// an assessed value is as of. NC counties run four to eight years between
	// them, so a wrong year here is a whole cycle of drift and the report prints
	// the year it used so it can be checked.
	RevaluedYear map[string]int `json:"revalued_year"`

	MonthlyTarget   float64            `json:"monthly_target"`
	MonthlyCeiling  float64            `json:"monthly_ceiling"`
	DPAAmount       float64            `json:"dpa_amount"`
	CountyTaxPer100 map[string]float64 `json:"county_tax_per_100"`
}

type Household struct {
	// Before and after school care, as published by the county programme. The
	// gap that matters is that after-school care is elementary only, so the
	// middle school years have nothing.
	BeforeCareOpens string  `json:"before_care_opens"`
	AfterCareCloses string  `json:"after_care_closes"`
	AfterCareGrades string  `json:"after_care_grades"`
	DropoffGreatMin float64 `json:"dropoff_great_min"`
	DropoffOkMin    float64 `json:"dropoff_ok_min"`
	DropoffBadMin   float64 `json:"dropoff_bad_min"`
	LotIdealAcres   float64 `json:"lot_ideal_acres"`
}

type Config struct {
	Label        string             `json:"label"`
	Geography    Geography          `json:"geography"`
	Filters      Filters            `json:"filters"`
	Money        Money              `json:"money"`
	Household    Household          `json:"household"`
	Destinations []Place            `json:"destinations"`
	Weights      map[string]float64 `json:"weights"`

	// Set when data/config.json was not there, so every page can say the
	// numbers on it are defaults and not his.
	Defaulted bool `json:"-"`
}

// The scored factors, in the order they render, which is the order they matter in:
// the child first, then safety, then the partner's work and study, then the money
// the whole household lives inside, and the commute last.
//
// That order is chosen rather than arbitrary. Moving is for a family that is
// happy and safe, so what serves them outranks what is convenient for the
// driver, and the two factors that are purely about his time sit at the bottom.
var defaultWeights = map[string]float64{
	"schools":      18,
	"outings":      8,
	"crime":        14,
	"flood":        10,
	"quiet":        10,
	"community":    8,
	"elbow":        6,
	"highway":      6,
	"partnerwork":  10,
	"partnerstudy": 8,
	"monthly":      14,
	"land":         8,
	"dropoff":      6,
	"commute":      4,
}

var factorOrder = []string{
	"schools", "outings", "crime", "flood", "quiet", "community", "elbow", "highway",
	"partnerwork", "partnerstudy", "land", "monthly", "dropoff", "commute",
}

var factorLabels = map[string]string{
	"schools":      "Schools, K-12",
	"outings":      "Places to go",
	"crime":        "Crime and safety",
	"flood":        "Flood margin",
	"quiet":        "Quiet, safe road",
	"community":    "Settled neighbourhood",
	"elbow":        "Elbow room",
	"partnerwork":  "Nursing work nearby",
	"partnerstudy": "Nursing school nearby",
	"monthly":      "All-in monthly",
	"land":         "Land, garden and animals",
	"dropoff":      "Drop-off detour",
	"commute":      "Commute",
	"highway":      "Away from the interstate",
}

// Which of the household each factor is for, so the report can group them and say
// whose life it is describing rather than presenting thirteen equal rows.
// Land sits with the partner rather than the child: the garden, the beds and the
// chickens are hers, and what a ten year old wants is somewhere to be taken on a
// Saturday.
var factorFor = map[string]string{
	"schools": "child", "outings": "child",
	"crime": "safety", "flood": "safety", "quiet": "safety", "community": "safety",
	"highway":     "safety",
	"partnerwork": "partner", "partnerstudy": "partner", "land": "partner",
	"elbow":   "household",
	"monthly": "driver", "dropoff": "driver", "commute": "driver",
}

// Nobody is named anywhere in this app. Not in the repository, not in the
// database, and not on a page that might be open on a phone on a kitchen counter
// with somebody else in the room. It is a den of bears, which reads rather better
// than a row of initials and is the sort of thing you do not mind somebody
// glancing at over your shoulder.
var forLabels = map[string]string{
	"child":     "For Little Bear",
	"safety":    "Safe and sound",
	"partner":   "For Mama Bear",
	"household": "For the whole den",
	"driver":    "For Papa Bear",
}

var forOrder = []string{"child", "partner", "driver", "household", "safety"}

// A line under each heading saying why that band is on the page. The bands are
// not really separate: a house that suits the family is what makes the driver
// happy, and the mortgage is his because he is the one paying it.
var forNotes = map[string]string{
	"child":     "a decent school, and somewhere to go at the weekend",
	"partner":   "work on her shift, the LPN and the RN within reach, and room for a garden and animals",
	"driver":    "the drive and the money, which is his because he pays it",
	"household": "room to breathe, for all of us",
	"safety":    "the part none of the rest is worth anything without",
}

func defaultConfig() Config {
	return Config{
		Label: "defaults, no data/config.json",
		Geography: Geography{
			OriginName: "Office",
			// Left empty, since a fake origin would compute a whole dashboard
			// of drive times to the wrong place and look like it worked.
			IdealMinutes:   25,
			MaxMinutes:     45,
			StretchMinutes: 60,
			NorthLatCap:    90,
			BearingFrom:    150,
			BearingTo:      310,
		},
		Filters: Filters{
			// Placeholders. The real numbers live in data/config.json, which is
			// gitignored, because this repository is public.
			MinPrice:               100000,
			PreferredPrice:         200000,
			MaxPrice:               250000,
			FloodZones:             []string{"A", "AE", "AO", "AH", "AR", "A99", "V", "VE"},
			WaterBufferFeet:        300,
			IntermittentBufferFeet: 150,
			DitchBufferFeet:        0,
			AADTCutoff:             4000,
			AADTRadiusFt:           250,
			RoadClasses:            []string{"motorway", "trunk", "primary", "secondary"},
			ManufacturedStyles: []string{
				"manufactured", "mobile", "modular", "doublewide", "double wide",
				"singlewide", "single wide", "trailer", "mfd",
			},
			ManufacturedMinYear: 2000,
			CornerFeet:          150,
			HighwayTooCloseFeet: 2000,
			HighwayComfyFeet:    5280,
			Severity:            map[string]string{},
			Penalties:           map[string]float64{},
		},
		Money: Money{
			DownPaymentPercent: 3,
			RatePercent:        6.5,
			TermYears:          30,
			PMIAnnualPercent:   0.55,
			InsuranceAnnual:    1500,
			UtilitiesMonthly:   250,
			InternetMonthly:    80,
			RevaluedYear: map[string]int{
				"alexander": 2023, "burke": 2023, "caldwell": 2025,
				"catawba": 2023, "iredell": 2023, "lincoln": 2023,
			},
			MonthlyTarget:   2000,
			MonthlyCeiling:  2200,
			DPAAmount:       10000,
			CountyTaxPer100: map[string]float64{},
		},
		Household: Household{
			DropoffGreatMin: 8,
			DropoffOkMin:    15,
			DropoffBadMin:   20,
			LotIdealAcres:   1,
		},
		Weights:   defaultWeights,
		Defaulted: true,
	}
}

// LoadConfig reads data/config.json, filling anything it leaves out from the
// defaults above so a half-written file still boots.
func LoadConfig(path string) (Config, error) {
	cfg := defaultConfig()

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}

	// Decoding onto the defaults means an absent key keeps its default and a
	// present one wins, with no per-field plumbing.
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return defaultConfig(), fmt.Errorf("parse config: %w", err)
	}
	cfg.Defaulted = false

	if cfg.Weights == nil {
		cfg.Weights = map[string]float64{}
	}
	for k, v := range defaultWeights {
		if _, ok := cfg.Weights[k]; !ok {
			cfg.Weights[k] = v
		}
	}
	if cfg.Money.CountyTaxPer100 == nil {
		cfg.Money.CountyTaxPer100 = map[string]float64{}
	}
	if cfg.Filters.Severity == nil {
		cfg.Filters.Severity = map[string]string{}
	}
	if cfg.Filters.Penalties == nil {
		cfg.Filters.Penalties = map[string]float64{}
	}
	if cfg.Filters.IntermittentBufferFeet == 0 {
		cfg.Filters.IntermittentBufferFeet = 150
	}
	if cfg.Filters.CornerFeet == 0 {
		cfg.Filters.CornerFeet = 150
	}
	if cfg.Filters.HighwayTooCloseFeet == 0 {
		cfg.Filters.HighwayTooCloseFeet = 2000
	}
	if cfg.Filters.HighwayComfyFeet == 0 {
		cfg.Filters.HighwayComfyFeet = 5280
	}
	return cfg, nil
}

// HasOrigin reports whether there is an office to measure from. Without one
// every drive time would be a distance from nowhere.
func (c Config) HasOrigin() bool {
	return c.Geography.OriginLat != 0 || c.Geography.OriginLon != 0
}

// WeightTotal is what a score is scaled against, so editing one weight in
// config does not quietly change the ceiling.
func (c Config) WeightTotal() float64 {
	var total float64
	for _, k := range factorOrder {
		total += c.Weights[k]
	}
	if total == 0 {
		return 1
	}
	return total
}

// GroupLabel is the heading a band of factors renders under.
func (c Config) GroupLabel(group string) string { return forLabels[group] }

func (c Config) GroupNote(group string) string { return forNotes[group] }

// RevaluationYear is the year a county's assessed values were set.
func (m Money) RevaluationYear(county string) int {
	key := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(county), " County")))
	return m.RevaluedYear[key]
}

// InBearingArc reports whether a bearing falls inside the searched arc, handling
// an arc that crosses north (from 300 to 30, say) rather than assuming from < to.
func (g Geography) InBearingArc(bearing float64) bool {
	from, to := math.Mod(g.BearingFrom, 360), math.Mod(g.BearingTo, 360)
	b := math.Mod(bearing+360, 360)
	if from <= to {
		return b >= from && b <= to
	}
	return b >= from || b <= to
}
