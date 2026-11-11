package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The weights are the whole and nothing past it.
func TestTheWeightsAddToTheWhole(t *testing.T) {
	sum := gameWeight * len(powerGames())
	groups := map[string]int{}
	for _, w := range powerWeights {
		sum += w.weight
		groups[w.group] += w.weight
	}
	if sum != powerWhole {
		t.Errorf("the weights come to %d, want %d", sum, powerWhole)
	}
	t.Logf("finds %d, games %d, frogs %d", groups["finds"], gameWeight*len(powerGames()), groups["frogs"])
	for _, w := range powerWeights {
		if w.weight > powerWhole/7 {
			t.Errorf("%s is %d of the whole, which swamps the rest", w.key, w.weight)
		}
		if riseWord(w.key) == "" {
			t.Errorf("%s has nothing to say when it goes up", w.key)
		}
	}
	if gameWeight*len(powerGames()) > powerWhole/4 {
		t.Error("the games are over a quarter of the whole")
	}
	if p := powerOf(powerIn{}, 0); p.Total != 0 || p.Rank != ranks[0].name {
		t.Errorf("nothing at all is %d, %s", p.Total, p.Rank)
	}
}

// filled is a shelf with n of its things found, the commonest first.
func filled(ps []piece, n int) []piece {
	out := append([]piece{}, ps...)
	for i := range out {
		out[i].got = false
	}
	for t := range tierCount {
		for i := range out {
			if n > 0 && out[i].tier == t && !out[i].got {
				out[i].got = true
				n--
			}
		}
	}
	return out
}

// shelves is every shelf power counts, all found.
func shelves() map[string][]piece {
	out := map[string][]piece{}
	all := func(key string, tier int) { out[key] = append(out[key], piece{tier, true}) }
	for t, hs := range hatsByTier {
		for range hs {
			all("hats", t)
		}
	}
	for i := range jobs {
		all("hoods", hoodFor(i).tier())
	}
	for range siteHats {
		all("sites", 0)
	}
	for _, x := range trinkets {
		all("trinkets", x.tier)
	}
	for _, x := range rocks {
		all("rocks", x.tier)
	}
	for _, x := range keepsakes {
		all("keepsakes", x.tier)
	}
	for range mementos {
		all("mementos", mementoTier)
	}
	for _, x := range fishKinds {
		all("fish", x.tier)
	}
	out["looks"] = looksOf(nil)
	for i := range out["looks"] {
		out["looks"][i].got = true
	}
	for _, d := range dishTiers {
		all("cookbook", d)
	}
	for range furniture {
		all("furniture", 0)
	}
	for range sets {
		all("sets", 0)
	}
	for range badges {
		all("badges", 0)
	}
	return out
}

// wholePond is a pond that has found, raised and done everything, and played
// every game as well as the ponds that keep at it.
func wholePond() powerIn {
	in := powerIn{finds: shelves(), games: map[string]int{}, refs: refFloor, frogs: mostFrogs, rarest: rarestEgg,
		bond: bondHearts * bondHeart, levels: map[string]int{}, mastery: map[string]int{}, front: []int{4, 4, 4, 4}}
	for _, g := range powerGames() {
		in.games[g.key] = refFloor[g.key]
	}
	for _, j := range jobs {
		in.levels[j.key] = maxLevel
		for _, tg := range j.targets {
			in.mastery[masteryKey(j.key, tg.key)] = masteryTop
		}
	}
	return in
}

func TestPowerAddsUp(t *testing.T) {
	in := powerIn{finds: shelves(), games: map[string]int{"fish": 50, "dive": 500}, refs: refFloor, front: []int{-1, -1, -1, -1}}
	for k, ps := range in.finds {
		in.finds[k] = filled(ps, 0)
	}
	in.finds["hats"] = filled(in.finds["hats"], 5)
	in.finds["badges"] = filled(in.finds["badges"], 47)
	p := powerOf(in, 0)
	hats, badges := p.part("hats"), p.part("badges")
	if hats.Have != 5 || hats.All != hatsInGame || badges.Pts != badges.Of {
		t.Errorf("hats %+v, badges %+v", hats, badges)
	}
	// Five commons are a little of the hats, since a legendary is four of them.
	if hats.Pts < 400 || hats.Pts > 800 {
		t.Errorf("five common hats count %d", hats.Pts)
	}
	if f := p.part("game_fish"); f.Pts != gameWeight/2 || f.Have != 50 || f.All != refFloor["fish"] {
		t.Errorf("half the reference at fishing: %+v", f)
	}
	if d := p.part("game_dive"); d.Pts != gameWeight {
		t.Errorf("past the reference at Dive counts %d, want the whole %d", d.Pts, gameWeight)
	}
	sum := 0
	for _, x := range p.Parts {
		sum += x.Pts
	}
	if sum != p.Total || len(p.Parts) != len(powerWeights)+len(powerGames()) {
		t.Errorf("%d parts add to %d, the total is %d", len(p.Parts), sum, p.Total)
	}
	// The commonest frog counts nothing for rarity, the rarest egg all of it.
	if r := powerOf(powerIn{rarest: commonestEgg}, 0).part("rarity"); r.Pts != 0 {
		t.Errorf("the commonest frog: %+v", r)
	}
	if r := powerOf(powerIn{rarest: rarestEgg * 10}, 0).part("rarity"); r.Pts != r.Of {
		t.Errorf("a frog with drops: %+v", r)
	}
	if b := powerOf(powerIn{bond: 9 * bondHeart}, 0).part("bond"); b.Pts != b.Of || b.Have != bondHearts {
		t.Errorf("five hearts and no more: %+v", b)
	}
}

