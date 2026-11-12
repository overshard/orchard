package main

import (
	"database/sql"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
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
	if st, _ := s.arcadeStart(id, "school"); st.Fish != nil {
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
	d, err := s.arcadeFinish(id, "fish", st.Run, score, landed, nil)
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
	pw := v.Share.Power
	if f := pw.part("fish"); f.Have != 5 || f.All != len(fishKinds) || f.Pts < 1000 || f.Pts > f.Of/2 {
		t.Errorf("five fish, one of each tier, count %+v", f)
	}
	if got := pw.Total - before.Share.Power.Total; got != pw.part("fish").Pts+pw.part("game_fish").Pts+pw.part("badges").Pts-before.Share.Power.part("badges").Pts {
		t.Errorf("the total went up %d", got)
	}
	var public string
	if err := s.r.QueryRow(`SELECT public_id FROM ponds WHERE id = ?`, id).Scan(&public); err != nil {
		t.Fatal(err)
	}
	sh, err := s.shownBy(public)
	if err != nil {
		t.Fatal(err)
	}
	if sh.Power.part("fish") != pw.part("fish") || sh.Power.Total != pw.Total {
		t.Errorf("the frog's page counts %+v for fish and the pond %+v", sh.Power.part("fish"), pw.part("fish"))
	}
	if len(sh.Best) == 0 || sh.Best[0].Kind != fishHad+"wels" {
		t.Errorf("the best find = %+v", sh.Best)
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

func TestPondRecordsAreMostlySedges(t *testing.T) {
	sedge := 0
	for _, f := range fishKinds {
		size, who := pondRecord(f)
		t.Logf("%s: %s inches, %s", f.name, inches(size), residents[who].name)
		if size <= f.lo+(f.hi-f.lo)*3/4 || size >= f.hi {
			t.Errorf("the %s record is %d between %d and %d", f.kind, size, f.lo, f.hi)
		}
		if who == 0 {
			sedge++
		}
		for _, w := range []int{0, 3} {
			if l := sedgeSays(f, w); strings.ContainsAny(l, "—–;:!") || !strings.HasSuffix(l, ".") {
				t.Errorf("sedge on %s: %s", f.kind, l)
			}
		}
	}
	if sedge <= len(fishKinds)/2 || sedge == len(fishKinds) {
		t.Errorf("sedge holds %d of %d records", sedge, len(fishKinds))
	}
}

func TestBeatingAPondRecord(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	roach, _ := fishOf("roach")
	rec, who := pondRecord(roach)
	// Rounds of roach until one is over the record, which doesn't take long.
	var d *arcadeDone
	for range 400 {
		d = goFishing(t, s, clock, id, 200, map[string]int{"roach": 40})
		if d.Catch[0].Record {
			break
		}
		if d.Sedge != "" || len(bookOf(t, s, id)["roach"].Holder) == 0 || bookOf(t, s, id)["roach"].Yours {
			t.Fatalf("no record and %+v, %q", bookOf(t, s, id)["roach"], d.Sedge)
		}
	}
	got := d.Catch[0]
	if !got.Record || got.Size <= rec || got.From != residents[who].name || d.Sedge == "" {
		t.Fatalf("the record round = %+v, sedge %q", got, d.Sedge)
	}
	if b := bookOf(t, s, id)["roach"]; !b.Yours || b.Record != rec || b.Best != got.Size {
		t.Errorf("the book after = %+v", b)
	}
	if v, _ := s.view(id); len(v.Gazette.Records) != 0 {
		t.Errorf("today's record is in today's paper, %v", v.Gazette.Records)
	}
	// Once it's the pond's a bigger one is only a best.
	for range 30 {
		for _, c := range goFishing(t, s, clock, id, 200, map[string]int{"roach": 40}).Catch {
			if c.Record {
				t.Fatalf("the record went twice, %+v", c)
			}
		}
	}
	*clock = clock.Add(24 * time.Hour)
	v, _ := s.view(id)
	want := "landed a roach of " + inches(bookOf(t, s, id)["roach"].Best)
	found := false
	for _, l := range v.Gazette.Records {
		found = found || (strings.Contains(l, "took the pond record off "+residents[who].name+".") && strings.Contains(l, "a roach of "))
	}
	if !found {
		t.Errorf("the paper says %v, want something like %q", v.Gazette.Records, want)
	}
}

func TestTheDiaryKeepsFishingNews(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	// Clear of midwinter, a full moon and the keepsakes for days at the pond.
	*clock = clock.Add(47 * 24 * time.Hour)
	s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ponds SET days_visited = 8, last_gift_at = ? WHERE id = ?`, clock.Add(30*24*time.Hour).Unix(), id)
		return err
	})
	s.greet(id, false)
	next := func() string {
		t.Helper()
		*clock = clock.Add(24 * time.Hour)
		if _, err := s.greet(id, false); err != nil {
			t.Fatal(err)
		}
		all := diaryAll(t, s, id)
		return all[len(all)-1].Line
	}
	next()
	// Put in by hand at the smallest size, since a rolled one can be a record.
	s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO fish (pond_id, kind, count, best, first_day) VALUES (?1, 'minnow', 2, 15, ?2), (?1, 'eel', 1, 160, ?2)`, id, dayOf(*clock))
		return err
	})
	// The badge for a first fish lands with the catch, as it would off a round.
	s.greet(id, false)
	if l := next(); kindOf(l) != "catch" || !strings.Contains(l, "eel") || strings.Contains(l, "a eel") {
		t.Errorf("after a first eel: %s", l)
	}
	// The same fish again is no news, and a lower score is no best.
	goFishing(t, s, clock, id, 50, nil)
	if l := next(); kindOf(l) != "quiet" {
		t.Errorf("after fish already in the book: %s", l)
	}
	minnow, _ := fishOf("minnow")
	_, who := pondRecord(minnow)
	for range 400 {
		if goFishing(t, s, clock, id, 55, map[string]int{"minnow": 25}).Sedge != "" {
			break
		}
	}
	if l := next(); kindOf(l) != "biggest" || !strings.Contains(l, "minnow") || !strings.Contains(l, residents[who].name) {
		t.Errorf("after a pond record: %s", l)
	}
}

