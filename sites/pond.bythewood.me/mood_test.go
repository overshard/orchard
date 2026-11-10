package main

import (
	"strings"
	"testing"
)

func TestMoodOrder(t *testing.T) {
	rain := dayView{Rain: true}
	cases := []struct {
		r    reading
		v    dayView
		want string
	}{
		{reading{Major: true, HasSP: true, SP: 2}, rain, moodSombre},
		{reading{HasSP: true, SP: -3}, rain, moodGloom3},
		{reading{HasSP: true, SP: -2}, rain, moodRain},
		{reading{HasSP: true, SP: -2}, dayView{}, moodGloom2},
		{reading{HasHL: true, Low: 28, High: 50}, dayView{}, moodFrost},
		{reading{HasHL: true, Low: 70, High: 95}, dayView{}, moodHeat},
		{reading{VIX: 25, HasSP: true, SP: -0.6}, dayView{}, moodJitters},
		{reading{HasSP: true, SP: -0.6}, dayView{}, moodGloom1},
		{reading{Pollen: 3, HasSP: true, SP: 1}, dayView{}, moodPollen},
		{reading{Tmr: &tomorrow{Rain: 70}, HasSP: true, SP: 1}, dayView{}, moodShowers},
		{reading{HasSP: true, SP: 1}, dayView{}, moodBright},
		{reading{}, dayView{}, moodPlain},
	}
	for _, c := range cases {
		if got := moodOf(&c.r, c.v); got != c.want {
			t.Errorf("%+v: got %s, want %s", c.r, got, c.want)
		}
	}
	if moodOf(nil, dayView{}) != moodPlain {
		t.Error("no reading was not plain")
	}
}

// Every line the frog can say is short and carries no number.
func TestMoodLinesAreClean(t *testing.T) {
	check := func(where, l string) {
		if len(l) > 110 || strings.ContainsAny(l, "0123456789%;—–") {
			t.Errorf("%s: %q", where, l)
		}
		rest := strings.NewReplacer("{show}", "", "{game}", "").Replace(l)
		if strings.ContainsAny(rest, "{}") {
			t.Errorf("%s has an unknown slot: %q", where, l)
		}
	}
	for _, m := range []string{moodPlain, moodBright, moodGloom1, moodGloom2, moodGloom3, moodJitters, moodRain, moodShowers, moodFrost, moodHeat, moodPollen, moodSombre} {
		if len(moodLines[m]) < 3 {
			t.Errorf("mood %s has %d lines", m, len(moodLines[m]))
		}
		for _, l := range moodLines[m] {
			check(m, l)
		}
	}
	for _, l := range moodLines[moodSombre] {
		if strings.Contains(l, "{") {
			t.Errorf("a sombre line names a show or game: %q", l)
		}
	}
	for k, pool := range hobbyLines {
		for _, l := range pool {
			check(k, l)
		}
	}
	for _, f := range fortunes {
		check("fortune", f.Line)
	}
	for _, tm := range []tomorrow{{30, 20, 0, "Snow"}, {95, 72, 10, "Clear"}, {60, 40, 90, "Rain"}, {70, 50, 0, "Fog"}} {
		for _, day := range []string{"2026-11-10", "2026-11-11", "2026-11-12"} {
			check("forecast", forecastLine(&tm, day))
		}
	}
}

func TestForecastLovesRain(t *testing.T) {
	l := forecastLine(&tomorrow{55, 40, 80, "Rain showers"}, "2026-11-10")
	if !strings.Contains(strings.ToLower(l), "rain") || strings.Contains(l, "sadly") {
		t.Errorf("rain: %q", l)
	}
	l = forecastLine(&tomorrow{50, 28, 0, "Clear"}, "2026-11-10")
	if !strings.Contains(l, "cool") || !strings.Contains(l, "frost") {
		t.Errorf("cold and clear: %q", l)
	}
	if forecastLine(nil, "2026-11-10") != "" {
		t.Error("no tomorrow still made a forecast")
	}
}

func TestTitlesTheFrogWillMention(t *testing.T) {
	cases := map[string]bool{
		"Apex Legends™":                 false,
		"Stardew Valley":                true,
		"Call of Duty®: Modern Warfare": false,
		"Dead Island":                   false,
		"Edward's Garden":               true,
		"Murder, She Wrote":             false,
		"Battlefield":                   false,
		"Unpacking":                     true,
		"Cyberpunk 2077":                false,
	}
	for name, ok := range cases {
		got, fits := fitTitle(name, nil)
		if name == "Apex Legends™" {
			if !fits || got != "Apex Legends" {
				t.Errorf("trademark not tidied: %q %v", got, fits)
			}
			continue
		}
		if fits != ok {
			t.Errorf("%q: fits %v, want %v", name, fits, ok)
		}
	}
	if _, ok := fitTitle("Cosy Farm", []string{"Sexual Content"}); ok {
		t.Error("a blocked tag got through")
	}
}

func TestLinesRotateDaily(t *testing.T) {
	pool := moodLines[moodPlain]
	days := []string{"2026-11-10", "2026-11-11", "2026-11-12", "2026-11-13"}
	for _, seed := range []uint32{1, 77, 9001, 4000000000} {
		prev := ""
		for _, d := range days {
			l := pickLine(pool, seed, d, nil, nil)
			if l == "" || l == prev {
				t.Errorf("seed %d on %s: %q after %q", seed, d, l, prev)
			}
			prev = l
		}
	}
	// A line with a slot is skipped when there's nothing to fill it, and
	// filled when there is.
	for i := range 20 {
		l := pickLine(moodLines[moodRain], uint32(i), "2026-11-10", nil, nil)
		if strings.Contains(l, "{") {
			t.Errorf("empty slot left in %q", l)
		}
		l = pickLine(moodLines[moodRain], uint32(i), "2026-11-10", []string{"Bluey"}, nil)
		if strings.Contains(l, "{") {
			t.Errorf("slot not filled in %q", l)
		}
	}
}

func TestHobbies(t *testing.T) {
	r := &reading{Shows: []string{"Bluey"}, Games: []string{"Stardew Valley"}}
	seen := map[string]int{}
	for seed := range uint32(400) {
		h := hobbyOf(r, moodPlain, seed, "2026-11-10")
		if h == nil {
			seen[""]++
			continue
		}
		seen[h.Kind]++
		if !strings.Contains(h.Line, h.Title) {
			t.Errorf("hobby line misses its title: %+v", h)
		}
		if hobbyOf(r, moodSombre, seed, "2026-11-10") != nil {
			t.Error("a hobby on a sombre day")
		}
	}
	if seen["tv"] < 50 || seen["game"] < 50 || seen[""] < 200 {
		t.Errorf("hobby split: %v", seen)
	}
	if hobbyOf(&reading{}, moodPlain, 0, "2026-11-10") != nil {
		t.Error("a hobby with nothing to watch or play")
	}
}

func TestFortuneHoldsAllDay(t *testing.T) {
	lucky := 0
	for id := range int64(300) {
		a, b := fortuneOf(id, "2026-11-10"), fortuneOf(id, "2026-11-10")
		if a != b {
			t.Fatal("a fortune changed inside a day")
		}
		if a.Luck != luckNone {
			lucky++
		}
	}
	if lucky < 60 || lucky > 180 {
		t.Errorf("%d of 300 ponds lucky", lucky)
	}
}