// A pond that has done everything is at the top, and one that has every
// collection but never played a game isn't.
func TestAWholePondIsTheTop(t *testing.T) {
	p := powerOf(wholePond(), 0)
	top := ranks[len(ranks)-1]
	t.Logf("a whole pond is %d, and the top rung is %d%% of it", p.Total, 100*top.at/p.Total)
	if p.Total != powerWhole || p.Rank != top.name || p.Next != 0 || p.NextName != "" || p.Fill != 100 {
		t.Errorf("a whole pond: %d, %s, %d more to %q, bar at %d", p.Total, p.Rank, p.Next, p.NextName, p.Fill)
	}
	in := wholePond()
	in.games = map[string]int{}
	if q := powerOf(in, 0); q.Rank == top.name {
		t.Errorf("a pond that never played is %d, at the top", q.Total)
	}
	// Half the reference in every game is short of the top too.
	in = wholePond()
	for k, v := range in.games {
		in.games[k] = v / 2
	}
	if q := powerOf(in, 0); q.Rank == top.name {
		t.Errorf("a pond at half the reference in every game is %d, at the top", q.Total)
	}
	for i := 1; i < len(ranks); i++ {
		if ranks[i].at <= ranks[i-1].at {
			t.Errorf("rung %s isn't above %s", ranks[i].name, ranks[i-1].name)
		}
		q := power{Total: ranks[i].at}
		if q.climb(0); q.Next < 0 || q.Rank != ranks[i].name {
			t.Errorf("at %d: %s with %d to go", ranks[i].at, q.Rank, q.Next)
		}
	}
}

// A rung once reached stays, and the bar never runs backwards past it.
func TestARankIsKept(t *testing.T) {
	p := power{Total: ranks[1].at + 10}
	p.climb(3)
	if p.Rank != ranks[3].name || p.Next != ranks[4].at-p.Total || p.Fill != 100*3/(len(ranks)-1) || !p.Ranks[3].Got || p.Ranks[4].Got {
		t.Errorf("kept at rung 3 on %d: %+v", p.Total, p)
	}
}

// plainPond is a pond played a little every day, at a day, a week, a month
// and three months. Hats are what the purse sim finds by then, job levels
// TestTheJobsPace's ordinary player's, with a new frog on the next job along.
var plainPond = []struct {
	day                                          int
	hats, trinkets, rocks, keepsakes, fish, dish int
	sites, mementos, furniture, sets, badges     int
	games                                        int
	form                                         float64
	frogs, rarest, bond                          int
	levels                                       []int
	mastery                                      []int
	front                                        []int
}{
	{1, 1, 1, 0, 1, 3, 0, 1, 0, 0, 0, 4, 5, 0.4, 1, 60, 20, []int{10}, []int{2}, []int{0, -1, -1, -1}},
	{7, 11, 4, 3, 3, 8, 3, 1, 0, 1, 0, 12, 12, 0.6, 2, 100, 120, []int{30, 10}, []int{2, 2, 2, 2, 3}, []int{1, 0, -1, -1}},
	{30, 33, 13, 6, 7, 13, 12, 2, 0, 3, 1, 22, 15, 0.75, 4, 300, 400, []int{50, 30, 20}, []int{3, 3, 3, 3, 3, 3, 3, 3, 2, 2, 2, 2}, []int{2, 1, 1, -1}},
	{90, 48, 20, 8, 12, 16, 30, 3, 2, 5, 2, 30, 15, 0.85, 8, 1000, 500, []int{70, 55, 45, 35, 20}, []int{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3}, []int{3, 2, 2, 2}},
}

