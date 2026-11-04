package main

import (
	"errors"
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

// The county writes November 1st as 11/1/2026, and keeps a day it hasn't
// forecast yet as a hidden column of zeros.
func TestParsePollenUnpaddedAndHidden(t *testing.T) {
	page := `<table id="Forecast"><thead><tr><td class="empty"></td>
<th scope="col"> Sunday<br/>11/1/2026 </th>
<th scope="col" style="display: none;"> Tuesday<br/>11/3/2026 </th></tr></thead>
<tbody><tr><th scope="row">Trees </th><td> 1 </td><td style="display: none;"> 0 </td></tr>
<tr><th scope="row">Grasses </th><td> 2 </td><td style="display: none;"> 0 </td></tr>
<tr><th scope="row">Weeds </th><td> 3 </td><td style="display: none;"> 0 </td></tr></tbody></table>`
	day := func(d int) time.Time { return time.Date(2026, 11, d, 12, 0, 0, 0, time.UTC) }

	got, err := parsePollen(page, day(1))
	if err != nil || got["Trees"] != 1 || got["Weeds"] != 3 {
		t.Fatalf("11/1 = %v, %v, want the forecast", got, err)
	}
	if got, err := parsePollen(page, day(3)); err == nil || errors.Is(err, errPollenOff) {
		t.Fatalf("hidden 11/3 = %v, %v, want no reading", got, err)
	}

	last := page + `<p>TODAY IS THE LAST DAY OF OUR CY2026 POLLEN MONITORING AND REPORTING SEASON.</p>`
	if _, err := parsePollen(last, day(1)); err != nil {
		t.Fatalf("the last report's forecast should still read, got %v", err)
	}
	if _, err := parsePollen(last, day(3)); !errors.Is(err, errPollenOff) {
		t.Fatalf("after the last report = %v, want off season", err)
	}

	stale := `<table id="Graph"><caption>Friday, October 30, 2026</caption></table>` + page
	if _, err := parsePollen(stale, day(5)); errors.Is(err, errPollenOff) {
		t.Fatal("a report six days old is not off season")
	}
	if _, err := parsePollen(stale, day(9)); !errors.Is(err, errPollenOff) {
		t.Fatalf("a report ten days old = %v, want off season", err)
	}
}