func TestKeepingAFishForTheKitchen(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.keepFish(id, "perch"); !errors.Is(err, errNotHeld) {
		t.Fatalf("a fish with no round behind it: %v", err)
	}
	// The kitchen is in the works, which a new pond doesn't have yet.
	if d := goFishing(t, s, clock, id, 60, map[string]int{"perch": 1}); d.Keeps != 0 {
		t.Fatalf("a pond on its first day was offered %d keeps", d.Keeps)
	}
	*clock = clock.Add(worksOpenAfter + 24*time.Hour)
	if d := goFishing(t, s, clock, id, 60, map[string]int{"perch": 1, "pike": 1}); d.Keeps != fishKeeps {
		t.Fatalf("a round with fish offered %d keeps", d.Keeps)
	}
	if _, err := s.keepFish(id, "carp"); !errors.Is(err, errNotHeld) {
		t.Fatalf("a fish the round didn't land: %v", err)
	}
	k, err := s.keepFish(id, "pike")
	if err != nil || k.Kept != "Fish fillet" || k.Left != fishKeeps-1 {
		t.Fatalf("keeping the pike = %+v, %v", k, err)
	}
	if _, err := s.keepFish(id, "perch"); !errors.Is(err, errNotHeld) {
		t.Fatalf("a second fish off one round: %v", err)
	}
	// Starting again gives up the pick, and a round with nothing has none.
	goFishing(t, s, clock, id, 60, map[string]int{"perch": 1})
	if d := goFishing(t, s, clock, id, 60, nil); d.Keeps != 0 {
		t.Fatalf("an empty round offered %d keeps", d.Keeps)
	}
	if _, err := s.keepFish(id, "perch"); !errors.Is(err, errNotHeld) {
		t.Fatalf("a fish from a round before the last: %v", err)
	}
	for i := 1; i < fishKeeps; i++ {
		if d := goFishing(t, s, clock, id, 60, map[string]int{"perch": 1}); d.Keeps != fishKeeps-i {
			t.Fatalf("round %d offered %d keeps", i, d.Keeps)
		}
		if k, err := s.keepFish(id, "perch"); err != nil || k.Kept != "Small fry" {
			t.Fatalf("keeping a perch = %+v, %v", k, err)
		}
	}
	if d := goFishing(t, s, clock, id, 60, map[string]int{"perch": 1}); d.Keeps != 0 {
		t.Fatalf("a round past the day's keeps offered %d", d.Keeps)
	}
	if _, err := s.keepFish(id, "perch"); !errors.Is(err, errNotHeld) {
		t.Fatalf("a keep past the day's: %v", err)
	}
	v, _ := s.view(id)
	if v.held["fillet"] != 1 || v.held["smallfry"] != 2 {
		t.Fatalf("the bag holds %v", v.held)
	}
	// A fish is in the bag to cook and nowhere to sell.
	for _, b := range v.Bag {
		if b.Kind == "fillet" && (b.Free != 0 || b.Count != 1 || b.Sort != "food") {
			t.Errorf("a fillet in the bag = %+v", b)
		}
	}
	if _, err := s.stallList(id, "fillet", 5); err == nil {
		t.Error("a fillet went on the stall")
	}
	if _, err := s.wizard(id, "smallfry"); err == nil {
		t.Error("the wizard took small fry")
	}
	before := v.Pebbles
	c, err := s.cook(id, "smallfry", "fillet")
	if err != nil || c.Dish.Name != "Fisher's pie" || !c.New {
		t.Fatalf("cooking the two = %+v, %v", c, err)
	}
	v, err = s.sellDish(id, c.Dish.Kind)
	if err != nil || v.Pebbles != before+dishWorth[1] {
		t.Fatalf("Big Bean paid %d for it, %v", v.Pebbles-before, err)
	}
	*clock = clock.Add(24 * time.Hour)
	if d := goFishing(t, s, clock, id, 60, map[string]int{"perch": 1}); d.Keeps != fishKeeps {
		t.Fatalf("a new day offered %d keeps", d.Keeps)
	}
}