func ordinaryAt(i int) powerIn {
	o := plainPond[i]
	in := powerIn{finds: shelves(), games: map[string]int{}, refs: refFloor, frogs: o.frogs, rarest: o.rarest, bond: o.bond,
		levels: map[string]int{}, mastery: map[string]int{}, front: o.front}
	for k, n := range map[string]int{"hats": o.hats, "trinkets": o.trinkets, "rocks": o.rocks, "keepsakes": o.keepsakes,
		"fish": o.fish, "cookbook": o.dish, "sites": o.sites, "mementos": o.mementos, "furniture": o.furniture,
		"sets": o.sets, "badges": o.badges, "hoods": 0, "looks": min(18, 1+2*o.frogs)} {
		in.finds[k] = filled(in.finds[k], n)
	}
	for _, g := range powerGames()[:o.games] {
		in.games[g.key] = int(o.form * float64(refFloor[g.key]))
	}
	for j, l := range o.levels {
		in.levels[jobs[j].key] = l
	}
	n := 0
	for _, j := range jobs {
		for _, tg := range j.targets {
			if n < len(o.mastery) {
				in.mastery[masteryKey(j.key, tg.key)] = o.mastery[n]
			}
			n++
		}
	}
	return in
}

// An ordinary pond is about the second rung at a week and the third at a
// month, with most of the ladder still ahead of it at three months.
func TestTheLadderPace(t *testing.T) {
	var at [4]power
	for i, o := range plainPond {
		at[i] = powerOf(ordinaryAt(i), 0)
		var parts []string
		for _, x := range at[i].Parts {
			if x.Pts > 0 && !strings.HasPrefix(x.Key, "game_") {
				parts = append(parts, fmt.Sprintf("%s %d", x.Key, x.Pts))
			}
		}
		t.Logf("day %2d: %6d %-16s %s", o.day, at[i].Total, at[i].Rank, strings.Join(parts, ", "))
	}
	if at[0].at != 0 {
		t.Errorf("a first day is %s", at[0].Rank)
	}
	if at[1].at != 1 {
		t.Errorf("a week is %s, want %s", at[1].Rank, ranks[1].name)
	}
	if at[2].at != 2 {
		t.Errorf("a month is %s, want %s", at[2].Rank, ranks[2].name)
	}
	if at[3].at < 3 || at[3].at > len(ranks)/2 {
		t.Errorf("three months is %s", at[3].Rank)
	}
}

// The register in Go has the tiers collect.js draws it with.
func TestTheLooksMatchThePage(t *testing.T) {
	src, err := os.ReadFile("frontend/src/collect.js")
	if err != nil {
		t.Fatal(err)
	}
	for name, set := range map[string]map[string]int{"MORPH_TIER": morphTier, "BACK_TIER": patternTier, "PUPIL_TIER": pupilTier} {
		m := regexp.MustCompile(`const ` + name + ` = \{([^}]*)\}`).FindSubmatch(src)
		if m == nil {
			t.Fatalf("no %s in collect.js", name)
		}
		pairs := regexp.MustCompile(`"?([A-Z][A-Z ]*)"?: (\d)`).FindAllSubmatch(m[1], -1)
		if len(pairs) != len(set) {
			t.Errorf("%s has %d in the page and %d here", name, len(pairs), len(set))
		}
		for _, p := range pairs {
			if got, ok := set[string(p[1])]; !ok || fmt.Sprint(got) != string(p[2]) {
				t.Errorf("%s %s is %s in the page and %d here", name, p[1], p[2], got)
			}
		}
	}
}

