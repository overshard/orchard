package property

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/t.db?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func testConfig() Config {
	c := defaultConfig()
	c.Label = "test"
	c.Money.CountyTaxPer100 = map[string]float64{"alexander": 0.79, "default": 0.7}
	c.Money.InsuranceAnnual = 1500
	c.Money.UtilitiesMonthly = 300
	c.Money.InternetMonthly = 70
	c.Money.CreditScore = 740
	return c
}

// Freddie Mac leaves the ARM columns empty on recent rows, so the last line of
// the file is not reliably the last complete one.
func TestParsePMMSTakesTheLastWeekWithARate(t *testing.T) {
	body := strings.Join([]string{
		"date,pmms30,pmms30p,pmms15,pmms15p,pmms51,pmms51p,pmms51m,pmms51spread",
		"4/2/1971,7.33, ,,,,,,",
		"9/10/2026,6.76,,6.09,,,,,",
		"9/17/2026,6.95,,6.26,,,,,",
		"9/24/2026,,,,,,,,",
	}, "\n")

	m, err := parsePMMS(body)
	if err != nil {
		t.Fatal(err)
	}
	if m.Thirty != 6.95 || m.Deuce != 6.26 {
		t.Fatalf("got %v and %v, want 6.95 and 6.26", m.Thirty, m.Deuce)
	}
	if m.Week != "9/17/2026" || !m.Found {
		t.Fatalf("got week %q found %v", m.Week, m.Found)
	}
}

func TestParsePMMSRefusesAFileWithNoRate(t *testing.T) {
	if _, err := parsePMMS("date,pmms30\n9/24/2026,,\n"); err == nil {
		t.Fatal("a file with no 30 year rate should be an error, not a zero rate")
	}
}

func TestAmortize(t *testing.T) {
	// $250,000 at 6.5% over 30 years is $1,580.17 a month, which is the figure
	// every amortisation table prints.
	got := amortize(250000, 6.5, 30)
	if math.Abs(got-1580.17) > 0.01 {
		t.Fatalf("got %.2f, want 1580.17", got)
	}
	if amortize(0, 6.5, 30) != 0 || amortize(250000, 6.5, 0) != 0 {
		t.Fatal("a loan of nothing or a term of nothing has to be zero, not a NaN")
	}
	// A zero rate is a real case at the edge of the formula, where r(1+r)^n over
	// (1+r)^n - 1 is nought over nought.
	if got := amortize(120000, 0, 10); math.Abs(got-1000) > 0.001 {
		t.Fatalf("zero rate: got %.4f, want 1000", got)
	}
}

// The PMI cliff at 80% is the whole reason a conventional loan wins once there is
// a deposit behind it, so it has to be exact rather than nearly right.
func TestConventionalPMIStopsAt80(t *testing.T) {
	if p := conventionalPMI(80, 740); p != 0 {
		t.Fatalf("at 80%% LTV there is no PMI, got %v", p)
	}
	if p := conventionalPMI(80.01, 740); p == 0 {
		t.Fatal("just over 80% LTV there is PMI")
	}
	if conventionalPMI(96, 620) <= conventionalPMI(96, 780) {
		t.Fatal("a worse score has to cost more, not less")
	}
}

// Above 90% at closing FHA's premium runs for the life of the loan, which is the
// figure people are surprised by and the reason FHA can be dearer than
// conventional at the same rate.
func TestFHAMIPDuration(t *testing.T) {
	_, life := fhaMIP(200000, 96.5)
	if !strings.Contains(life, "life of the loan") {
		t.Fatalf("over 90%% LTV should be life of loan, got %q", life)
	}
	_, eleven := fhaMIP(200000, 89)
	if !strings.Contains(eleven, "eleven years") {
		t.Fatalf("at or under 90%% LTV should be eleven years, got %q", eleven)
	}
	big, _ := fhaMIP(fhaMIPStep+1, 96.5)
	small, _ := fhaMIP(fhaMIPStep-1, 96.5)
	if big <= small {
		t.Fatalf("over the step the premium is higher, got %v against %v", big, small)
	}
}