// What one kept fish adds to a dish over a duckweed in its place, which is
// free out of any garden. A day's keeps have to stay well under fishing's cap.
func TestAKeptFishIsWorthLittle(t *testing.T) {
	most := 0.0
	for a, A := range ingredients {
		for b := a; b < len(ingredients); b++ {
			B := ingredients[b]
			d := dishOf(a, b)
			var each float64
			switch {
			case A.fish && B.fish:
				each = float64(dishWorth[d.tier]) / 2
			case B.fish:
				each = float64(dishWorth[d.tier] - dishWorth[dishOf(0, a).tier])
			default:
				continue
			}
			if each > most {
				most = each
				t.Logf("%s is worth %d, %.1f a fish", d.name, dishWorth[d.tier], each)
			}
		}
	}
	g, _ := arcadeOf("fish")
	if day := most * fishKeeps; day > float64(g.cap)/2 {
		t.Errorf("a day's kept fish cook into %.0f pebbles and fishing's cap is %d", day, g.cap)
	}
	for _, in := range ingredients {
		if !in.fish {
			continue
		}
		if in.tier != 0 || in.grows() {
			t.Errorf("%s is tier %d and grows %v", in.kind, in.tier, in.grows())
		}
		if _, ok := worthOf(in.kind); ok {
			t.Errorf("%s has a going rate on the stall", in.kind)
		}
		if _, ok := wizardTier(in.kind); ok {
			t.Errorf("the wizard takes %s", in.kind)
		}
		if _, ok := shopFloor(in.kind); ok {
			t.Errorf("a shop sells %s", in.kind)
		}
	}
	for day := range 400 {
		for _, l := range larderStock(dayOf(time.Date(2026, 1, 1, 12, 0, 0, 0, pondZone).AddDate(0, 0, day)), false) {
			if i, _ := ingredientOf(l.Kind); !ingredients[i].grows() {
				t.Fatalf("Big Bean has %s on the shelf", l.Kind)
			}
		}
	}
	for seed := range uint32(3000) {
		d, _ := dishOfKind(seedDish(seed))
		if ingredients[d.a].fish || ingredients[d.b].fish {
			t.Fatalf("seed %d's favourite off its seed is %s", seed, d.name)
		}
	}
	if w := seasonWeights(time.Now(), true); w[len(w)-1] != 0 || w[len(w)-2] != 0 {
		t.Errorf("a fish can come up in a plot, %v", w)
	}
}

