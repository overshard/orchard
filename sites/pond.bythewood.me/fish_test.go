package main

import (
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"
)

func TestTheFishTableFollowsTheDay(t *testing.T) {
	plain := fishTable(fishDay{})
	for _, k := range []string{"minnow", "gudgeon", "roach", "rudd", "perch", "bream"} {
		if plain[k] <= 0 {
			t.Errorf("%s isn't biting on a plain day", k)
		}
	}
	for _, k := range []string{"stickleback", "tench", "moonkoi"} {
		if _, ok := plain[k]; ok {
			t.Errorf("%s is in the water on a plain day", k)
		}
	}
	for _, c := range []struct {
		name string
		day  fishDay
		up   []string
	}{
		{"night", fishDay{night: true}, []string{"eel", "catfish", "zander", "wels"}},
		{"rain", fishDay{rain: true}, []string{"trout"}},
		{"a warm afternoon", fishDay{warm: true}, []string{"carp"}},
		{"cold", fishDay{cold: true}, []string{"pike", "monster"}},
		{"clear water", fishDay{clear: true}, []string{"grayling"}},
		{"murky water", fishDay{murky: true}, []string{"tench"}},
		{"a full moon", fishDay{moon: true}, []string{"moonkoi"}},
		{"frogspawn season", fishDay{spawn: true}, []string{"stickleback"}},
	} {
		got := fishTable(c.day)
		for _, k := range c.up {
			if got[k] <= plain[k] {
				t.Errorf("%s: %s bites at %v and %v on a plain day", c.name, k, got[k], plain[k])
			}
		}
		for k, w := range plain {
			if got[k] < w {
				t.Errorf("%s: %s dropped from %v to %v", c.name, k, w, got[k])
			}
		}
	}
	every := fishTable(fishDay{true, true, true, true, true, true, true, true})
	if len(every) != len(fishKinds) {
		t.Errorf("%d of %d fish can bite", len(every), len(fishKinds))
	}
}

func TestTheFishDayReadsTheRealDay(t *testing.T) {
	at := func(s string) time.Time { d, _ := time.ParseInLocation("2006-01-02 15", s, pondZone); return d }
	noon := at("2026-07-08 14")
	if d := fishDayOf(nil, noon); d != (fishDay{}) {
		t.Errorf("no reading = %+v", d)
	}
	r := &reading{HasTemp: true, Temp: 78, Cond: "Sunny", Sunrise: "6:30am", Sunset: "8:50pm", HasSP: true, SP: -0.9}
	if d := fishDayOf(r, noon); !d.warm || !d.murky || d.night || d.cold || d.rain || d.clear {
		t.Errorf("a warm murky afternoon = %+v", d)
	}
	if d := fishDayOf(r, at("2026-07-08 23")); !d.night || d.warm {
		t.Errorf("that night = %+v", d)
	}
	r = &reading{HasTemp: true, Temp: 38, Cond: "Light rain", Sunrise: "7:00am", Sunset: "5:30pm", HasSP: true, SP: 0.8}
	if d := fishDayOf(r, at("2026-03-01 10")); !d.cold || !d.rain || !d.clear || !d.spawn || d.warm {
		t.Errorf("a cold wet morning in frogspawn season = %+v", d)
	}
	if d := fishDayOf(nil, at("2026-11-24 12")); !d.moon {
		t.Error("no moon on the full moon of 2026-11-24")
	}
	if d := fishDayOf(&reading{Moon: true, Spawn: true}, noon); !d.moon || !d.spawn {
		t.Errorf("a dev day's moon and spawn = %+v", d)
	}
}

func TestStartingFishingDealsTheTable(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	s.day.pinned.Store(true)
	s.day.last.Store(&reading{At: s.now(), HasSP: true, SP: -1})
	st, err := s.arcadeStart(id, "fish")
	if err != nil {
		t.Fatal(err)
	}
	if st.Fish["minnow"] <= 0 || st.Fish["tench"] <= 0 || st.Fish["moonkoi"] != 0 {
		t.Errorf("a murky day dealt %v", st.Fish)
	}
	if st, _ := s.arcadeStart(id, "flick"); st.Fish != nil {
		t.Errorf("tongue flick was dealt fish, %v", st.Fish)
	}
}

// The page scores a round and the server checks it, so both have every fish
// at the same points.
func TestFishMatchThePage(t *testing.T) {
	src, err := os.ReadFile("frontend/src/fishes.js")
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile(`\{ k: "(\w+)", one: "[^"]+", many: "[^"]+", pts: (\d+),`).FindAllStringSubmatch(string(src), -1)
	if len(rows) != len(fishKinds) {
		t.Fatalf("the page has %d fish and the server %d", len(rows), len(fishKinds))
	}
	for i, m := range rows {
		f := fishKinds[i]
		if pts, _ := strconv.Atoi(m[2]); m[1] != f.kind || pts != f.pts {
			t.Errorf("fish %d is %s for %s on the page and %s for %d here", i, m[1], m[2], f.kind, f.pts)
		}
	}
}