func TestVAFundingFeeSteps(t *testing.T) {
	cases := []struct {
		down   float64
		first  bool
		exempt bool
		want   float64
	}{
		{0, true, false, 2.15},
		{5, true, false, 1.50},
		{10, true, false, 1.25},
		{0, false, false, 3.30},
		{10, false, false, 1.25},
		{0, true, true, 0},
	}
	for _, c := range cases {
		got, _ := vaFundingFee(c.down, c.first, c.exempt)
		if got != c.want {
			t.Errorf("down %v first %v exempt %v: got %v, want %v", c.down, c.first, c.exempt, got, c.want)
		}
	}
}

// Every government programme finances its upfront fee, so it lands on the loan
// and not on the cash to close. Getting that backwards makes USDA look like it
// needs a deposit.
func TestUpfrontFeesAreFinancedNotPaid(t *testing.T) {
	cfg := testConfig()
	market := Market{Thirty: 6.95, Found: true, Week: "9/17/2026"}
	in := LoanInput{Price: 250000, County: "Alexander", USDAArea: true, USDAAreaChecked: true}

	usda := cfg.Quote("usda", in, market)
	if usda.DownPayment != 0 {
		t.Fatalf("USDA takes nothing down, got %v", usda.DownPayment)
	}
	if usda.UpfrontFee <= 0 {
		t.Fatal("USDA charges an upfront guarantee fee")
	}
	if usda.Loan <= usda.BaseLoan {
		t.Fatal("the upfront fee has to be added to the loan")
	}
	if usda.CashToClose >= 250000*0.04 {
		t.Fatalf("the fee is financed, so it must not be in the cash to close: %v", usda.CashToClose)
	}

	conv := cfg.Quote("conventional", in, market)
	if conv.UpfrontFee != 0 {
		t.Fatalf("conventional charges no upfront fee, got %v", conv.UpfrontFee)
	}
}

// A programme that was not checked and one that was checked and failed must not
// read alike, which is the same rule the whole engine follows about a missing
// measurement.
func TestUSDAUncheckedIsNotTheSameAsIneligible(t *testing.T) {
	cfg := testConfig()
	market := Market{Thirty: 6.95, Found: true}

	unchecked := cfg.Quote("usda", LoanInput{Price: 250000}, market)
	if !unchecked.Eligible {
		t.Fatal("not having checked the map is not a reason to rule USDA out")
	}
	if !hasSubstring(unchecked.Checks, "rural area map was not checked") {
		t.Fatalf("an unchecked map has to say so: %v", unchecked.Checks)
	}

	out := cfg.Quote("usda", LoanInput{Price: 250000, USDAAreaChecked: true, USDAArea: false}, market)
	if out.Eligible {
		t.Fatal("an ineligible area rules USDA out")
	}
	if !hasSubstring(out.Blockers, "ineligible area") {
		t.Fatalf("want the area named as the blocker, got %v", out.Blockers)
	}
}

func TestVANeedsEntitlement(t *testing.T) {
	cfg := testConfig()
	q := cfg.Quote("va", LoanInput{Price: 250000}, Market{Thirty: 6.95, Found: true})
	if q.Eligible {
		t.Fatal("no entitlement on file means VA is not available")
	}
	if q.Total <= 0 {
		t.Fatal("an ineligible programme is still priced, since knowing what it would cost is the point of showing it")
	}

	cfg.Money.VAEligible = true
	if q := cfg.Quote("va", LoanInput{Price: 250000}, Market{Thirty: 6.95, Found: true}); !q.Eligible {
		t.Fatal("with entitlement VA is available")
	}
}

func TestLoanOverTheConformingLimitIsBlocked(t *testing.T) {
	cfg := testConfig()
	q := cfg.Quote("conventional", LoanInput{Price: 2000000}, Market{Thirty: 6.95, Found: true})
	if q.Eligible {
		t.Fatal("a two million dollar loan is over the conforming limit")
	}
}

// The programme's own floor wins over a lower figure, since quoting FHA at 3%
// down would be a payment nobody can get.
func TestDownPaymentFloorIsApplied(t *testing.T) {
	cfg := testConfig()
	q := cfg.Quote("fha", LoanInput{Price: 200000, DownPct: 1}, Market{Thirty: 6.95, Found: true})
	if q.DownPct < 3.5 {
		t.Fatalf("FHA takes 3.5%% at least, got %v", q.DownPct)
	}
	if len(q.Checks) == 0 {
		t.Fatal("moving the deposit up to the floor has to be said out loud")
	}
}