func TestAFewNeighboursLikeAFishDish(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	for name, pair := range alsoLikes {
		i := residentNamed(name)
		if i < 0 || !ingredients[pair[0]].fish || !ingredients[pair[1]].fish {
			t.Fatalf("%s also likes %v", name, pair)
		}
		also, _ := residents[i].alsoDish()
		if also == residents[i].favDish() {
			t.Errorf("%s's second favourite is its first", name)
		}
	}
	sedge := residentNamed("Sedge")
	supper, _ := residents[sedge].alsoDish()
	stock(t, s, id, supper.kind(), 1)
	b, err := s.gift(id, sedge, supper.kind())
	if err != nil || !strings.Contains(b.Line, "nearly best") {
		t.Fatalf("a fish supper for Sedge = %+v, %v", b, err)
	}
	v, _ := s.view(id)
	if n := v.Neighbours.All[sedge]; n.Points != 2*giftPoints[supper.tier] || n.Also != "Fish supper" || n.AlsoA != "" {
		t.Errorf("Sedge after = %+v", n)
	}
	if n := v.Neighbours.All[residentNamed("Dot")]; n.Also != "" {
		t.Errorf("Dot also likes %s", n.Also)
	}
}

// Ninety days of fishing to the cap, keeping a fish off every round that
// offers one, cooking them in pairs and selling Big Bean the lot.
func TestNinetyDaysOfKeptFish(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	g, _ := arcadeOf("fish")
	paid, cooked, kept := 0, 0, 0
	for range 90 {
		*clock = clock.Add(20 * time.Hour)
		for range 6 {
			d := goFishing(t, s, clock, id, 110, map[string]int{"perch": 4, "pike": 2, "minnow": 2})
			paid += d.Paid
			if d.Keeps == 0 {
				continue
			}
			if _, err := s.keepFish(id, "pike"); err != nil {
				t.Fatal(err)
			}
			kept++
		}
		v, _ := s.view(id)
		for n := v.held["fillet"]; n >= 2; n -= 2 {
			c, err := s.cook(id, "fillet", "fillet")
			if err != nil {
				t.Fatal(err)
			}
			before := v.Pebbles
			if v, err = s.sellDish(id, c.Dish.Kind); err != nil {
				t.Fatal(err)
			}
			cooked += v.Pebbles - before
		}
	}
	t.Logf("fishing paid %d a day, and %d kept fish cooked into %d a day more", paid/90, kept, cooked/90)
	if paid/90 > g.cap {
		t.Errorf("fishing paid %d a day over a cap of %d", paid/90, g.cap)
	}
	if kept > 90*fishKeeps || cooked/90 > g.cap/2 {
		t.Errorf("%d kept fish cooked into %d pebbles a day", kept, cooked/90)
	}
}
