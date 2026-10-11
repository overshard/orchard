package main

import (
	"strings"
	"testing"
	"time"
)

func TestGlanceLevels(t *testing.T) {
	et := easternTime()
	saturday := time.Date(2026, 10, 10, 11, 0, 0, 0, et)

	var st State
	st.Market.Cards = []Card{{Key: "sp500", Label: "S&P 500", Price: "7,859.75", Percent: "+0.51%"}, {Key: "vix", Label: "VIX", Price: "14.84", Percent: "-3.70%"}}
	st.Briefs.News.Read = &Read{Level: "steady", Label: "STEADY", Text: "A day."}
	for _, s := range glanceSlots(st, nil, saturday) {
		if s.level != levelQuiet {
			t.Errorf("an ordinary saturday has %s at %s because %v", s.label, s.level, s.why)
		}
	}

	st.Market.Cards[0].Percent = "-2.30%"
	st.Briefs.News.Read.Level = "major"
	st.Alerts = []Alert{{Event: "Flood Watch", Severity: "Moderate"}}
	st.HNPulse = Pulse{Level: "hot", Label: "BLOWING UP"}
	want := map[string]string{"MARKETS": levelAct, "NEWS": levelQuiet, "TECH": levelWatch, "WEATHER": levelWatch, "PLAY": levelQuiet}
	for _, s := range glanceSlots(st, nil, saturday) {
		if s.level != want[s.label] {
			t.Errorf("%s is %s, want %s", s.label, s.level, want[s.label])
		}
	}

	st.Alerts = []Alert{{Event: "Tornado Warning", Severity: "Severe"}}
	if s := glanceWeather(st); s.level != levelAct {
		t.Errorf("a tornado warning is only %s", s.level)
	}
}

// The day before a Fed decision is worth a look whatever the tape does.
func TestGlanceFlagsTheFedTheDayBefore(t *testing.T) {
	var st State
	g := glanceMarkets(st, nil, time.Date(2026, 10, 27, 17, 0, 0, 0, easternTime()))
	if g.level != levelWatch || !strings.Contains(strings.Join(g.why, " "), "Wednesday") {
		t.Errorf("the evening before the 28 October decision: %s %v", g.level, g.why)
	}
}

func TestUnsupportedFigures(t *testing.T) {
	facts := "S&P 500 7,859.75, +0.51% on the day\nHurricane Simon reached Category 4"
	if bad := unsupported("The S&P 500 rose 0.51% to 7,859.75 as Simon hit Category 4.", facts); bad != "" {
		t.Errorf("flagged %q that the facts carry", bad)
	}
	if bad := unsupported("The S&P 500 rose 1.2% this week.", facts); bad != "1.2" {
		t.Errorf("let an invented figure through, got %q", bad)
	}
}

func TestEmbolden(t *testing.T) {
	facts := "Hurricane Simon reached Category 4. Christa Pike left the hospital."
	got := embolden("Nothing needs you. Hurricane Simon is at Category 4 and a big rally, Christa Pike too.",
		[]string{"a big rally", "Hurricane Simon", "Christa Pike", "Category 4"}, facts)
	if got != "Nothing needs you. **Hurricane Simon** is at Category 4 and a big rally, **Christa Pike** too." {
		t.Errorf("got %q", got)
	}
}

func TestMarkedEscapes(t *testing.T) {
	if got := string(marked("A <b> tag and **S&P 500** up, and a stray ** left")); got != "A &lt;b&gt; tag and <b>S&amp;P 500</b> up, and a stray ** left" {
		t.Errorf("got %q", got)
	}
}