func TestRatesFallBelowConventionalForTheGovernmentProgrammes(t *testing.T) {
	base := 6.95
	if noteRate(base, "fha", 740) >= base {
		t.Fatal("FHA normally goes out under conventional")
	}
	if noteRate(base, "conventional", 620) <= noteRate(base, "conventional", 780) {
		t.Fatal("a worse score costs rate on a conventional loan")
	}
	// Government programmes are not priced on the score the same way, so the
	// adjustment must not reach them.
	if noteRate(base, "va", 620) != noteRate(base, "va", 780) {
		t.Fatal("the conventional credit adjustment must not apply to VA")
	}
}

func TestUSDAIncomeLimitRisesWithHousehold(t *testing.T) {
	cfg := testConfig()
	cfg.Money.USDAIncomeLimit = map[string]float64{"alexander": 100000}
	cfg.Money.HouseholdSize = 4
	four := cfg.usdaIncomeLimit("Alexander County")
	cfg.Money.HouseholdSize = 5
	five := cfg.usdaIncomeLimit("Alexander")
	if five <= four {
		t.Fatalf("a household over four gets a higher cap, got %v against %v", five, four)
	}
	cfg.Money.USDAIncomeLimit = nil
	if cfg.usdaIncomeLimit("Alexander") != 0 {
		t.Fatal("with nothing configured the limit is unknown, not zero dollars")
	}
}

// The map publishes ineligible areas, so no match means USDA will lend. Reading
// it the other way round would call the whole county ineligible.
func TestUSDALookupReadsTheLayerTheRightWayRound(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"count": count})
	}))
	defer srv.Close()
	old := usdaLayer
	usdaLayer = srv.URL
	defer func() { usdaLayer = old }()

	db := testDB(t)
	u := NewUSDA(db)

	count = 0
	got, err := u.Lookup(context.Background(), 35.92, -81.17)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Eligible || !got.Measured {
		t.Fatalf("no ineligible polygon means eligible, got %+v", got)
	}

	count = 1
	got, err = u.Lookup(context.Background(), 35.227, -80.843)
	if err != nil {
		t.Fatal(err)
	}
	if got.Eligible {
		t.Fatalf("inside an ineligible polygon means not eligible, got %+v", got)
	}
}

func TestUSDALookupCaches(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		json.NewEncoder(w).Encode(map[string]any{"count": 0})
	}))
	defer srv.Close()
	old := usdaLayer
	usdaLayer = srv.URL
	defer func() { usdaLayer = old }()

	u := NewUSDA(testDB(t))
	for i := 0; i < 3; i++ {
		if _, err := u.Lookup(context.Background(), 35.92, -81.17); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Fatalf("the same point should be asked once, it was asked %d times", hits)
	}
}

// A distance of not-found is -1 rather than zero, because JSON cannot carry an
// infinity and a zero reads as standing in a creek.
func TestNotFoundDistancesReadAsNotFound(t *testing.T) {
	if got := feetOrNone(notFound); !strings.Contains(got, "none found") {
		t.Fatalf("got %q", got)
	}
	if got := feetOrNone(0); strings.Contains(got, "none") {
		t.Fatalf("nought feet is a measurement, got %q", got)
	}
	if got := feetOrNone(10560); !strings.Contains(got, "miles") {
		t.Fatalf("over a mile reads in miles, got %q", got)
	}
}

// A lookup that never ran must never read as a measurement of nothing.
func TestUnmeasuredSectionsSaySo(t *testing.T) {
	r := &Report{Address: "1 Test Rd", Flood: newFloodResult(), Road: newRoadResult()}
	if got := r.FloodPart()["flood"]; got == nil || !strings.Contains(got.(string), "not measured") {
		t.Fatalf("an unmeasured flood lookup has to say so, got %v", got)
	}
	if got := r.RoadPart()["road"]; got == nil || !strings.Contains(got.(string), "not measured") {
		t.Fatalf("an unmeasured road lookup has to say so, got %v", got)
	}
	if got := r.floodLine(); got != "not measured" {
		t.Fatalf("got %q", got)
	}
}

func TestEveryAspectAnswers(t *testing.T) {
	r := &Report{Address: "1 Test Rd", County: "Alexander", Price: 250000, Drives: map[string]Leg{}}
	for _, name := range Aspects {
		got := r.Aspect(name)
		if got == nil {
			t.Fatalf("aspect %q returned nothing", name)
		}
		if m, ok := got.(map[string]any); ok {
			if _, bad := m["error"]; bad && name != "cost" {
				t.Fatalf("aspect %q errored: %v", name, m["error"])
			}
		}
	}
}

