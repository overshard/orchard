package main

import (
	"os"
	"testing"
	"time"
)

func TestAlertHeadlineDropsTheTemplate(t *testing.T) {
	generated := "Severe Thunderstorm Watch issued September 4 at 2:54PM EDT until September 4 at 10:00PM EDT by NWS Blacksburg VA"
	if got := alertHeadline("Severe Thunderstorm Watch", generated); got != "" {
		t.Fatalf("want empty for the generated headline, got %q", got)
	}

	written := "Flooding is expected along the Yadkin River by NWS Blacksburg VA"
	want := "Flooding is expected along the Yadkin River"
	if got := alertHeadline("Flood Warning", written); got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestAlertOffice(t *testing.T) {
	in := "Severe Thunderstorm Watch issued September 4 at 2:54PM EDT until September 4 at 10:00PM EDT by NWS Blacksburg VA"
	if got := alertOffice(in); got != "BLACKSBURG VA" {
		t.Fatalf("want BLACKSBURG VA, got %q", got)
	}
	if got := alertOffice("no office here"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestAlertAreaLeadsWithHome(t *testing.T) {
	in := "Alleghany, NC; Ashe, NC; Caswell, NC; Rockingham, NC; Stokes, NC; Surry, NC; Watauga, NC; Wilkes, NC; Yadkin, NC"
	if got := alertArea(in); got != "YADKIN +8" {
		t.Fatalf("want YADKIN +8, got %q", got)
	}

	// Nowhere near home, so the first name stands.
	if got := alertArea("Buncombe, NC; Madison, NC"); got != "BUNCOMBE +1" {
		t.Fatalf("want BUNCOMBE +1, got %q", got)
	}
	if got := alertArea("Yadkin, NC"); got != "YADKIN" {
		t.Fatalf("want YADKIN, got %q", got)
	}
	if got := alertArea(""); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

// The fixture is the county's page from Monday 10-26, with weeds at 7 posted and
// a forecast of 1, 1 and 10 for Tuesday.
func TestParsePollen(t *testing.T) {
	raw, err := os.ReadFile("testdata/pollen.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }

	posted, err := parsePollen(page, day(26))
	if err != nil || posted["Weeds"] != 7 || posted["Trees"] != 0 {
		t.Fatalf("Monday = %v, %v, want the posted count", posted, err)
	}
	ahead, err := parsePollen(page, day(27))
	if err != nil || ahead["Weeds"] != 10 || ahead["Trees"] != 1 {
		t.Fatalf("Tuesday = %v, %v, want the forecast", ahead, err)
	}
	if _, err := parsePollen(page, day(31)); err == nil {
		t.Fatal("a day past the forecast should have no reading")
	}
	if _, err := parsePollen("<html>redesigned</html>", day(26)); err == nil {
		t.Fatal("a page without the tables should fail")
	}

	p := pollenSummary(ahead)
	if p.Pollen != "10" || p.PollenState != "LOW" || p.PollenTop != "WEEDS · GRASSES" || !p.PollenKnown {
		t.Fatalf("summary = %+v", p)
	}
}