// A game counts against the median best of the ponds that play it, once
// there are enough of them, and never under its floor.
func TestTheReferenceHasAFloorAndNeedsRuns(t *testing.T) {
	s, _ := testStore(t)
	add := func(game string, best, plays int) {
		_, id := newPond(t, s)
		if _, err := s.w.Exec(`INSERT INTO arcade (pond_id, game, best, plays) VALUES (?, ?, ?, ?)`, id, game, best, plays); err != nil {
			t.Fatal(err)
		}
	}
	refs := func() map[string]int {
		if _, err := s.sweep(); err != nil {
			t.Fatal(err)
		}
		r, err := gameRefs(s.r)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for i := range refPonds - 1 {
		add("fish", 200+i, refRuns)
	}
	// Plenty of ponds that only tried it once don't count.
	for range 20 {
		add("fish", 5000, 1)
	}
	if r := refs(); r["fish"] != refFloor["fish"] {
		t.Errorf("with %d keen ponds fishing counts against %d", refPonds-1, r["fish"])
	}
	add("fish", 300, refRuns)
	if r := refs(); r["fish"] != 204 {
		t.Errorf("with %d keen ponds fishing counts against %d, want their median", refPonds, r["fish"])
	}
	for range refPonds {
		add("flick", 3, refRuns+4)
	}
	if r := refs(); r["flick"] != refFloor["flick"] {
		t.Errorf("a median under the floor counts against %d", r["flick"])
	}
	_, id := newPond(t, s)
	if _, err := s.w.Exec(`UPDATE ponds SET dive_best = 90, dive_plays = ? WHERE id = ?`, refRuns, id); err != nil {
		t.Fatal(err)
	}
	refs()
	var best, ponds int
	if err := s.r.QueryRow(`SELECT best, ponds FROM refs WHERE game = 'dive'`).Scan(&best, &ponds); err != nil || best != 90 || ponds != 1 {
		t.Errorf("dive kept %d over %d ponds (%v)", best, ponds, err)
	}
}

// What the sweep kept is what the pond counts against.
func TestTheCachedReferenceIsUsed(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	goFishing(t, s, clock, id, 100, nil)
	v, _ := s.view(id)
	if f := v.Share.Power.part("game_fish"); f.All != refFloor["fish"] || f.Pts != gameWeight {
		t.Fatalf("fishing at the floor: %+v", f)
	}
	if _, err := s.w.Exec(`INSERT INTO refs (game, best, ponds) VALUES ('fish', 400, ?)`, refPonds); err != nil {
		t.Fatal(err)
	}
	v, _ = s.view(id)
	if f := v.Share.Power.part("game_fish"); f.All != 400 || f.Pts != gameWeight/4 {
		t.Errorf("fishing against a kept 400: %+v", f)
	}
}

// A rise is said once, with what it came from, and a fall never is.
func TestNoticesOnlyOnARise(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	*clock = clock.Add(time.Hour)
	if v, err := s.wear(id, hat{}); err != nil || v.Share.Power.Rise != nil {
		t.Fatalf("the first move after a pond is laid: %+v (%v)", v.Share.Power.Rise, err)
	}
	if v, _ := s.wear(id, hat{}); v.Share.Power.Rise != nil {
		t.Errorf("nothing changed and it said %+v", v.Share.Power.Rise)
	}
	d := goFishing(t, s, clock, id, 100, nil)
	r := d.Pond.Share.Power.Rise
	if r == nil || r.To != d.Pond.Share.Power.Total || r.From >= r.To || !strings.HasPrefix(r.Line, "+"+commas(r.To-r.From)+", a best at Fishing") {
		t.Fatalf("a best at fishing said %+v", r)
	}
	if v, _ := s.wear(id, hat{}); v.Share.Power.Rise != nil {
		t.Errorf("said twice: %+v", v.Share.Power.Rise)
	}
	// The reference rising takes a little off and says nothing.
	if _, err := s.w.Exec(`INSERT INTO refs (game, best, ponds) VALUES ('fish', 1000, ?)`, refPonds); err != nil {
		t.Fatal(err)
	}
	v, _ := s.wear(id, hat{})
	if v.Share.Power.Rise != nil || v.Share.Power.Total >= r.To {
		t.Errorf("a fall said %+v at %d", v.Share.Power.Rise, v.Share.Power.Total)
	}
	// What comes after counts from the lower number.
	low := v.Share.Power.Total
	if _, err := s.w.Exec(`INSERT INTO fish (pond_id, kind, count, best, first_day) VALUES (?, 'minnow', 1, 20, '2026-11-10')`, id); err != nil {
		t.Fatal(err)
	}
	v, _ = s.wear(id, hat{})
	if r := v.Share.Power.Rise; r == nil || r.From != low || !strings.Contains(r.Line, "a new fish") {
		t.Errorf("a new fish said %+v from %d", r, low)
	}
}

// A pond from before the ladder was respread keeps the rank it had.
func TestOldRanksAreKept(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	for _, h := range hatsByTier[tierCount-1][:7] {
		if _, err := s.w.Exec(`INSERT INTO hats (pond_id, shape, colour, count) VALUES (?, ?, ?, 1)`, id, h.Shape, h.Colour); err != nil {
			t.Fatal(err)
		}
	}
	old, err := oldPower(s.r, id, s.now())
	if err != nil || old < 5000 || old >= 9000 {
		t.Fatalf("seven golden hats were %d (%v)", old, err)
	}
	if err := seedRanks(s.w, s.now()); err != nil {
		t.Fatal(err)
	}
	v, _ := s.view(id)
	if v.Share.Power.Rank != "LILY LORD" || v.Share.Power.at >= v.Share.Power.top {
		t.Errorf("an old lily lord at %d is %s", v.Share.Power.Total, v.Share.Power.Rank)
	}
}
