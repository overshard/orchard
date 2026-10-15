package main

import (
	"path/filepath"
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

// An inch and a third of rain over nine hours with no alert out isn't a quiet
// day.
func TestGlanceWeatherWatchesRainAndWind(t *testing.T) {
	var st State
	st.Weather.Days = []WeatherDay{{Rain: 0.2, RainHours: 3, Gusts: 12}, {Rain: 0.1, RainHours: 2, Gusts: 15}}
	if g := glanceWeather(st); g.level != levelQuiet {
		t.Errorf("a showery day is %s because %v", g.level, g.why)
	}
	st.Weather.Days[0] = WeatherDay{Rain: 1.331, RainHours: 9, Gusts: 22.8}
	if g := glanceWeather(st); g.level != levelWatch || !strings.Contains(g.text(), "1.3 in over 9 hours") {
		t.Errorf("an inch and a third of rain is %s: %s", g.level, g.text())
	}
	st.Weather.Days[0] = WeatherDay{Rain: 0.6, RainHours: 12, Gusts: 10}
	if g := glanceWeather(st); g.level != levelWatch {
		t.Errorf("rain all day is %s", g.level)
	}
	st.Weather.Days[0] = WeatherDay{}
	st.Weather.Days[1] = WeatherDay{Gusts: 34}
	if g := glanceWeather(st); g.level != levelWatch || !strings.Contains(strings.Join(g.why, " "), "tomorrow") {
		t.Errorf("gusts to 34 tomorrow: %s %v", g.level, g.why)
	}
}

func TestGlancePlayWatchesANewTop(t *testing.T) {
	now := time.Date(2026, 10, 11, 7, 0, 0, 0, easternTime())
	var st State
	st.Steam = []Game{{Name: "Counter-Strike 2", Released: "2012-08-21"}, {Name: "Dota 2", Released: "2013-07-09"}}
	if g := glancePlay(st, now); g.level != levelQuiet {
		t.Errorf("the usual chart is %s because %v", g.level, g.why)
	}
	st.Steam = append([]Game{{Name: "AION 2", Released: "2026-10-05"}}, st.Steam...)
	if g := glancePlay(st, now); g.level != levelWatch || !strings.Contains(g.text(), "released October 5") {
		t.Errorf("a week old game at No. 1 is %s: %s", g.level, g.text())
	}
	st.Steam[0].Released = "2025-01-01"
	st.SteamUsual = "Counter-Strike 2"
	if g := glancePlay(st, now); g.level != levelWatch || !strings.Contains(strings.Join(g.why, " "), "from Counter-Strike 2") {
		t.Errorf("an old game pushing the usual off No. 1 is %s %v", g.level, g.why)
	}
}

func TestSteamChartUsual(t *testing.T) {
	c := openSteamChart(t.TempDir())
	cs, aion := Game{Name: "Counter-Strike 2", URL: "cs"}, Game{Name: "AION 2", URL: "aion"}
	day := time.Date(2026, 10, 1, 12, 0, 0, 0, easternTime())
	if got := c.record([]Game{cs}, day); got != "" {
		t.Errorf("one day on file already has a usual: %q", got)
	}
	for i := 1; i < 5; i++ {
		c.record([]Game{cs}, day.AddDate(0, 0, i))
	}
	got := c.record([]Game{aion}, day.AddDate(0, 0, 5))
	if got != "Counter-Strike 2" {
		t.Errorf("usual is %q", got)
	}
	if again := openSteamChart(filepath.Dir(c.path)); again.usual(day.AddDate(0, 0, 5)) != "Counter-Strike 2" {
		t.Error("the chart didn't survive a restart")
	}
}

func TestReleaseDay(t *testing.T) {
	for raw, want := range map[string]string{"Oct 5, 2026": "2026-10-05", "5 Oct, 2026": "2026-10-05", "Q4 2026": "", "Coming soon": ""} {
		if got := releaseDay(raw); got != want {
			t.Errorf("%q read as %q", raw, got)
		}
	}
}

// A bad CPI print can take the Nasdaq down far more than the S&P, and that's a
// day to watch on its own.
func TestGlanceWatchesTheNasdaq(t *testing.T) {
	now := time.Date(2026, 10, 14, 11, 0, 0, 0, easternTime())
	var st State
	st.Market.Cards = []Card{{Key: "sp500", Label: "S&P 500", Price: "7,780.00", Percent: "-0.60%"}, {Key: "nasdaq", Label: "Nasdaq", Price: "30,100.00", Percent: "-1.60%"}}
	if g := glanceMarkets(st, nil, now); g.level != levelWatch || !strings.Contains(strings.Join(g.why, " "), "Nasdaq") {
		t.Errorf("the Nasdaq down 1.6%% is %s %v", g.level, g.why)
	}
	st.Market.Cards[1].Percent = "-2.73%"
	if g := glanceMarkets(st, nil, now); g.level != levelAct {
		t.Errorf("the Nasdaq down 2.73%% is %s %v", g.level, g.why)
	}
}

// Handed the points, the model writes how fast a thread grew and nothing about
// what it is.
func TestGlanceTechNamesTheThread(t *testing.T) {
	var st State
	st.HNPulse = Pulse{Level: "hot", Label: "BLOWING UP", Text: "No. 1 hit 807 points in 5 hours.", Thread: "Frontier labs are skipping security basics"}
	st.HN = []Story{{Title: "Frontier labs are skipping security basics", Points: 807}}
	g := glanceTech(st)
	if strings.Contains(g.text(), "807") {
		t.Errorf("points reached the model: %s", g.text())
	}
	if !strings.Contains(g.text(), "the thread is Frontier labs") || !strings.HasPrefix(g.fallback, "Frontier labs") {
		t.Errorf("the hot thread isn't named: %s / %s", g.text(), g.fallback)
	}
	if bad := unsupported("Frontier labs thread hit 807 points on Hacker News.", g.text()); bad != "807" {
		t.Errorf("a line quoting points got through, %q", bad)
	}
}

func TestGlanceVerdict(t *testing.T) {
	for _, s := range []string{"Nothing needs him. Stocks fell.", "Nothing needs attention, stocks fell.", "Stocks fell, which needs your attention."} {
		if !talksToHim.MatchString(s) {
			t.Errorf("%q got through", s)
		}
	}
	if talksToHim.MatchString("A bad CPI print took the Nasdaq down 2.03%, and AION 2 still tops Steam.") {
		t.Error("an ordinary summary was caught")
	}
}