// A section nobody has heard of comes back with the list, since a model that
// invents a name has still said what it wants.
func TestUnknownAspectNamesTheOnesThereAre(t *testing.T) {
	m, ok := (&Report{}).Aspect("vibes").(map[string]any)
	if !ok {
		t.Fatal("want a map back")
	}
	if m["sections"] == nil {
		t.Fatalf("an unknown section has to list the real ones, got %v", m)
	}
}

func TestAddressKeyNormalizes(t *testing.T) {
	a := addressKey("402 Sample Road", "27055")
	b := addressKey("402 sample rd.", "27055-1234")
	if a != b {
		t.Fatalf("%q and %q should be the same house", a, b)
	}
	if addressKey("402 Sample Rd", "27055") == addressKey("404 Sample Rd", "27055") {
		t.Fatal("different house numbers are different houses")
	}
}

func TestTitleAddressKeepsAbbreviatedDirectionsShouted(t *testing.T) {
	if got := titleAddress("402 SAMPLE RD"); got != "402 Sample Rd" {
		t.Fatalf("got %q", got)
	}
	if got := titleAddress("358 N MAIN AVE"); got != "358 N Main Ave" {
		t.Fatalf("got %q", got)
	}
	if got := titleAddress("358 WEST MAIN AVE"); got != "358 West Main Ave" {
		t.Fatalf("a spelled out direction is a word, got %q", got)
	}
}

