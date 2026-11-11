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

func goFishing(t *testing.T, s *store, clock *time.Time, id int64, score int, landed map[string]int) *arcadeDone {
	t.Helper()
	st, err := s.arcadeStart(id, "fish")
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(61 * time.Second)
	d, err := s.arcadeFinish(id, "fish", st.Run, score, landed)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func bookOf(t *testing.T, s *store, id int64) map[string]fishSlot {
	t.Helper()
	v, err := s.view(id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]fishSlot{}
	for _, f := range v.Fishing.All {
		out[f.Kind] = f
	}
	return out
}

func TestACatchGoesInTheFishBook(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	d := goFishing(t, s, clock, id, 20, map[string]int{"perch": 2, "minnow": 1, "boot": 1})
	if len(d.Catch) != 2 || d.Catch[0].Kind != "minnow" || d.Catch[1].Kind != "perch" || d.Catch[1].N != 2 {
		t.Fatalf("the catch = %+v", d.Catch)
	}
	for _, c := range d.Catch {
		f, _ := fishOf(c.Kind)
		if c.Size < f.lo || c.Size > f.hi || !c.First || c.Best {
			t.Errorf("%s came out as %+v", c.Kind, c)
		}
	}
	book := bookOf(t, s, id)
	if p := book["perch"]; p.Count != 2 || p.Best != d.Catch[1].Size || p.First != "2026-11-10" {
		t.Errorf("perch in the book = %+v", p)
	}
	if v, _ := s.view(id); v.Fishing.Found != 2 || len(v.Fishing.All) != len(fishKinds) {
		t.Errorf("the book has %d of %d", v.Fishing.Found, len(v.Fishing.All))
	}
	// Enough perch that one of them is all but sure to be bigger.
	*clock = clock.Add(24 * time.Hour)
	d = goFishing(t, s, clock, id, 200, map[string]int{"perch": 30})
	after := bookOf(t, s, id)["perch"]
	if after.Count != 32 || after.Best < book["perch"].Best || after.First != "2026-11-10" || d.Catch[0].First {
		t.Errorf("perch after another round = %+v, %+v", after, d.Catch)
	}
	if d.Catch[0].Best != (after.Best > book["perch"].Best) {
		t.Errorf("a best of %d after %d said %v", after.Best, book["perch"].Best, d.Catch[0].Best)
	}
}

func TestTheCatchHasToFitTheScore(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	for name, landed := range map[string]map[string]int{
		"more points than the score": {"wels": 2},
		"more fish than a round":     {"minnow": fishMost + 1},
		"a count under nothing":      {"minnow": -1, "perch": 1},
	} {
		if d := goFishing(t, s, clock, id, 100, landed); len(d.Catch) != 0 {
			t.Errorf("%s was kept, %+v", name, d.Catch)
		}
	}
	if d := goFishing(t, s, clock, id, 5000, map[string]int{"perch": 1}); len(d.Catch) != 0 || d.Score != 0 {
		t.Errorf("a score that can't be kept its fish, %+v", d)
	}
	if v, _ := s.view(id); v.Fishing.Found != 0 {
		t.Errorf("the book has %d fish", v.Fishing.Found)
	}
}

func TestAFishThatWasntBitingIsDropped(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	d := goFishing(t, s, clock, id, 120, map[string]int{"moonkoi": 1, "tench": 1, "roach": 1, "kraken": 3})
	if len(d.Catch) != 1 || d.Catch[0].Kind != "roach" {
		t.Fatalf("a plain day's catch = %+v", d.Catch)
	}
	s.day.pinned.Store(true)
	s.day.last.Store(&reading{At: s.now(), Moon: true})
	d = goFishing(t, s, clock, id, 120, map[string]int{"moonkoi": 1, "tench": 1})
	if len(d.Catch) != 1 || d.Catch[0].Kind != "moonkoi" {
		t.Fatalf("a full moon's catch = %+v", d.Catch)
	}
}

func TestSizesLeanSmall(t *testing.T) {
	f, _ := fishOf("pike")
	sum, top := 0, 0
	const n = 20000
	for range n {
		size := rollSize(f)
		if size < f.lo || size > f.hi {
			t.Fatalf("a pike of %d", size)
		}
		sum += size
		if size > f.lo+(f.hi-f.lo)*9/10 {
			top++
		}
	}
	if mean := sum / n; mean < f.lo+(f.hi-f.lo)/4 || mean > f.lo+(f.hi-f.lo)*4/10 {
		t.Errorf("pike average %d between %d and %d", mean, f.lo, f.hi)
	}
	if top < n/50 || top > n/10 {
		t.Errorf("%d of %d pike were in the top tenth", top, n)
	}
}

func TestTheFishBookCountsInPower(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	before, _ := s.view(id)
	goFishing(t, s, clock, id, 200, map[string]int{"minnow": 1, "trout": 1, "carp": 2, "koi": 1, "wels": 1})
	v, _ := s.view(id)
	want := hatPower[0] + hatPower[1] + hatPower[2] + hatPower[3] + hatPower[4]
	pw := v.Share.Power
	if pw.Fish != want || pw.FishFound != 5 || pw.Total != before.Share.Power.Total+want {
		t.Errorf("five fish count %d of %d found, total %d from %d", pw.Fish, pw.FishFound, pw.Total, before.Share.Power.Total)
	}
	var public string
	if err := s.r.QueryRow(`SELECT public_id FROM ponds WHERE id = ?`, id).Scan(&public); err != nil {
		t.Fatal(err)
	}
	sh, err := s.shownBy(public)
	if err != nil {
		t.Fatal(err)
	}
	if sh.Power.Fish != want || sh.Power.Total != pw.Total {
		t.Errorf("the frog's page counts %d for fish and the pond %d", sh.Power.Fish, want)
	}
	if len(sh.Best) == 0 || sh.Best[0].Kind != fishHad+"wels" {
		t.Errorf("the best find = %+v", sh.Best)
	}
	full := 0
	for _, f := range fishKinds {
		full += hatPower[f.tier]
	}
	if got := fullPower().Fish; got != full {
		t.Errorf("a full book counts %d, want %d", got, full)
	}
}

func TestTheFishBookIsAGoal(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	goFishing(t, s, clock, id, 20, map[string]int{"minnow": 1})
	v, _ := s.view(id)
	v.Starter = nil
	p, _ := loadPond(s.r, id)
	var got *goal
	for len(v.Kitchen.Cookbook) < v.Kitchen.Of {
		v.Kitchen.Cookbook = append(v.Kitchen.Cookbook, dishView{})
	}
	v.Bank.Gravel.Level, v.Bank.Garden.Level, v.Bank.Burrows.Level = 1, 1, 0
	for i := range v.Arcade {
		v.Arcade[i].Best = 1
	}
	v.Dive.Best = 1
	p.Tried = triedStall | triedShare
	v.Neighbours.All[0].Points = 1
	for _, g := range goalsOf(v, p) {
		if g.Kind == "book" {
			got = &g
		}
	}
	if got == nil || got.To != "fish" || got.Count != "1/20" {
		t.Errorf("the fish book goal = %+v out of %+v", got, goalsOf(v, p))
	}
}
