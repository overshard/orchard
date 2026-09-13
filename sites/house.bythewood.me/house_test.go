package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every page template listed with no file behind it parses at boot and not at
// build, so nothing but doing it catches a page that was deleted and left listed.
// That failure mode ships an image that builds, passes every other test, and then
// crash-loops on "pattern matches no files".
func TestTemplatesParse(t *testing.T) {
	sub, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRendererFor(sub); err != nil {
		t.Fatalf("templates do not parse: %v", err)
	}
}

// NewRendererFor is the test's way in, so the page list the server uses is the
// page list the test parses.
func NewRendererFor(files fs.FS) (any, error) {
	for _, page := range pageTemplates {
		patterns := append(append([]string{}, layoutTemplates...), page)
		if _, err := template.New("base.html").Funcs(templateFuncs).ParseFS(files, patterns...); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// A measured lookup that found nothing nearby, which is the common case here and
// is not the same as a lookup that never ran.
func measuredFlood() FloodResult {
	f := newFloodResult()
	f.Measured = true
	f.Zone = "X"
	return f
}

func measuredRoad() RoadResult {
	r := newRoadResult()
	r.Measured = true
	return r
}

// The zero value of either lookup must not read as standing in a creek next to a
// motorway ramp. It is the shape a skipped lookup leaves behind, and it used to
// flag every such listing.
func TestUnmeasuredLookupsFlagNothing(t *testing.T) {
	cfg := defaultConfig()
	cfg.Geography.OriginLat, cfg.Geography.OriginLon = 35.90, -81.10

	a := &Assessment{
		Listing: Listing{Address: "1 Test Rd", Price: 250000, Lat: 35.87, Lon: -81.15, YearBuilt: 2005},
		Flood:   FloodResult{},
		Road:    RoadResult{},
		Morning: Morning{Commute: Leg{Minutes: 18}},
		Bearing: 240,
	}
	for _, f := range Flags(cfg, a) {
		if f.Key == ruleWaterBuffer || f.Key == ruleNextToHighway {
			t.Errorf("an unmeasured lookup must not fire %s: %s", f.Key, f.Why)
		}
	}
}

// A wrong dedupe key is invisible: it does not error, it shows the same house
// three times or merges two different ones.
func TestAddressKey(t *testing.T) {
	same := [][2]string{
		{"1234 Marchbank Road", "1234 Marchbank Rd."},
		{"118 NC 90 East", "118 nc 90 e"},
		{"226 Pleasant Hill Avenue", "226 Pleasant Hill Ave"},
	}
	for _, pair := range same {
		if addressKey(pair[0], "28600") != addressKey(pair[1], "28600") {
			t.Errorf("%q and %q should key the same", pair[0], pair[1])
		}
	}
	if addressKey("118 Marchbank Rd", "28600") == addressKey("119 Marchbank Rd", "28600") {
		t.Error("different house numbers must not key the same")
	}
	// A zip+4 and its bare zip are one place.
	if addressKey("118 Marchbank Rd", "28600-1234") != addressKey("118 Marchbank Rd", "28600") {
		t.Error("zip+4 should key as its bare zip")
	}
}

// The searched arc is south through west, which does not cross north. One that
// does is the case a naive from <= to comparison gets silently wrong.
func TestBearingArc(t *testing.T) {
	g := Geography{BearingFrom: 150, BearingTo: 310}
	for _, b := range []float64{150, 225, 310, 286} {
		if !g.InBearingArc(b) {
			t.Errorf("%v should be inside 150..310", b)
		}
	}
	for _, b := range []float64{0, 90, 149, 311, 359} {
		if g.InBearingArc(b) {
			t.Errorf("%v should be outside 150..310", b)
		}
	}

	crossing := Geography{BearingFrom: 300, BearingTo: 30}
	for _, b := range []float64{300, 350, 0, 29} {
		if !crossing.InBearingArc(b) {
			t.Errorf("%v should be inside an arc that crosses north", b)
		}
	}
	if crossing.InBearingArc(180) {
		t.Error("180 should be outside 300..30")
	}
}

// Two points well apart in both axes, since a pair that differs mostly in one
// would not catch a swapped latitude and longitude.
func TestBearing(t *testing.T) {
	b := bearingDeg(35.90, -81.10, 35.92, -81.18)
	if b < 270 || b > 300 {
		t.Errorf("west northwest expected, got %.0f", b)
	}
	south := bearingDeg(35.90, -81.10, 35.73, -81.34)
	if south < 190 || south > 250 {
		t.Errorf("southwest expected, got %.0f", south)
	}
}

// The payment formula, checked against a figure that can be worked out by hand:
// $100,000 at 6% over 30 years is $599.55 a month.
func TestAmortize(t *testing.T) {
	got := amortize(100000, 6, 30)
	if math.Abs(got-599.55) > 0.05 {
		t.Errorf("want about 599.55, got %.2f", got)
	}
	if amortize(0, 6, 30) != 0 {
		t.Error("no loan is no payment")
	}
	// A zero rate has to divide rather than hit the formula's division by zero.
	if got := amortize(120000, 0, 10); math.Abs(got-1000) > 0.01 {
		t.Errorf("zero rate should be principal over the term, got %.2f", got)
	}
}

func TestMonthlyEstimate(t *testing.T) {
	m := Money{
		DownPaymentPercent: 3,
		RatePercent:        6.5,
		TermYears:          30,
		PMIAnnualPercent:   0.55,
		InsuranceAnnual:    1500,
		UtilitiesMonthly:   250,
		InternetMonthly:    80,
		DPAAmount:          10000,
		CountyTaxPer100:    map[string]float64{"alexander": 0.65, "default": 0.6},
	}

	base := m.Estimate(300000, 0, 0, "Alexander", "", false)
	if base.Loan != 300000*0.97 {
		t.Errorf("3%% down on 300k is a 291,000 loan, got %.0f", base.Loan)
	}
	// 0.65 per $100 on 300k is $1,950 a year, so $162.50 a month.
	if math.Abs(base.Tax-162.50) > 0.5 {
		t.Errorf("Alexander tax should be about 162.50 a month, got %.2f", base.Tax)
	}
	if base.PMI == 0 {
		t.Error("PMI applies above 80% loan to value")
	}

	// The assistance goes in as cash, so it lowers the loan, the payment and the
	// PMI all at once.
	dpa := m.Estimate(300000, 0, 0, "Alexander", "", true)
	if dpa.Loan >= base.Loan || dpa.Total >= base.Total {
		t.Error("DPA should lower the loan and the payment")
	}

	// A reported tax bill wins over the county rate, since it is the actual bill
	// on the actual assessment.
	reported := m.Estimate(300000, 2400, 0, "Alexander", "", false)
	if math.Abs(reported.Tax-200) > 0.01 {
		t.Errorf("a reported 2400 a year is 200 a month, got %.2f", reported.Tax)
	}

	// An unlisted county falls back rather than charging no tax at all.
	fallback := m.Estimate(300000, 0, 0, "Yadkin", "", false)
	if fallback.Tax == 0 {
		t.Error("an unknown county should use the default rate, not zero")
	}
}

// PMI has to stop once the loan is under 80% of the price, or the figure on every
// card with a big deposit is too high.
func TestPMIStopsUnder80(t *testing.T) {
	m := Money{DownPaymentPercent: 25, RatePercent: 6, TermYears: 30, PMIAnnualPercent: 0.55}
	if got := m.Estimate(200000, 0, 0, "", "", false); got.PMI != 0 {
		t.Errorf("25%% down is 75%% LTV and needs no PMI, got %.2f", got.PMI)
	}
}

// JSON cannot carry an infinity, and these structs are cached as JSON, so a
// marshal would fail on exactly the rows that are furthest from water.
func TestFloodResultRoundTrips(t *testing.T) {
	in := newFloodResult()
	in.Zone = "X"
	in.Measured = true
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out FloodResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SFHAFeet != notFound || out.WaterFeet != notFound {
		t.Errorf("not found should survive a round trip, got %+v", out)
	}
	// And a not-found distance is the best possible margin, not the worst.
	if distanceRaw(notFound, 2640) != 1 {
		t.Error("nothing within the search radius is the best margin")
	}
	if distanceRaw(0, 2640) != 0 {
		t.Error("standing in it is the worst margin")
	}
}

// Off a small road that leads to a main road is the arrangement wanted, so a main
// road has to be at frontage distance to count. This is the test that keeps the
// filter from throwing out most of the county.
func TestFrontsMainRoad(t *testing.T) {
	cfg := defaultConfig()
	cfg.Filters.AADTCutoff = 4000
	cfg.Filters.AADTRadiusFt = 250

	onIt := RoadResult{AADT: 9800, AADTFeet: 60, AADTRoute: "NC 90", Class: "secondary", ClassFeet: 55}
	if _, bad := frontsMainRoad(cfg, onIt); !bad {
		t.Error("a house 60ft from a road carrying 9,800 a day fronts it")
	}

	nearIt := RoadResult{AADT: 9800, AADTFeet: 1400, AADTRoute: "NC 90", Class: "residential", ClassFeet: 40}
	if why, bad := frontsMainRoad(cfg, nearIt); bad {
		t.Errorf("a quarter mile off NC 90 on a residential street is fine, got %q", why)
	}

	// A primary road drawn 400ft away is not the frontage either.
	notFrontage := RoadResult{Class: "primary", ClassFeet: 420}
	if _, bad := frontsMainRoad(cfg, notFrontage); bad {
		t.Error("a primary road past the frontage radius is not the frontage")
	}

	// A quiet secondary is judged on its count, not on the label.
	quietSecondary := RoadResult{Class: "secondary", ClassFeet: 50, AADT: 900, AADTFeet: 50}
	if _, bad := frontsMainRoad(cfg, quietSecondary); bad {
		t.Error("a secondary road carrying 900 a day is two quiet lanes")
	}
}

func TestCornerAndRampFlags(t *testing.T) {
	cfg := defaultConfig()
	cfg.Geography.OriginLat, cfg.Geography.OriginLon = 35.90, -81.10
	cfg.Filters.CornerFeet = 150

	a := &Assessment{
		Listing: Listing{Address: "1 Test Rd", Price: 250000, Lat: 35.87, Lon: -81.15, YearBuilt: 2005},
		Flood:   measuredFlood(),
		Road: RoadResult{
			Measured: true, AADTFeet: notFound, ClassFeet: 40, Class: "residential",
			NearbyRoads: []string{"Sipe Rd", "Taft Ave"},
			RampFeet:    400,
		},
		Morning: Morning{Commute: Leg{Minutes: 18}},
		Bearing: 240,
	}

	flags := Flags(cfg, a)
	var corner, highway bool
	for _, f := range flags {
		switch f.Key {
		case ruleCorner:
			corner = true
			if f.Exclude {
				t.Error("a corner lot is points off, not an exclusion")
			}
			if f.Penalty == 0 {
				t.Error("a corner lot has to cost something or the flag is decoration")
			}
		case ruleNextToHighway:
			highway = true
			if f.Exclude {
				t.Error("sitting near a ramp is points off, not an exclusion")
			}
		}
	}
	if !corner {
		t.Error("two roads inside the corner radius is a corner lot")
	}
	if !highway {
		t.Error("400ft from a ramp is too close")
	}
	if len(Excluded(flags)) != 0 {
		t.Errorf("nothing here should exclude, got %v", Excluded(flags))
	}
}

// Severity is config, because which of these is a dealbreaker is the reader's
// call and it moves as they see houses.
func TestSeverityIsConfigurable(t *testing.T) {
	cfg := defaultConfig()
	cfg.Filters.Severity = map[string]string{ruleManufactured: "exclude"}

	a := &Assessment{
		Listing: Listing{Address: "1 Test Rd", Price: 189000, Lat: 35.87, Lon: -81.15,
			YearBuilt: 1985, Style: "Doublewide", PropertyType: "Manufactured Home"},
		Flood:   measuredFlood(),
		Road:    measuredRoad(),
		Morning: Morning{Commute: Leg{Minutes: 18}},
		Bearing: 240,
	}
	if len(Excluded(Flags(cfg, a))) == 0 {
		t.Error("with manufactured set to exclude, an 1985 doublewide is excluded")
	}

	cfg.Filters.Severity = map[string]string{}
	flags := Flags(cfg, a)
	if len(Excluded(flags)) != 0 {
		t.Error("by default an old doublewide is points off, not excluded")
	}
	if PenaltyTotal(flags) == 0 {
		t.Error("and the points have to come off")
	}
}

// Modular is built to the residential code on a permanent foundation, so it is
// recognised and deliberately not flagged.
func TestModularIsNotManufactured(t *testing.T) {
	f := defaultConfig().Filters
	if _, bad := manufacturedKind(Listing{Style: "Modular"}, f); bad {
		t.Error("modular is not the thing being avoided")
	}
	if kind, bad := manufacturedKind(Listing{Style: "Singlewide"}, f); !bad || kind != "singlewide" {
		t.Errorf("singlewide should be flagged, got %q %v", kind, bad)
	}
	if _, bad := manufacturedKind(Listing{Style: "Ranch", PropertyType: "Single Family Residence"}, f); bad {
		t.Error("a site built ranch is not manufactured")
	}
}

// A lookup that failed must not lower the score. A failed lookup says nothing
// about the house, and the earlier rule here was the opposite: it kept unmeasured
// weight in the denominator so a county server having a bad morning marked a
// house down. What it costs is that a house with gaps is scored on less of
// itself, so the breakdown has to report how much it actually saw.
func TestAFailedLookupDoesNotLowerTheScore(t *testing.T) {
	cfg := defaultConfig()
	cfg.Geography.OriginLat, cfg.Geography.OriginLon = 35.90, -81.10

	good := Assessment{
		Morning: Morning{Commute: Leg{Minutes: 20}, WithElementary: Leg{Minutes: 25}, DetourMin: 5},
		Flood:   measuredFlood(),
		Road:    measuredRoad(),
		Monthly: Monthly{Total: 2000},
	}
	// The same house, with the drop-off lookup having failed rather than having
	// come back badly.
	gappy := good
	gappy.Morning = Morning{Commute: Leg{Minutes: 20}, Partial: true}

	a, b := Score(cfg, good), Score(cfg, gappy)

	if b.Unmeasured() <= 0 {
		t.Fatal("the missing detour should be counted as unmeasured")
	}
	if b.MeasuredPct() >= a.MeasuredPct() {
		t.Errorf("the house with the gap was measured on %.0f%% and the whole one on %.0f%%",
			b.MeasuredPct(), a.MeasuredPct())
	}

	// The old rule kept the unmeasured weight in the denominator, which is the
	// same arithmetic as scoring the failure zero. Anything at or below that is
	// the house wearing somebody else's downtime.
	oldRule := b.Score * b.Measured() / b.Total
	if b.Score <= oldRule {
		t.Errorf("a failed lookup should cost less than scoring it zero, got %.1f against %.1f",
			b.Score, oldRule)
	}

	// And it must stay in the same neighbourhood as the house it is a copy of,
	// rather than collapsing the way it used to.
	if b.Score < a.Score-2 {
		t.Errorf("a failed detour lookup dropped the score from %.1f to %.1f", a.Score, b.Score)
	}
}

// A weight edit has to change the ranking and leave the scale alone.
func TestScoreStaysOutOfAHundred(t *testing.T) {
	cfg := defaultConfig()
	perfect := Assessment{
		Morning: Morning{Commute: Leg{Minutes: 10}, DetourMin: 1},
		Flood:   measuredFlood(),
		Road:    RoadResult{Measured: true, AADT: 200, AADTFeet: 100, Class: "residential", RampFeet: 6000},
		Monthly: Monthly{Total: 1800},
		Zones:   SchoolZones{Verified: true, K8: true},
		Drives:  map[string]Leg{"a": {Minutes: 9}},
		Listing: Listing{Acres: 1.5},
	}
	got := Score(cfg, perfect)
	if got.Score > 100.01 || got.Score < 0 {
		t.Errorf("score out of range: %.2f", got.Score)
	}

	cfg.Weights["dropoff"] = 80
	again := Score(cfg, perfect)
	if again.Score > 100.01 {
		t.Errorf("a heavier weight must not raise the ceiling: %.2f", again.Score)
	}
}

// Ray casting and the flat projection are the two places a quiet geometry bug
// would turn into a wrong flood answer.
func TestGeometry(t *testing.T) {
	square := [][]float64{{-81.1, 35.9}, {-81.0, 35.9}, {-81.0, 36.0}, {-81.1, 36.0}, {-81.1, 35.9}}
	if !pointInRing(point{35.95, -81.05}, square) {
		t.Error("the middle of the square is inside it")
	}
	if pointInRing(point{35.95, -80.9}, square) {
		t.Error("a point east of the square is outside it")
	}

	g := esriGeometry{Rings: [][][]float64{square}}
	if d := g.distanceFeet(point{35.95, -81.05}); d != 0 {
		t.Errorf("inside a polygon is zero distance, got %.0f", d)
	}
	if d := g.distanceFeet(point{35.95, -80.99}); d < 1000 || d > 4000 {
		// A hundredth of a degree of longitude at this latitude is about 2,950ft.
		t.Errorf("about 2,950ft expected, got %.0f", d)
	}

	// A degree of latitude is about 364,000ft, so a tenth of one is 36,400.
	d := haversineFeet(35.9, -81.0, 36.0, -81.0)
	if math.Abs(d-36400) > 600 {
		t.Errorf("a tenth of a degree of latitude is about 36,400ft, got %.0f", d)
	}
}

// Column names differ by MLS and by export template, so the reader matches on a
// squashed form. A header spelling it does not know is a file that imports as
// nothing, silently.
func TestCSVAdapterReadsVariedHeaders(t *testing.T) {
	dir := t.TempDir()
	body := "MLS#,Street Address,City,ZipCode,ListPrice,Bedrooms,BathroomsTotal,LivingArea,LotSizeAcres,YearBuilt,PublicRemarks,PhotoURLs\n" +
		"123,\"118 Marchbank Rd\",Taylorsville,28600,\"$231,500\",3,2,\"1,684\",1.12 ac,1996,Nice brick ranch,\"https://example.test/a.jpg;https://example.test/b.jpg\"\n"
	if err := os.WriteFile(filepath.Join(dir, "export.csv"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	rows, err := NewCSVAdapter(dir).Fetch(nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want one row, got %d", len(rows))
	}
	l := rows[0]
	if l.Price != 231500 {
		t.Errorf("a dollar sign and a comma are still a price: got %d", l.Price)
	}
	if l.SqFt != 1684 {
		t.Errorf("a comma is still a square footage: got %d", l.SqFt)
	}
	if l.Acres != 1.12 {
		t.Errorf("a unit suffix is still an acreage: got %v", l.Acres)
	}
	if len(l.Photos) != 2 {
		t.Errorf("want two photos, got %v", l.Photos)
	}
	if l.MLS != "123" {
		t.Errorf("MLS# is an MLS number: got %q", l.MLS)
	}
}

// A missing address column is an error rather than an empty import, because a
// file that quietly imports nothing looks like a market with no houses in it.
func TestCSVAdapterRefusesAFileWithNoAddress(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.csv"), []byte("price,beds\n1,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCSVAdapter(dir).Fetch(nil); err == nil {
		t.Error("a file with no address column should be an error")
	}
}

// The zone layers carry a district number and shout the name, and the school point
// layers spell the same school differently. Both have to land on one school.
func TestZoneNameTidying(t *testing.T) {
	if got := tidyZoneName("328 TAYLORSVILLE"); got != "Taylorsville" {
		t.Errorf("got %q", got)
	}
	if got := tidyZoneName("306 EAST MIDDLE"); got != "East Middle" {
		t.Errorf("got %q", got)
	}
	if got := tidyZoneName("Hudson Elementary"); got != "Hudson Elementary" {
		t.Errorf("an already tidy name should survive: got %q", got)
	}
}

func TestSchoolMatching(t *testing.T) {
	points := []schoolPoint{
		{Name: "East Alexander Middle School", Lat: 35.88, Lon: -81.10},
		{Name: "Taylorsville Elementary School", Lat: 35.92, Lon: -81.18},
		{Name: "West Alexander Middle School", Lat: 35.90, Lon: -81.25},
	}
	if lat, _, ok := bestSchoolMatch(points, "East Middle"); !ok || lat != 35.88 {
		t.Errorf("East Middle is East Alexander Middle: ok=%v lat=%v", ok, lat)
	}
	if lat, _, ok := bestSchoolMatch(points, "Taylorsville"); !ok || lat != 35.92 {
		t.Errorf("Taylorsville is Taylorsville Elementary: ok=%v lat=%v", ok, lat)
	}
	if _, _, ok := bestSchoolMatch(points, "Hudson"); ok {
		t.Error("a school in another county should not match")
	}
}

// A number on a card has to read the same everywhere, so the formatters are
// pinned rather than reasoned about.
func TestFormatters(t *testing.T) {
	cases := []struct{ got, want string }{
		{commaInt(1684), "1,684"},
		{commaInt(231500), "231,500"},
		{commaInt(-500), "-500"},
		{money(2500), "$2,500"},
		{money(2499.6), "$2,500"},
		{feet(notFound), "none nearby"},
		{feet(284), "280 ft"},
		{feet(5280), "1.00 mi"},
		{fmtMinutes(0), "unknown"},
		{fmtMinutes(17.4), "17 min"},
		{trimFloat(1.00), "1"},
		{trimFloat(1.25), "1.25"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q want %q", c.got, c.want)
		}
	}
}

func TestDetourBands(t *testing.T) {
	cfg := defaultConfig()
	if got := detourBand(cfg, 6); got != "good" {
		t.Errorf("under 8 minutes is great: got %q", got)
	}
	if got := detourBand(cfg, 12); got != "fair" {
		t.Errorf("under 15 is workable: got %q", got)
	}
	if got := detourBand(cfg, 25); got != "poor" {
		t.Errorf("over 20 is a real problem: got %q", got)
	}
}

// A config that is half written still has to boot, because a dashboard that
// refuses to start is harder to fix than one that tells you what it is missing.
func TestPartialConfigFillsFromDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"label":"half","weights":{"dropoff":40}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Weights["dropoff"] != 40 {
		t.Error("a stated weight should win")
	}
	if cfg.Weights["flood"] != defaultWeights["flood"] {
		t.Error("an absent weight should fall back")
	}
	if cfg.Filters.AADTCutoff != defaultConfig().Filters.AADTCutoff {
		t.Error("an absent filter should fall back")
	}
	if cfg.Defaulted {
		t.Error("a file that exists is not the defaults")
	}

	missing, err := LoadConfig(filepath.Join(dir, "nope.json"))
	if err != nil {
		t.Fatalf("a missing config is not an error: %v", err)
	}
	if !missing.Defaulted {
		t.Error("a missing config has to say so, since every page reports it")
	}
	if missing.HasOrigin() {
		t.Error("the defaults must not invent an office to measure from")
	}
}

// Every factor's weight has to be reachable from the order the breakdown renders
// in, or a weight in config silently does nothing.
func TestEveryWeightIsOrderedAndLabelled(t *testing.T) {
	for key := range defaultWeights {
		found := false
		for _, k := range factorOrder {
			if k == key {
				found = true
			}
		}
		if !found {
			t.Errorf("weight %q is not in factorOrder, so it is never scored", key)
		}
		if factorLabels[key] == "" {
			t.Errorf("weight %q has no label, so the breakdown renders a blank row", key)
		}
	}
}

// The slope arithmetic, without a network call. A ten foot rise across a 75ft
// sample is 13%, which is over the line, and the same rise across the whole grid
// is not.
func TestComputeTerrain(t *testing.T) {
	flat := make([]float64, terrainGrid*terrainGrid)
	for i := range flat {
		flat[i] = 1120
	}
	got := computeTerrain(flat, 75)
	if got.ReliefFeet != 0 || got.MeanSlopePct != 0 || got.FlatShare != 1 {
		t.Errorf("a flat grid is all usable: %+v", got)
	}

	// A constant fall of ten feet per sample, so every slope is 13.3%.
	steep := make([]float64, terrainGrid*terrainGrid)
	for row := 0; row < terrainGrid; row++ {
		for col := 0; col < terrainGrid; col++ {
			steep[row*terrainGrid+col] = 1200 - float64(row)*10
		}
	}
	got = computeTerrain(steep, 75)
	if got.FlatShare != 0 {
		t.Errorf("a 13%% hillside has no buildable share: %+v", got)
	}
	if math.Abs(got.ReliefFeet-40) > 0.01 {
		t.Errorf("four steps of ten feet is forty feet of relief, got %.1f", got.ReliefFeet)
	}

	// Fewer than half the samples back means the numbers describe different ground.
	sparse := make([]float64, terrainGrid*terrainGrid)
	for i := range sparse {
		sparse[i] = math.NaN()
	}
	sparse[0], sparse[1] = 1100, 1101
	if !computeTerrain(sparse, 75).Partial {
		t.Error("two samples out of twenty five is not an answer")
	}
}

// A fence is read off the listing's own words, so the false positives are the
// thing to pin.
func TestFenceNote(t *testing.T) {
	if FenceNote(Listing{Remarks: "Brick ranch with a fenced back lot."}) == "" {
		t.Error("a fenced back lot is a fence")
	}
	if FenceNote(Listing{Remarks: "Level yard, newer roof."}) != "" {
		t.Error("no mention is no fence")
	}
	if got := FenceNote(Listing{Remarks: "Enjoy the fenced community pool."}); got != "" {
		t.Errorf("a fenced community pool is not a fenced yard, got %q", got)
	}
}

// Flat ground and a fence are bonuses inside the land factor, so each has to move
// the number and neither may dominate the acreage.
func TestLandFactorParts(t *testing.T) {
	h := defaultConfig().Household
	base := Assessment{Listing: Listing{Acres: 1.2}}

	bare := landRaw(h, base)

	flat := base
	flat.Terrain = TerrainResult{FlatShare: 1, ReliefFeet: 4}
	withFlat := landRaw(h, flat)

	fenced := flat
	fenced.Fence = "fenced"
	withBoth := landRaw(h, fenced)

	if withFlat <= bare {
		t.Error("flat ground has to be worth something")
	}
	if withBoth <= withFlat {
		t.Error("a fence has to be worth something")
	}
	if withBoth > 1.0001 {
		t.Errorf("the factor stays inside 0..1, got %.3f", withBoth)
	}

	// A hillside on two acres must not beat flat ground on one.
	hill := Assessment{Listing: Listing{Acres: 2}, Terrain: TerrainResult{FlatShare: 0.05, ReliefFeet: 70}}
	level := Assessment{Listing: Listing{Acres: 1}, Terrain: TerrainResult{FlatShare: 1, ReliefFeet: 5}}
	if landRaw(h, hill) >= landRaw(h, level) {
		t.Error("two acres on a ridge is not two acres")
	}
}

// The facts row has to actually land. A hand-maintained column list, a row of
// question marks and a list of arguments drifted three apart once and the only
// symptom was an empty table behind a dashboard that said it had scored sixteen
// listings, so this writes one for real and reads it back.
func TestWriteFactsRoundTrips(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	cfg := defaultConfig()
	cfg.Geography.OriginLat, cfg.Geography.OriginLon = 35.90, -81.10
	// The committed defaults are placeholders, so a test pricing a real house
	// states the ceiling it means rather than leaning on them.
	cfg.Filters.MaxPrice = 250000

	l := Listing{
		Address: "118 Marchbank Rd", City: "Taylorsville", Zip: "28600", County: "Alexander",
		Source: "test", Lat: 35.9083, Lon: -81.1724,
		Price: 231500, Beds: 3, Baths: 2, SqFt: 1684, Acres: 1.12,
		YearBuilt: 1996, Style: "Ranch", Remarks: "fenced back lot",
		Photos: []string{"https://example.test/a.jpg"},
	}
	id, isNew, _, err := Upsert(ctx, db, l, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Fatal("the first write is new")
	}

	a := &Assessment{
		Listing: l,
		Flood:   measuredFlood(),
		Road:    measuredRoad(),
		Zones:   SchoolZones{Elementary: "Taylorsville", Middle: "East Middle", High: "Alexander Central", Source: "Alexander County Schools", Verified: true},
		Morning: Morning{Commute: Leg{Minutes: 13, Miles: 8.2}, WithElementary: Leg{Minutes: 16}, DetourMin: 3},
		Monthly: cfg.Money.Estimate(l.Price, 0, 0, l.County, l.City, false),
		Terrain: TerrainResult{FlatShare: 0.75, ReliefFeet: 12, MeanSlopePct: 4},
		Fence:   FenceNote(l),
		Bearing: 282,
		Drives:  map[string]Leg{"cvmc": {Minutes: 31, Miles: 22}},
	}
	a.Flags = Flags(cfg, a)
	a.Excluded = Excluded(a.Flags)
	if len(a.Excluded) > 0 {
		t.Fatalf("the fixture should pass every rule: %v", a.Excluded)
	}

	r := &Refresher{db: db, cfg: cfg}
	if err := r.writeFacts(ctx, id, a, Score(cfg, *a)); err != nil {
		t.Fatalf("writeFacts: %v", err)
	}

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM facts`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("want one facts row, got %d", rows)
	}

	// And a second write has to update rather than fail on the conflict.
	if err := r.writeFacts(ctx, id, a, Score(cfg, *a)); err != nil {
		t.Fatalf("second writeFacts: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM facts`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("a rewrite must update, not insert: got %d rows", rows)
	}

	card, err := OneCard(ctx, db, id)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if card.Address != l.Address {
		t.Errorf("address came back as %q", card.Address)
	}
	if card.Score == 0 {
		t.Error("a scored listing should not read back as zero")
	}
	if card.Elementary != "Taylorsville" || !card.ZoningVerified {
		t.Errorf("zones came back as %q verified=%v", card.Elementary, card.ZoningVerified)
	}
	if card.DetourMin != 3 {
		t.Errorf("detour came back as %v", card.DetourMin)
	}
	if card.Fence == "" {
		t.Error("the fence note came back empty")
	}
	if card.FlatShare != 0.75 {
		t.Errorf("flat share came back as %v", card.FlatShare)
	}
	if card.MonthlyTotal == 0 {
		t.Error("the monthly figure came back as zero")
	}
	if len(card.Photos) != 1 {
		t.Errorf("want one photo, got %v", card.Photos)
	}
	if len(card.Drives) != 1 {
		t.Errorf("want one drive, got %v", card.Drives)
	}

	// The verdict is its own table and a refresh must never touch it.
	if err := SetVerdict(ctx, db, id, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.writeFacts(ctx, id, a, Score(cfg, *a)); err != nil {
		t.Fatal(err)
	}
	card, err = OneCard(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	if card.Rating != 1 {
		t.Errorf("a refresh wiped the verdict, rating=%d", card.Rating)
	}
}

// Dedupe across sources, which is the failure state worth a test: the same house
// arriving three times from three exports must be one row.
func TestUpsertDedupes(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now()

	base := Listing{MLS: "A1", Address: "118 Marchbank Rd", Zip: "28600", Price: 231500,
		Source: "one", Lat: 35.9083, Lon: -81.1724}
	id, _, _, err := Upsert(ctx, db, base, now)
	if err != nil {
		t.Fatal(err)
	}

	// Same MLS number, different spelling of the address.
	again := base
	again.Address = "118 Marchbank Road"
	again.Source = "two"
	id2, isNew, _, err := Upsert(ctx, db, again, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if isNew || id2 != id {
		t.Error("the same MLS number is the same house")
	}

	// No MLS number, but the same address.
	noMLS := base
	noMLS.MLS = ""
	noMLS.Address = "118 MARCHBANK RD."
	id3, isNew, _, err := Upsert(ctx, db, noMLS, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if isNew || id3 != id {
		t.Error("the same address is the same house")
	}

	// No MLS number and a different looking address, but the same spot.
	nearby := base
	nearby.MLS = ""
	nearby.Address = "118 Marchbank"
	nearby.Lat, nearby.Lon = 35.90831, -81.17241
	id4, isNew, _, err := Upsert(ctx, db, nearby, now.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if isNew || id4 != id {
		t.Error("a listing a few feet away is the same house")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM listings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("four arrivals of one house should be one row, got %d", count)
	}

	// A price change is reported and the original price is kept, which is what a
	// PRICE DROP badge is measured from.
	cut := base
	cut.Price = 221500
	_, _, moved, err := Upsert(ctx, db, cut, now.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Error("a different price is a price change")
	}

	card, err := OneCard(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	if card.PriceFrom != 231500 || !card.PriceDropped() {
		t.Errorf("want a drop from 231,500, got from=%d dropped=%v", card.PriceFrom, card.PriceDropped())
	}

	// first_seen never moves, because it is the only thing here that says how long
	// a house has actually been sitting.
	var firstSeen int64
	if err := db.QueryRow(`SELECT first_seen FROM listings WHERE id = ?`, id).Scan(&firstSeen); err != nil {
		t.Fatal(err)
	}
	if firstSeen != now.Unix() {
		t.Errorf("first_seen moved: want %d got %d", now.Unix(), firstSeen)
	}
}

func TestScoreIsNotZeroedByAWantFlag(t *testing.T) {
	cfg := defaultConfig()
	cfg.Geography.OriginLat, cfg.Geography.OriginLon = 35.90, -81.10
	cfg.Filters.MaxPrice = 250000

	l := Listing{Address: "118 Marchbank Rd", Price: 231500, Acres: 1.12, Lat: 35.9083, Lon: -81.1724}
	a := &Assessment{
		Listing: l,
		Flood:   measuredFlood(),
		Road:    measuredRoad(),
		Zones:   SchoolZones{Elementary: "Taylorsville", Verified: true},
		Morning: Morning{Commute: Leg{Minutes: 13}, DetourMin: 3},
		Monthly: cfg.Money.Estimate(l.Price, 0, 0, "Alexander", "", false),
		Terrain: TerrainResult{FlatShare: 0.75},
		Bearing: 282,
	}
	a.Flags = Flags(cfg, a)
	a.Excluded = Excluded(a.Flags)
	if len(a.Excluded) > 0 {
		t.Fatalf("nothing here should exclude: %v", a.Excluded)
	}
	if got := Score(cfg, *a); got.Score <= 0 {
		t.Errorf("a listing that passes every rule must score above zero, got %.2f (gross %.2f docked %.2f)",
			got.Score, got.Gross, got.Docked)
	}
}

// A mirror with no data for North Carolina answers 200 with an empty element
// list, which reads as a house with no road within six hundred feet. Treating
// that as an answer cached a blank and scored the listing as having no frontage,
// so an empty list has to be an error and the next mirror tried.
func TestEmptyOverpassAnswerIsAnError(t *testing.T) {
	var hits []string
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "empty")
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	defer empty.Close()

	real := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "real")
		_, _ = w.Write([]byte(`{"elements":[{"type":"way","tags":{"highway":"residential","name":"Marchbank Rd"},
			"geometry":[{"lat":35.9083,"lon":-81.1724},{"lat":35.9085,"lon":-81.1720}]}]}`))
	}))
	defer real.Close()

	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	saved := overpassMirrors
	overpassMirrors = []string{empty.URL, real.URL}
	defer func() { overpassMirrors = saved }()

	r := NewRoads(db, []string{"primary"}, 150)
	ans, err := r.osmAround(context.Background(), 35.9083, -81.1724, 150)
	if err != nil {
		t.Fatalf("the second mirror should have answered: %v", err)
	}
	if ans.class != "residential" {
		t.Errorf("want the real mirror's answer, got %q", ans.class)
	}
	if len(hits) != 2 || hits[0] != "empty" || hits[1] != "real" {
		t.Errorf("mirrors should be tried in order, got %v", hits)
	}

	// And when every mirror is empty it is an error, not a blank answer. A fresh
	// Roads, because the mirror list is read once when one is built.
	overpassMirrors = []string{empty.URL}
	if _, err := NewRoads(db, []string{"primary"}, 150).
		osmAround(context.Background(), 35.91, -81.18, 150); err == nil {
		t.Error("an empty answer from every mirror is an error")
	}
}

// Two exports describe the same house and neither has everything, so a blank from
// one must not wipe a real value from the other. An agent's export has the
// photographs and a Redfin one has the coordinate.
func TestUpsertKeepsWhatTheNewRowLacks(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now()

	// An agent's export: photographs, remarks and a link, but no coordinate.
	fromCSV := Listing{
		MLS: "5519228", Source: "csv", Address: "118 Marchbank Rd", Zip: "28600",
		Price: 231500, Beds: 3, Acres: 1.12, YearBuilt: 1996,
		Style: "Ranch", Remarks: "fenced back lot", URL: "https://example.test/listing",
		TaxAnnual: 1712,
		Photos:    []string{"https://example.test/a.jpg", "https://example.test/b.jpg"},
	}
	id, _, _, err := Upsert(ctx, db, fromCSV, now)
	if err != nil {
		t.Fatal(err)
	}

	// A Redfin export an hour later: a coordinate and a fresh price, none of the rest.
	fromAPI := Listing{
		MLS: "5519228", Source: "redfin", Address: "118 Marchbank Rd", Zip: "28600",
		City: "Taylorsville", County: "Alexander",
		Lat: 35.9083, Lon: -81.1724, Price: 254900, Beds: 3, Status: "Active",
	}
	if _, _, _, err := Upsert(ctx, db, fromAPI, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	card, err := OneCard(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	if card.Lat == 0 {
		t.Error("the coordinate the second export brought should have landed")
	}
	if card.Price != 254900 {
		t.Errorf("the newer price should win, got %d", card.Price)
	}
	if card.Acres != 1.12 {
		t.Errorf("acreage the second export does not carry should survive, got %v", card.Acres)
	}
	if card.YearBuilt != 1996 {
		t.Errorf("year built should survive, got %d", card.YearBuilt)
	}
	if card.Style != "Ranch" || card.Remarks == "" || card.URL == "" {
		t.Errorf("style, remarks and url should survive: %q %q %q", card.Style, card.Remarks, card.URL)
	}
	if card.TaxAnnual != 1712 {
		t.Errorf("the reported tax bill should survive, got %v", card.TaxAnnual)
	}
	if len(card.Photos) != 2 {
		t.Errorf("photographs should survive a source that has none, got %d", len(card.Photos))
	}
	if card.City != "Taylorsville" || card.County != "Alexander" {
		t.Errorf("the second export's city and county should land, got %q %q", card.City, card.County)
	}
}

// Redfin's Download All export, which is the shortest path to real listings:
// a button on a search page, no account and no key. Its header spellings are odd
// enough to be worth pinning, in particular the lot size in square feet under a
// header that does not say so, and a URL column carrying a sentence after it.
func TestCSVAdapterReadsRedfinExport(t *testing.T) {
	dir := t.TempDir()
	header := "SALE TYPE,SOLD DATE,PROPERTY TYPE,ADDRESS,CITY,STATE OR PROVINCE," +
		"ZIP OR POSTAL CODE,PRICE,BEDS,BATHS,LOCATION,SQUARE FEET,LOT SIZE,YEAR BUILT," +
		"DAYS ON MARKET,$/SQUARE FEET,HOA/MONTH,STATUS,NEXT OPEN HOUSE START TIME," +
		"NEXT OPEN HOUSE END TIME," +
		"URL (SEE https://www.redfin.com/buy-a-home/comparative-market-analysis FOR INFO ON PRICING)," +
		"SOURCE,MLS#,FAVORITE,INTERESTED,LATITUDE,LONGITUDE\n"
	row := "MLS Listing,,Single Family Residential,118 Marchbank Rd,Taylorsville,NC," +
		"28600,231500,3,2,Taylorsville,1684,48787,1996,9,157,0,Active,,," +
		"https://www.redfin.com/NC/Taylorsville/118-Marchbank-Rd/home/12345," +
		"Canopy MLS,4123456,N,N,35.9083,-81.1724\n"
	if err := os.WriteFile(filepath.Join(dir, "redfin.csv"), []byte(header+row), 0o600); err != nil {
		t.Fatal(err)
	}

	rows, err := NewCSVAdapter(dir).Fetch(nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want one row, got %d", len(rows))
	}

	l := rows[0]
	if l.Address != "118 Marchbank Rd" || l.City != "Taylorsville" || l.Zip != "28600" {
		t.Errorf("address fields came through as %q %q %q", l.Address, l.City, l.Zip)
	}
	if l.State != "NC" {
		t.Errorf("STATE OR PROVINCE should be the state, got %q", l.State)
	}
	if l.Price != 231500 || l.Beds != 3 || l.SqFt != 1684 || l.YearBuilt != 1996 {
		t.Errorf("numbers came through as %+v", l)
	}
	// 48,787 square feet is about 1.12 acres, and reading it as acres would call
	// a one acre lot a small county.
	if l.Acres < 1.1 || l.Acres > 1.13 {
		t.Errorf("lot size in square feet should convert to acres, got %v", l.Acres)
	}
	if l.Lat == 0 || l.Lon == 0 {
		t.Error("Redfin carries a coordinate, so nothing should need geocoding")
	}
	if l.MLS != "4123456" {
		t.Errorf("MLS# came through as %q", l.MLS)
	}
	if !strings.HasPrefix(l.URL, "https://www.redfin.com/") {
		t.Errorf("the URL column has a sentence in its header and still has to match, got %q", l.URL)
	}
	if l.Remarks == "" {
		t.Error("the originating MLS is worth keeping when there are no remarks")
	}

	// An acreage already in acres must not be multiplied by anything.
	plain := "ADDRESS,PRICE,LOT SIZE\n1 Test Rd,200000,1.25\n"
	if err := os.WriteFile(filepath.Join(dir, "plain.csv"), []byte(plain), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err = NewCSVAdapter(dir).Fetch(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Address == "1 Test Rd" && r.Acres != 1.25 {
			t.Errorf("a lot already in acres should stay put, got %v", r.Acres)
		}
	}
}

// The backoff has to grow. A service having a bad afternoon should be asked once
// an hour and then less often, not poked every ten minutes until it recovers.
func TestBackoffGrowsAndIsCapped(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	g := NewGuard(db, "test", GuardOpts{OpenFor: 10 * time.Minute})
	steps := []struct {
		trips int
		want  time.Duration
	}{
		{0, 10 * time.Minute},
		{1, 10 * time.Minute},
		{2, 20 * time.Minute},
		{3, 40 * time.Minute},
		{4, 80 * time.Minute},
		{20, 8 * time.Hour},
	}
	for _, s := range steps {
		if got := g.backoff(s.trips); got != s.want {
			t.Errorf("%d trips: want %s got %s", s.trips, s.want, got)
		}
	}
}

// A breaker that opens has to stay open, survive a restart, and reopen for longer
// the next time. The state is a row in SQLite for exactly that reason.
func TestBreakerPersistsAndEscalates(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var calls int
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer down.Close()

	g := NewGuard(db, "flaky", GuardOpts{
		MinInterval: time.Millisecond, TripAfter: 1, OpenFor: time.Hour, Timeout: 2 * time.Second,
	})
	ctx := context.Background()

	if _, err := g.GetRaw(ctx, down.URL); err == nil {
		t.Fatal("a 502 is an error")
	}
	if calls != 1 {
		t.Fatalf("want one call, got %d", calls)
	}

	// The second attempt must not reach the server at all.
	if _, err := g.GetRaw(ctx, down.URL); err == nil {
		t.Fatal("the breaker should be open")
	} else if _, ok := err.(*ErrGuarded); !ok {
		t.Errorf("want a guarded error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("the breaker let a request through: %d calls", calls)
	}

	// A fresh Guard is a restart. The state is in the database, so it stays shut.
	restarted := NewGuard(db, "flaky", GuardOpts{
		MinInterval: time.Millisecond, TripAfter: 1, OpenFor: time.Hour, Timeout: 2 * time.Second,
	})
	if _, err := restarted.GetRaw(ctx, down.URL); err == nil {
		t.Fatal("a restart must not reopen the breaker")
	}
	if calls != 1 {
		t.Errorf("a restart let a request through: %d calls", calls)
	}

	statuses, err := guardStatuses(db)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range statuses {
		if s.Endpoint == "flaky" {
			found = true
			if !s.Open {
				t.Error("the status page should show it backed off")
			}
		}
	}
	if !found {
		t.Error("the guard should appear in the status list")
	}
}

// The budget is the backstop for a bug that loops, so it has to bite before the
// request goes out rather than after.
func TestBudgetStopsARunawayLoop(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var calls int
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ok.Close()

	g := NewGuard(db, "budgeted", GuardOpts{
		MinInterval: time.Millisecond, Budget: 3, Window: time.Hour, Timeout: 2 * time.Second,
	})
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, _ = g.GetRaw(ctx, ok.URL)
	}
	if calls != 3 {
		t.Errorf("the budget is three, so three requests: got %d", calls)
	}
}

// Retry-After comes in two formats and reading only one means ignoring the other
// entirely, which is asking again immediately after being told not to.
func TestRetryAfterBothFormats(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	if got := retryAfter("120", now); got != 2*time.Minute {
		t.Errorf("seconds: got %s", got)
	}
	if got := retryAfter(now.Add(30*time.Minute).Format(http.TimeFormat), now); got < 29*time.Minute || got > 30*time.Minute {
		t.Errorf("http date: got %s", got)
	}
	if got := retryAfter("", now); got != 0 {
		t.Errorf("absent: got %s", got)
	}
	// A date already past asks for nothing rather than a negative wait.
	if got := retryAfter(now.Add(-time.Hour).Format(http.TimeFormat), now); got != 0 {
		t.Errorf("a past date: got %s", got)
	}
	if got := retryAfter("not a thing", now); got != 0 {
		t.Errorf("nonsense: got %s", got)
	}
}

// CREATE TABLE IF NOT EXISTS does nothing to a table that already exists, so a
// column added to the schema reaches a fresh database and no other. Every other
// test starts fresh, which is exactly why that failed in production and nowhere a
// test could see it. This one builds an old database on purpose.
func TestOldDatabaseGainsNewColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.sqlite3")

	// A guards table as it was before the backoff counter, and a facts table
	// before the terrain and corner columns.
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`
		CREATE TABLE guards (
			endpoint     TEXT PRIMARY KEY,
			failures     INTEGER NOT NULL DEFAULT 0,
			opened_at    INTEGER NOT NULL DEFAULT 0,
			window_start INTEGER NOT NULL DEFAULT 0,
			window_count INTEGER NOT NULL DEFAULT 0,
			last_error   TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO guards (endpoint, failures) VALUES ('legacy', 2);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := openDB(path)
	if err != nil {
		t.Fatalf("opening an older database should migrate it, not fail: %v", err)
	}
	defer db.Close()

	for _, c := range addedColumns {
		have, err := hasColumn(db, c.table, c.column)
		if err != nil {
			t.Fatal(err)
		}
		if !have {
			t.Errorf("%s.%s was not added", c.table, c.column)
		}
	}

	// The row that was already there survives, with the new column defaulted.
	var failures, trips int
	if err := db.QueryRow(`SELECT failures, trips FROM guards WHERE endpoint = 'legacy'`).
		Scan(&failures, &trips); err != nil {
		t.Fatalf("reading the existing row: %v", err)
	}
	if failures != 2 || trips != 0 {
		t.Errorf("want failures 2 and trips 0, got %d and %d", failures, trips)
	}

	// And the guard works against it, which is the thing that was broken.
	if _, err := guardStatuses(db); err != nil {
		t.Errorf("guard status against a migrated database: %v", err)
	}

	// Opening twice must be a no-op rather than an error about a duplicate column.
	db.Close()
	again, err := openDB(path)
	if err != nil {
		t.Fatalf("the migration has to be idempotent: %v", err)
	}
	again.Close()
}

// The geocoder interpolates along the street centreline, so the coordinate it
// returns sits in the road and a point query matches no parcel at all. The
// envelope catches the neighbours too, so the nearest one has to win rather than
// the first one the server happened to return.
func TestParcelPicksTheNearest(t *testing.T) {
	// Three parcels in a row, west to east, each about 200ft wide. The point sits
	// inside the middle one.
	square := func(west, east float64) esriGeometry {
		return esriGeometry{Rings: [][][]float64{{
			{west, 35.420}, {east, 35.420}, {east, 35.422}, {west, 35.422}, {west, 35.420},
		}}}
	}
	res := esriQueryResult{Features: []esriFeature{
		{Attributes: map[string]any{"gisacres": 1.0, "cntyname": "Alexander", "parval": 100000.0}, Geometry: square(-80.690, -80.688)},
		{Attributes: map[string]any{"gisacres": 0.74, "cntyname": "Alexander", "parval": 452569.0,
			"saddno": "78", "saddstr": "MARCHBANK", "saddsttyp": "RD", "saledatetx": "20041231"},
			Geometry: square(-80.688, -80.686)},
		{Attributes: map[string]any{"gisacres": 2.0, "cntyname": "Alexander", "parval": 300000.0}, Geometry: square(-80.686, -80.684)},
	}}

	p := &Parcels{}
	got := p.pick(res, point{35.421, -80.687}, "")
	if !got.Found {
		t.Fatal("the point is inside the middle parcel")
	}
	if got.Acres != 0.74 {
		t.Errorf("the parcel the point is in should win, got %v acres", got.Acres)
	}
	if got.MarketValue != 452569 {
		t.Errorf("market value %v", got.MarketValue)
	}
	if got.Address != "78 Marchbank Rd" {
		t.Errorf("the address should come off the parts and be tidied, got %q", got.Address)
	}
	if got.LastSold != "December 2004" {
		t.Errorf("sale date %q", got.LastSold)
	}
	if got.County != "Alexander" {
		t.Errorf("county %q", got.County)
	}

	// A point well outside every parcel matches none of them rather than the
	// least far away.
	if far := p.pick(res, point{35.460, -80.687}, ""); far.Found {
		t.Error("a parcel miles away is somebody else's")
	}

	// With no acreage published the shape answers instead.
	noAcres := esriQueryResult{Features: []esriFeature{
		{Attributes: map[string]any{"cntyname": "Catawba"}, Geometry: square(-80.688, -80.686)},
	}}
	got = p.pick(noAcres, point{35.421, -80.687}, "")
	if got.Acres <= 0 || !strings.Contains(got.AcresFrom, "shape") {
		t.Errorf("a county that publishes no acreage should get it off the polygon, got %v from %q",
			got.Acres, got.AcresFrom)
	}
}

// Directions stay in capitals and everything else is title cased. Length is not
// the test, which is how the first version produced "118 Marchbank RD".
func TestTitleAddress(t *testing.T) {
	cases := map[string]string{
		"118 MARCHBANK RD":    "118 Marchbank Rd",
		"2550 US HWY 70 SE":   "2550 Us Hwy 70 SE",
		"81 N MAIN ST":        "81 N Main St",
		"3459 STATE HWY 16 S": "3459 State Hwy 16 S",
	}
	for in, want := range cases {
		if got := titleAddress(in); got != want {
			t.Errorf("titleAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

// Owner occupancy on the street, which is the thing people mean by neighbourhood
// quality. It turns on comparing two addresses written by different clerks in
// different decades, so the comparison is what gets pinned.
func TestStreetOwnerOccupancy(t *testing.T) {
	house := func(no, street, mail, mzip, szip, use string) esriFeature {
		return esriFeature{Attributes: map[string]any{
			"saddno": no, "saddstr": street, "saddsttyp": "RD",
			"mailadd": mail, "mzip": mzip, "szip": szip, "parusedesc": use,
		}}
	}
	res := esriQueryResult{Features: []esriFeature{
		// Lives there: the post comes to the house, spelled a little differently.
		house("118", "MARCHBANK", "118 Marchbank Road", "28600", "28600", "Residential"),
		house("120", "MARCHBANK", "120 MARCHBANK RD", "28600", "28600", ""),
		house("122", "MARCHBANK", "122 Marchbank Rd.", "28600", "28600", "Residential"),
		house("124", "MARCHBANK", "124 MARCHBANK RD", "28600", "28600", ""),
		house("126", "MARCHBANK", "126 MARCHBANK RD", "28600", "28600", ""),
		house("128", "MARCHBANK", "128 MARCHBANK RD", "28600", "28600", ""),
		// Let: the post goes to a landlord in another town, and to a PO box.
		house("130", "MARCHBANK", "44 Elsewhere Ave", "28601", "28600", "Residential"),
		house("132", "MARCHBANK", "PO Box 88", "28600", "28600", "Residential"),
		// Not a neighbour: a church and a school are parcels and not houses.
		house("134", "MARCHBANK", "134 MARCHBANK RD", "28600", "28600", "Church"),
		house("136", "MARCHBANK", "136 MARCHBANK RD", "28600", "28600", "School"),
		// On the roll with no mailing address at all, so it counts toward neither.
		house("138", "MARCHBANK", "", "", "28600", "Residential"),
	}}

	got := tallyStreet(res)
	if !got.Found {
		t.Fatal("nine houses is a street")
	}
	if got.Parcels != 9 {
		t.Errorf("the church and the school are not neighbours: counted %d", got.Parcels)
	}
	if got.OwnerOccupied != 6 {
		t.Errorf("six of these are lived in by their owners, got %d", got.OwnerOccupied)
	}
	if got.Unknown != 1 {
		t.Errorf("one has no mailing address, got %d", got.Unknown)
	}
	// Six of the eight that can be judged, so seventy five percent.
	if math.Abs(got.Percent-75) > 0.01 {
		t.Errorf("want 75%%, got %.1f", got.Percent)
	}
	if got.Raw() <= 0.5 {
		t.Errorf("a mostly owned street should score above the middle, got %.2f", got.Raw())
	}

	// A handful of houses is not a street and must not report a percentage.
	few := esriQueryResult{Features: res.Features[:3]}
	if tallyStreet(few).Found {
		t.Error("three houses is not a street")
	}
}

// A PO box is never the house, and a different postcode is a different place.
func TestSameAddress(t *testing.T) {
	attrs := map[string]any{}
	if sameAddress("PO Box 128", "28600", "118 Marchbank Rd", "28600", attrs) {
		t.Error("a PO box is not the house")
	}
	if sameAddress("118 Marchbank Rd", "28601", "118 Marchbank Rd", "28600", attrs) {
		t.Error("a different postcode is a different place")
	}
	if !sameAddress("118 MARCHBANK ROAD", "28600", "118 Marchbank Rd", "28600", attrs) {
		t.Error("the same address spelled two ways is one address")
	}
	if sameAddress("44 Elsewhere Ave", "28600", "118 Marchbank Rd", "28600", attrs) {
		t.Error("a different street is a different address")
	}
}

// The two address lines are written by different people from different columns
// and never match as whole strings, which is what made a street of owner
// occupiers report as ten percent owned.
func TestSameHouse(t *testing.T) {
	same := [][2]string{
		{"118 Marchbank Rd", "118 MARCHBANK"},
		{"358 WEST MAIN AVENUE", "358 MAIN AVE"},
		{"74 7TH STREET NW", "74 7TH ST"},
		{"89 6TH ST NW", "89 6TH ST"},
		{"118 MARCHBANK RD", "118 Marchbank Road"},
		{"1234 N Main St", "1234 Main Street"},
		{"78 Marchbank Rd.", "78 MARCHBANK RD"},
	}
	for _, p := range same {
		if !sameHouse(p[0], p[1]) {
			t.Errorf("%q and %q are the same house", p[0], p[1])
		}
	}
	different := [][2]string{
		{"118 Marchbank Rd", "120 Marchbank Rd"},
		{"118 Marchbank Rd", "118 Hobbs Rd"},
		{"118 Marchbank Rd", "Marchbank Rd"},
	}
	for _, p := range different {
		if sameHouse(p[0], p[1]) {
			t.Errorf("%q and %q are not the same house", p[0], p[1])
		}
	}
}

// The crime response, parsed against a real one captured from the API. It carries
// national and state series alongside the agency's own, and the population is
// nested a level deeper than the offences, both of which are easy to get subtly
// wrong in a way that produces a plausible number rather than an error.
func TestCrimeParsing(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cde_violent.json"))
	if err != nil {
		t.Skip("no captured response to parse")
	}

	var res cdeSummary
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("decode: %v", err)
	}

	const agency = "Alexander County Sheriff's Office"

	total := sumSeries(res.Offenses.Actuals, agency+" Offenses")
	if total <= 0 {
		t.Fatalf("the agency's own offences should add up to something, got %v", total)
	}
	// The national series is in the same map and is enormous. Picking it up would
	// put the rate somewhere around a million.
	if total > 10000 {
		t.Errorf("that looks like the national series, not one county: %v", total)
	}

	pop := peakPopulation(res.Populations.Population, agency)
	if pop < 20000 || pop > 60000 {
		t.Errorf("Alexander County is about 36,000 people, got %v", pop)
	}

	// A clearances series sits beside the offences one and must not be added in.
	if sumSeries(res.Offenses.Actuals, agency+" Clearances") == total {
		t.Error("offences and clearances should not be the same number")
	}

	per1k := total / pop * 1000
	if per1k <= 0 || per1k > 50 {
		t.Errorf("a violent crime rate per thousand should be single digits here, got %.2f", per1k)
	}
	t.Logf("violent offences %v over %v people, %.2f per 1,000", total, pop, per1k)
}

// The geocoder lands its point in the road, so the nearest polygon is regularly
// the neighbour's and the assessed value then belongs to the wrong house. A site
// address that matches what was typed beats proximity.
func TestParcelPrefersTheMatchingAddress(t *testing.T) {
	square := func(west, east float64) esriGeometry {
		return esriGeometry{Rings: [][][]float64{{
			{west, 35.420}, {east, 35.420}, {east, 35.422}, {west, 35.422}, {west, 35.420},
		}}}
	}
	res := esriQueryResult{Features: []esriFeature{
		{Attributes: map[string]any{"gisacres": 0.5, "cntyname": "Alexander", "parval": 200000.0,
			"saddno": "120", "saddstr": "MARCHBANK", "saddsttyp": "RD"},
			Geometry: square(-80.6872, -80.6868)},
		{Attributes: map[string]any{"gisacres": 0.9, "cntyname": "Alexander", "parval": 350000.0,
			"saddno": "118", "saddstr": "MARCHBANK", "saddsttyp": "RD"},
			Geometry: square(-80.6860, -80.6856)},
	}}

	p := &Parcels{}
	// The point sits inside the first parcel, which is 120.
	got := p.pick(res, point{35.421, -80.6870}, "118 Marchbank Rd")
	if got.Address != "118 Marchbank Rd" {
		t.Fatalf("the typed address should win over the closer lot, got %q", got.Address)
	}
	if got.MarketValue != 350000 {
		t.Errorf("the value should come off the matched parcel, got %v", got.MarketValue)
	}

	// With nothing to match on it falls back to the closest.
	if near := p.pick(res, point{35.421, -80.6870}, "44 Elsewhere Ave"); near.Address != "120 Marchbank Rd" {
		t.Errorf("an address on no parcel here should fall back to the nearest, got %q", near.Address)
	}
}

// An assessed value carried forward on the index, which is the whole point of the
// estimate: the assessment is as of a revaluation and the market has moved since.
func TestValueEstimateCarriesTheAssessmentForward(t *testing.T) {
	h := &HPI{}
	table := hpiTable{
		Places: map[string]series{
			"25860": {"2022-4": 100, "2026-2": 130},
		},
		Latest: map[string]string{"25860": "2026-2"},
	}

	got := estimateFrom(table, "Alexander County", 200000, 2023)
	if !got.Found {
		t.Fatal("a county with an index and a year should estimate")
	}
	if got.Estimate != 260000 {
		t.Errorf("200000 up 30%% is 260000, got %v", got.Estimate)
	}
	if got.MovedPct < 29.9 || got.MovedPct > 30.1 {
		t.Errorf("the index moved 30%%, got %v", got.MovedPct)
	}
	if got.Low >= got.Estimate || got.High <= got.Estimate {
		t.Errorf("the range should straddle the estimate, got %v to %v", got.Low, got.High)
	}
	if got.Basis != "the Hickory area" {
		t.Errorf("Alexander is in the Hickory index, got %q", got.Basis)
	}

	// No year means no estimate rather than a wrong one.
	if none := estimateFrom(table, "Alexander County", 200000, 0); none.Found {
		t.Error("without a revaluation year there is nothing to carry forward from")
	}
	_ = h
}

// An http transport error carries the whole URL it was handed, and one of these
// URLs has an API key in its query string, so saving the error verbatim wrote the
// key into the database.
func TestGuardErrorsDoNotKeepTheKey(t *testing.T) {
	in := `Get "https://api.usa.gov/crime/fbi/cde/agency/byStateAbbr/NC?API_KEY=sekritsekritsekrit": context deadline exceeded`
	got := redactURL(in)
	if strings.Contains(got, "sekrit") {
		t.Errorf("the key survived redaction: %s", got)
	}
	if !strings.Contains(got, "API_KEY=REDACTED") {
		t.Errorf("want the parameter kept and the value replaced, got %s", got)
	}

	// Other query parameters are not secrets and are worth keeping, since they say
	// which lookup failed.
	keep := redactURL(`https://example.test/q?f=json&outSR=4326`)
	if keep != `https://example.test/q?f=json&outSR=4326` {
		t.Errorf("an ordinary url should be untouched, got %s", keep)
	}
}

// The dial's number is sized as a percentage, and a percentage font size resolves
// against the inherited font size rather than the element's width, so the box size
// has to carry a font size with it or the score renders at a few pixels.
func TestDialSizeCarriesAFontSize(t *testing.T) {
	got := string(sizeRem("5.5rem"))
	for _, want := range []string{"width:5.5rem", "height:5.5rem", "font-size:5.5rem"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %s in %q", want, got)
		}
	}
	// Still refuses anything that could carry a declaration of its own.
	if bad := string(sizeRem("5rem;color:red")); bad != "" {
		t.Errorf("want nothing for a value with a semicolon, got %q", bad)
	}
}

// Parsing is not enough. A template function called with the wrong number of
// arguments parses cleanly and fails at execute, which reaches a reader as a 500
// on the page they were looking at. So every page is actually rendered here,
// against a card carrying something in each of the branches worth exercising.
func TestEveryPageRenders(t *testing.T) {
	sub, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	card := Card{
		ID: 1, Address: "118 Marchbank Rd", City: "Taylorsville", County: "Alexander",
		Price: 231500, MonthlyTotal: 2100, Lat: 35.42, Lon: -80.68,
		Breakdown: Breakdown{
			Gross: 61, Total: 100,
			Factors: []FactorScore{
				{Key: "schools", Label: "Schools, K-12", Group: "child", Raw: 0.55, Weight: 18, Points: 9.9, Why: "known"},
				{Key: "elbow", Label: "Elbow room", Group: "household", Weight: 6, Why: "nothing answered", Unknown: true},
			},
			Penalties: []Flag{{Why: "corner lot", Penalty: 3}},
		},
		Area:  AreaProfile{Found: true, Name: "Alexander County", Population: 37000},
		Value: ValueEstimate{Found: true, Assessed: 200000, BaseYear: 2023, Estimate: 230000, Low: 195500, High: 264500, MovedPct: 15, Basis: "the Hickory area", AsOf: "Q2 2026"},
	}

	pages := []struct {
		name string
		data any
	}{
		{"grid.html", gridPage{
			PageData: PageData{Config: cfg, Year: 2026},
			Cards:    []Card{card},
			Summary:  gridSummary{Shown: 1, BestScore: 61, UnderTarget: 1, NewToday: 1},
			Sorts:    []option{{Value: "score", Label: "Best score", On: true}, {Value: "price", Label: "Cheapest"}},
			Views:    []viewTab{{Key: "grid", Label: "All", Count: 1, On: true}},
		}},
		{"detail.html", detailPage{
			PageData: PageData{Config: cfg, Year: 2026},
			Card:     card,
			Groups:   groupFactors(cfg, card.Breakdown),
			Missing:  []SourceState{{Key: "outings", Name: "Places to go", Who: "OpenStreetMap", Failed: true, Note: "did not answer"}},
		}},
		{"notfound.html", PageData{Config: cfg, Year: 2026}},
	}

	for _, p := range pages {
		patterns := append(append([]string{}, layoutTemplates...), p.name)
		tpl, err := template.New("base.html").Funcs(templateFuncs).ParseFS(sub, patterns...)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, p.data); err != nil {
			t.Errorf("%s does not render: %v", p.name, err)
			continue
		}
		if buf.Len() < 500 {
			t.Errorf("%s rendered only %d bytes, which is not a page", p.name, buf.Len())
		}
	}
}

// An excluded house keeps its score and the dial says so, rather than reading 0
// over a breakdown that says sixty something.
func TestExcludedKeepsItsScore(t *testing.T) {
	cfg := defaultConfig()
	cfg.Geography.OriginLat, cfg.Geography.OriginLon = 35.90, -81.10

	a := Assessment{
		Morning: Morning{Commute: Leg{Minutes: 20}, DetourMin: 5},
		Flood:   measuredFlood(),
		Road:    measuredRoad(),
		Monthly: Monthly{Total: 2000},
	}
	a.Excluded = []string{"further north than we said we would look"}

	b := Score(cfg, a)
	if !b.Out {
		t.Error("a house with a reason against it should be marked out")
	}
	if b.Score <= 0 {
		t.Errorf("the score should survive being ruled out, got %.1f", b.Score)
	}
}

// Delete has to take everything with it. A thumbs down hides a house and keeps
// what was learned, and this is the other thing entirely, so a row left behind in
// any of the six tables keyed to a listing is a bug worth a test.
func TestDeleteTakesEverythingWithIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	l := Listing{
		Source: "checked", Address: "118 Marchbank Rd", Zip: "28600",
		Price: 231500, Lat: 35.42, Lon: -80.68,
		Photos: []string{"https://example.test/a.jpg"},
	}
	id, _, _, err := Upsert(ctx, db, l, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// Something in every table that hangs off a listing.
	if err := writeFactsFor(ctx, db, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO verdicts (listing_id, rating, note, updated_at) VALUES (?,?,?,?)`,
		id, -1, "too close to the road", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	// A second listing, which must survive.
	// Far enough away that the proximity rung of the dedupe does not fold it into
	// the first one.
	other := l
	other.Address = "44 Elsewhere Ave"
	other.Zip = "28601"
	other.Lat, other.Lon = 35.72, -81.02
	otherID, _, _, err := Upsert(ctx, db, other, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM listings WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"listings", "facts", "photos", "price_history", "verdicts", "drives"} {
		var n int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM "+table+" WHERE "+listingKey(table)+" = ?", id).Scan(&n); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s still has %d rows for the deleted listing", table, n)
		}
	}

	var left int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM listings WHERE id = ?`, otherID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Error("deleting one listing took the other with it")
	}
}

func listingKey(table string) string {
	if table == "listings" {
		return "id"
	}
	return "listing_id"
}

// writeFactsFor puts a minimal facts row in, so the cascade has something to take.
func writeFactsFor(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO facts (listing_id, computed_at, score) VALUES (?,?,?)`,
		id, time.Now().Unix(), 61.0)
	return err
}

// The error handed back to the caller is the one that reaches slog and the log
// shipper, so redacting only the copy written to the database left the key going
// out over the wire to another service on every timeout.
func TestGuardReturnsARedactedError(t *testing.T) {
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// A port nothing is listening on, so client.Do fails and stringifies the URL.
	g := NewGuard(db, "secret-test", GuardOpts{
		MinInterval: time.Millisecond,
		Budget:      5,
		Window:      time.Hour,
		Timeout:     time.Second,
	})

	_, err = g.GetRaw(context.Background(), "http://127.0.0.1:1/thing?API_KEY=sekritsekritsekrit")
	if err == nil {
		t.Fatal("a request to a closed port should fail")
	}
	if strings.Contains(err.Error(), "sekrit") {
		t.Errorf("the key came back in the error: %s", err)
	}

	var stored string
	if err := db.QueryRow(`SELECT last_error FROM guards WHERE endpoint = 'secret-test'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "sekrit") {
		t.Errorf("the key was written to the database: %s", stored)
	}
}

// The routing service having a bad hour must not empty the grid. Latitude and
// bearing still rule a house out, because those need no network.
func TestARoutingOutageDoesNotExcludeAnything(t *testing.T) {
	cfg := defaultConfig()
	cfg.Geography.OriginLat, cfg.Geography.OriginLon = 35.90, -81.10

	a := &Assessment{
		Listing: Listing{Lat: 35.80, Lon: -81.30},
		Morning: Morning{Partial: true},
		Flood:   measuredFlood(),
		Road:    measuredRoad(),
	}
	a.Bearing = bearingDeg(35.90, -81.10, 35.80, -81.30)

	if why, out := outsideBand(cfg, a); out {
		t.Errorf("an unrouted house inside the arc should stay in the grid, got %q", why)
	}

	// Still out when the geography itself says so, which needs no routing.
	north := &Assessment{Listing: Listing{Lat: cfg.Geography.NorthLatCap + 0.5, Lon: -81.30}}
	north.Bearing = 200
	if _, out := outsideBand(cfg, north); !out {
		t.Error("a house north of the cap is out whether or not it routed")
	}
}

// A link is shareable and the row number is not. Two things have to hold: the
// token is what a URL carries, and it is not derivable from the order rows were
// created in.
func TestPublicIDsAreOpaqueAndUnique(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		l := Listing{
			Source: "checked", Address: fmt.Sprintf("%d Marchbank Rd", 100+i*2),
			Zip: "28600", Price: 200000, Lat: 35.42 + float64(i)*0.05, Lon: -80.68,
		}
		id, _, _, err := Upsert(ctx, db, l, time.Now())
		if err != nil {
			t.Fatal(err)
		}

		var pid string
		if err := db.QueryRowContext(ctx, `SELECT public_id FROM listings WHERE id = ?`, id).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		if len(pid) != 36 {
			t.Errorf("want a 36 character token, got %q", pid)
		}
		if strings.Contains(pid, fmt.Sprint(id)) && len(fmt.Sprint(id)) > 2 {
			t.Errorf("the token should not carry the row id: %q", pid)
		}
		if seen[pid] {
			t.Fatalf("token %q handed out twice", pid)
		}
		seen[pid] = true

		// And it resolves back to the row it came from.
		got, err := listingIDFor(ctx, db, pid)
		if err != nil || got != id {
			t.Errorf("token did not resolve: got %d want %d, err %v", got, id, err)
		}
	}

	// A token nobody issued is not a listing.
	if _, err := listingIDFor(ctx, db, "00000000-0000-4000-8000-000000000000"); err == nil {
		t.Error("an unissued token should not resolve")
	}
}

// A database that predates the column has to come out the other side with every
// row addressable, or existing links break on deploy.
func TestExistingRowsGetATokenOnOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sqlite3")

	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	id, _, _, err := Upsert(context.Background(), db, Listing{
		Source: "checked", Address: "118 Marchbank Rd", Zip: "28600", Price: 200000,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Put it back the way a pre-column database looked.
	if _, err := db.Exec(`UPDATE listings SET public_id = NULL WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	var pid string
	if err := db2.QueryRow(`SELECT COALESCE(public_id,'') FROM listings WHERE id = ?`, id).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if pid == "" {
		t.Error("opening the database should have given the old row a token")
	}
}