func TestReportCacheRoundTrip(t *testing.T) {
	db := testDB(t)
	e, err := NewEngine(db, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	want := &Report{Address: "1 Test Rd", County: "Alexander", Lat: 35.9, Lon: -81.1, Complete: true}
	if err := e.save(ctx, "k", want); err != nil {
		t.Fatal(err)
	}
	got, ok := e.cached(ctx, "k")
	if !ok {
		t.Fatal("a report just written should come back")
	}
	if got.Address != want.Address || got.County != want.County || !got.Complete {
		t.Fatalf("got %+v", got)
	}

	// Past its life it is gone rather than stale, since a month old drive time is
	// fine and a month old anything else is not worth the argument.
	if _, err := db.Exec(`UPDATE reports SET built_at = ?`, time.Now().Add(-reportTTL-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.cached(ctx, "k"); ok {
		t.Fatal("an expired report should not come back")
	}
}

// The price is the caller's and moves between two questions about one house, so
// it is worked out over the cached facts rather than cached with them.
func TestPricingACachedReportDoesNotNeedALookup(t *testing.T) {
	e, err := NewEngine(testDB(t), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	r := &Report{Address: "1 Test Rd", County: "Alexander",
		USDA: USDAArea{Eligible: true, Measured: true}}

	e.priceReport(context.Background(), r, Options{Price: 250000})
	if len(r.Quotes) != len(LoanTypes) {
		t.Fatalf("want a quote per programme, got %d", len(r.Quotes))
	}
	for _, q := range r.Quotes {
		if q.Type == "usda" && !q.Eligible {
			t.Fatal("the cached area check has to reach the quote")
		}
	}
}

func TestConfigFallsBackWithoutAFile(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir() + "/nothing.json")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Defaulted {
		t.Fatal("a missing config is defaults, and has to say so")
	}
	if cfg.HasOrigin() {
		t.Fatal("there is no real origin in the defaults, since a fake one computes drives to the wrong place")
	}
}

func TestBadConfigIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/property.json"
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("a malformed config is an error, since falling back silently hides a typo behind plausible numbers")
	}
}

func TestCountyTaxRatePrefersTheCityRate(t *testing.T) {
	m := testConfig().Money
	m.CountyTaxPer100["alexander:taylorsville"] = 1.29
	rate, src := m.rateFor("Alexander County", "Taylorsville")
	if rate != 1.29 || !strings.Contains(src, "Taylorsville") {
		t.Fatalf("a house inside town limits pays both, got %v from %q", rate, src)
	}
	if rate, _ := m.rateFor("Alexander County", ""); rate != 0.79 {
		t.Fatalf("outside town it is the county rate, got %v", rate)
	}
	if _, src := m.rateFor("Nowhere", ""); !strings.Contains(src, "default") {
		t.Fatalf("an unknown county falls back and says so, got %q", src)
	}
}

func hasSubstring(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// A gap bounds the rate and only a ceiling bounds the total. This is the fence
// against a model spelling one street name six ways inside a single turn.
func TestColdLookupsAreCapped(t *testing.T) {
	e, err := NewEngine(testDB(t), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for i := 0; i < coldPerHour; i++ {
		if err := e.spend(ctx, fmt.Sprintf("house-%d", i)); err != nil {
			t.Fatalf("address %d should be allowed: %v", i, err)
		}
	}

	err = e.spend(ctx, "one-too-many")
	var spent ErrSpent
	if !errors.As(err, &spent) {
		t.Fatalf("want the ceiling, got %v", err)
	}
	if spent.Window != "hour" || spent.Ceil != coldPerHour {
		t.Fatalf("got %+v", spent)
	}
	if !strings.Contains(spent.Error(), "already answers") && !strings.Contains(spent.Error(), "still answers") {
		t.Fatalf("a refusal has to say what still works: %q", spent.Error())
	}
}

// An hour rolling off has to hand the allowance back, or the first busy morning
// turns the tool off for good.
func TestTheHourlyCeilingRollsOff(t *testing.T) {
	db := testDB(t)
	e, err := NewEngine(db, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for i := 0; i < coldPerHour; i++ {
		if err := e.spend(ctx, fmt.Sprintf("house-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE assessments SET started_at = ?`,
		time.Now().Add(-90*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := e.spend(ctx, "next-hour"); err != nil {
		t.Fatalf("an hour later there should be allowance again: %v", err)
	}
}

func TestTheDailyCeilingHoldsWhenTheHoursRollOff(t *testing.T) {
	db := testDB(t)
	e, err := NewEngine(db, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Spread over the day, so the hourly window never bites and only the daily
	// one is doing any work.
	for i := 0; i < coldPerDay; i++ {
		if _, err := db.Exec(`INSERT INTO assessments (key, started_at) VALUES (?,?)`,
			fmt.Sprintf("h%d", i), time.Now().Add(-time.Duration(i+2)*time.Minute*50).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	err = e.spend(ctx, "one-too-many")
	var spent ErrSpent
	if !errors.As(err, &spent) || spent.Window != "day" {
		t.Fatalf("want the daily ceiling, got %v", err)
	}
}

// The ceiling is the cost of going out, so an address already in the cache must
// not pay it. A conversation about one house is a dozen questions.
func TestACachedAddressCostsNothingAgainstTheCeiling(t *testing.T) {
	db := testDB(t)
	e, err := NewEngine(db, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < coldPerHour; i++ {
		if err := e.spend(ctx, fmt.Sprintf("house-%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	if err := e.save(ctx, "known", &Report{Address: "1 Test Rd", Complete: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.cached(ctx, "known"); !ok {
		t.Fatal("a cached report has to come back with the ceiling spent, since it costs nobody anything")
	}
	hour, day := e.Spend(ctx)
	if hour != 0 || day != coldPerDay-coldPerHour {
		t.Fatalf("got %d left this hour and %d today", hour, day)
	}
}

// A restart handing back a fresh allowance is how a limit becomes a suggestion,
// and a deploy is a restart.
func TestTheCeilingSurvivesARestart(t *testing.T) {
	db := testDB(t)
	e, err := NewEngine(db, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < coldPerHour; i++ {
		if err := e.spend(ctx, fmt.Sprintf("house-%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	again, err := NewEngine(db, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := again.spend(ctx, "after-the-deploy"); err == nil {
		t.Fatal("a new process must not hand back a spent allowance")
	}
}

// Every one of these is somebody else's free server and several are one machine
// in a county building, so a budget that drifts up is worth failing a test over.
func TestEveryEndpointKeepsAModestBudget(t *testing.T) {
	db := testDB(t)
	if _, err := NewEngine(db, testConfig()); err != nil {
		t.Fatal(err)
	}
	// One cold address costs about 38 requests across these, and the ceiling is
	// six an hour, so nothing here needs a four figure allowance.
	for _, g := range []*Guard{
		NewGeocoder(db).guard, NewFlood(db, nil).fema, NewFlood(db, nil).usgs,
		NewTerrain(db).guard, NewParcels(db).guard, NewUSDA(db).guard,
		NewCensus(db).guard, NewFacilities(db, nil).guard, NewRouter(db).guard,
	} {
		if g.budget > 150 {
			t.Errorf("%s allows %d requests per window, which is more than this ever needs", g.name, g.budget)
		}
		if g.minInterval < time.Second {
			t.Errorf("%s paces at %v, which is faster than a free public service deserves", g.name, g.minInterval)
		}
	}
}

// A cached payload's shape is part of its key. An older row decoded into a newer
// struct comes back as zeroes, and a zero here is a claim about a county rather
// than a stale cache, which is why the kind is bumped whenever a field is added.
func TestAnOlderAreaCacheRowIsNotReadBack(t *testing.T) {
	db := testDB(t)
	// What house wrote under the old kind: population and composition only, so
	// every figure this engine added would come back nought.
	if _, err := db.Exec(
		`INSERT INTO lookups (kind, key, payload, fetched_at) VALUES ('acs',?,?,?)`,
		"north carolina|alexander county",
		`{"name":"Alexander County","population":36440,"found":true}`,
		time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM lookups WHERE kind = ?`, acsCacheKind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the old row must not be visible under the current kind")
	}
	if acsCacheKind == "acs" {
		t.Fatal("the kind has to change when the struct gains a field")
	}
}

// FEMA leaves plenty of rural parcels off the panel entirely, and reading that as
// zone X says somebody surveyed it and found minimal hazard.
func TestAnUnmappedPointIsNotZoneX(t *testing.T) {
	r := &Report{Flood: FloodResult{Measured: true, SFHAFeet: notFound}}
	if got := r.floodLine(); strings.Contains(got, "zone X") {
		t.Fatalf("an unmapped point must not read as zone X: %q", got)
	}
	if got := r.floodLine(); !strings.Contains(got, "no zone") {
		t.Fatalf("it has to say FEMA maps nothing here: %q", got)
	}
	mapped := &Report{Flood: FloodResult{Measured: true, Zone: "X", SFHAFeet: 1200}}
	if got := mapped.floodLine(); !strings.Contains(got, "zone X") {
		t.Fatalf("a real zone X still reads as one: %q", got)
	}
}

// The defaults are what an endpoint gets when somebody adds one and does not
// think about it, so they have to be the cautious numbers.
func TestGuardDefaultsAreCautious(t *testing.T) {
	g := NewGuard(testDB(t), "something-new", GuardOpts{})
	if g.budget > 60 {
		t.Errorf("an unconsidered endpoint gets %d requests a window, which is too many", g.budget)
	}
	if g.minInterval < time.Second {
		t.Errorf("an unconsidered endpoint paces at %v, which is faster than a free service deserves", g.minInterval)
	}
	if g.window > time.Hour {
		t.Errorf("the default window is %v, so the budget means less than it looks", g.window)
	}
}

// An assessment held up against an asking price is only worth anything when the
// record is for this house. Both of these are common and both produce a number
// that looks precise enough for a model to repeat as a finding.
func TestAWrongParcelRecordIsNotComparedToThePrice(t *testing.T) {
	// The geocoder interpolates along the street centreline, so the nearest
	// parcel is sometimes next door.
	nextDoor := &Report{
		Address: "2953 Link Dr", Price: 289900,
		Parcel: ParcelResult{Found: true, Address: "2949 Link Dr", MarketValue: 210000,
			LandValue: 40000, BuildValue: 170000},
		Value: ValueEstimate{Found: true, Estimate: 231000, Low: 210000, High: 252000, BaseYear: 2025},
	}
	land := nextDoor.LandPart()
	if _, bad := land["asking_against_that"]; bad {
		t.Fatal("a parcel next door must not be compared to this price")
	}
	if why, _ := land["do_not_compare_the_price_to_that"].(string); !strings.Contains(why, "2949") {
		t.Fatalf("it has to name the parcel it actually matched: %q", why)
	}

	// A tax roll carrying land and no buildings is a vacant lot record against a
	// house that is standing on it, which is where the $11,100 assessment on a
	// $289,900 house came from.
	vacant := &Report{
		Address: "2953 Link Dr", Price: 289900,
		Parcel: ParcelResult{Found: true, Address: "2953 Link Dr", MarketValue: 11100,
			LandValue: 11100, BuildValue: 0},
		Value: ValueEstimate{Found: true, Estimate: 11788, Low: 10000, High: 13000, BaseYear: 2025},
	}
	if _, bad := vacant.LandPart()["asking_against_that"]; bad {
		t.Fatal("a vacant lot record must not be compared to the price of a house")
	}
	if why, _ := vacant.LandPart()["do_not_compare_the_price_to_that"].(string); !strings.Contains(why, "vacant lot") {
		t.Fatalf("it has to say the record is land only: %q", why)
	}
	if got := vacant.Summary()["what_it_is_worth"].(string); !strings.Contains(got, "vacant lot") {
		t.Fatalf("the summary line carries the caveat too: %q", got)
	}

	// A record that is for this house and has buildings on it still gets the
	// comparison, which is the whole point of having it.
	good := &Report{
		Address: "2953 Link Dr", Price: 289900,
		Parcel: ParcelResult{Found: true, Address: "2953 Link Dr", MarketValue: 240000,
			LandValue: 40000, BuildValue: 200000},
		Value: ValueEstimate{Found: true, Estimate: 264000, Low: 240000, High: 288000, BaseYear: 2025},
	}
	if _, ok := good.LandPart()["asking_against_that"]; !ok {
		t.Fatal("a sound record is still compared to the price")
	}
}

// A question naming somebody in the house has to find them in the answer. Asked
// about a household member's work against a flat list of place names, the model
// had nothing to match the name on and looked the name up in the encyclopedia as
// a stranger instead.
func TestDrivesAreGroupedByWhoseTheyAre(t *testing.T) {
	r := &Report{
		Address: "1 Test Rd",
		Drives: map[string]Leg{
			"college": {Minutes: 26, Miles: 18, Who: "A Person", Name: "A Community College", Kind: "school"},
			"clinic":  {Minutes: 31, Miles: 20, Who: "A Person", Name: "A Medical Center", Kind: "employer"},
			"unnamed": {Minutes: 9, Miles: 4, Name: "A Shop"},
		},
		Household: []Person{{Name: "A Person", Role: "a CNA", Note: "work nearby means hospitals"}},
		Work:      []Facility{{Name: "A Nursing Home", Label: "nursing or assisted living"}},
	}

	m := r.DrivesPart()
	byWho, ok := m["drives_from_this_house"].(map[string][]string)
	if !ok {
		t.Fatalf("want the drives grouped by person, got %T", m["drives_from_this_house"])
	}
	if len(byWho["A Person"]) != 2 {
		t.Fatalf("both of that person's drives belong under their name: %v", byWho)
	}
	if len(byWho["anybody in the house"]) != 1 {
		t.Fatalf("a drive with nobody against it still has to appear: %v", byWho)
	}

	who, ok := m["who_lives_here"].(map[string]string)
	if !ok || who["A Person"] == "" {
		t.Fatalf("the roster has to be in the answer so a name resolves: %v", m["who_lives_here"])
	}
	if _, ok := m["work_within_reach"]; !ok {
		t.Fatal("where the work is has to be in the drives section")
	}
}

// Every section named in the tool's schema has to be one Aspect actually answers,
// or the model is being pointed at something that does not exist.
func TestTheSectionsTheModelIsOfferedAllExist(t *testing.T) {
	for _, name := range Aspects {
		m, ok := (&Report{Address: "1 Test Rd", Drives: map[string]Leg{}}).Aspect(name).(map[string]any)
		if !ok {
			continue // everything returns the whole report, which is fine
		}
		if _, missing := m["sections"]; missing {
			t.Errorf("section %q is offered but falls through to the unknown branch", name)
		}
	}
}

// A cached report that gains a field decodes with it blank, which is the same
// trap the lookup kinds carry a number for.
func TestAnOlderCachedReportIsRebuiltNotAnsweredFrom(t *testing.T) {
	db := testDB(t)
	e, err := NewEngine(db, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := db.Exec(
		`INSERT INTO reports (key, address, lat, lon, payload, built_at) VALUES (?,?,?,?,?,?)`,
		"old", "1 Test Rd", 35.9, -81.1,
		`{"address":"1 Test Rd","complete":true}`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.cached(ctx, "old"); ok {
		t.Fatal("a report from before the drives knew whose they are must not be answered from")
	}

	if err := e.save(ctx, "new", &Report{Address: "1 Test Rd", Version: reportVersion}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.cached(ctx, "new"); !ok {
		t.Fatal("a current one still comes back")
	}
}
