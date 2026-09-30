package main

import "testing"

func TestACountdownIsReadOffTheQuestion(t *testing.T) {
	for q, want := range map[string]string{
		"how long till 10am":                  "10am",
		"How many days until Christmas?":      "Christmas",
		"how much longer until friday at 5pm": "friday at 5pm",
		"how long to cook a turkey":           "cook a turkey",
	} {
		m := untilAsk.FindStringSubmatch(q)
		if m == nil || m[1] != want {
			t.Errorf("untilAsk(%q) = %v, want %q", q, m, want)
		}
	}
	if untilAsk.MatchString("how long is the long walk") {
		t.Error("a question about a length read as a countdown")
	}
}

func TestAWordToDefineIsFound(t *testing.T) {
	for q, want := range map[string]string{
		"define program":                                        "program",
		`define "wednesday"`:                                    "wednesday",
		"what does ad hoc mean?":                                "ad hoc",
		"what is the meaning of ennui":                          "ennui",
		"define the difference between these two things please": "",
		"what does it mean":                                     "",
	} {
		if got := definedWord(q); got != want {
			t.Errorf("definedWord(%q) = %q, want %q", q, got, want)
		}
	}
}

func TestASiteNameIsSearchedRatherThanLookedUp(t *testing.T) {
	for q, want := range map[string]bool{
		"what is america.gov":          true,
		"what's bythewood.me":          true,
		"what is dungeon crawler carl": false,
		"what is node.js":              false,
	} {
		if got := siteName.MatchString(subjectOf(q)); got != want {
			t.Errorf("%q site = %v, want %v", q, got, want)
		}
	}
}

func TestADraftHasToGiveTheTimeLeft(t *testing.T) {
	for draft, want := range map[string]bool{
		"8 minutes.":                     false,
		"1 hour 2 minutes.":              true,
		"About an hour and 2 minutes.":   true,
		"It's 1 hr 2 mins from now.":     true,
		"About 33 minutes, it's 8:27 AM": false,
	} {
		if got := saysTimeLeft(draft, "1 hour 2 minutes"); got != want {
			t.Errorf("saysTimeLeft(%q) = %v, want %v", draft, got, want)
		}
	}
}
