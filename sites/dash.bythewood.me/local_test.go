package main

import "testing"

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
