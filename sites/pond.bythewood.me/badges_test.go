package main

import (
	"database/sql"
	"os"
	"slices"
	"strings"
	"testing"
)

func badgeOf(v *view, kind string) badgeView {
	for _, b := range v.Badges {
		if b.Kind == kind {
			return b
		}
	}
	return badgeView{}
}

// most is the furthest one pond can get on its own at everything a badge
// counts, off the game's own tables.
func most() *tally {
	t := &tally{fish: len(fishKinds), records: len(fishKinds), podium: 1, won: 1, games: len(arcadeGames) + 1, played: len(arcadeGames) + 1,
		stage: stageRank["elder"], frogs: 3, hearts: bondHearts, jobBest: maxLevel, hoods: len(jobs), pets: len(pets),
		hats: hatsInGame, trinkets: len(trinkets), rocks: len(rocks), keepsakes: len(keepsakes), sets: len(sets),
		power: powerWhole, rank: len(ranks) - 1, gravel: buildLevels, garden: buildLevels, built: 4, dishes: dishCount, lodgers: 1,
		furniture: len(furniture), friends: len(residents), mementos: len(mementos), stall: 1, share: 1, days: 365, sites: len(siteHats), key: 1, swept: bugSafe, dropped: dropBadge,
		crickets: 100, fireflies: 1, mastered: 1, dealt: 1, steals: 1, stung: 1}
	for _, f := range fishKinds {
		t.bigFish += one(f.tier >= 3)
	}
	for _, h := range hatsByTier[tierCount-1] {
		t.goldHats += one(h.valid())
	}
	return t
}

// The check is a pass over a tally and nothing else, so there's no store
// here for a badge to ask.
func TestEveryBadgeCanBeEarned(t *testing.T) {
	seen := map[string]bool{}
	places := []string{"fish", "dive", "sweep", "drop", "play", "pond", "reeds", "collect", "settings", "bank", "garden", "neighbours", "stall", "market", "shops", "key", "job"}
	none, all := badgesOf(tallyOf(&view{}, &pond{}), nil), badgesOf(most(), nil)
	if len(badges) < 38 || len(badges) > 56 || len(all) != len(badges) {
		t.Fatalf("%d badges", len(badges))
	}
	for i, b := range badges {
		if seen[b.kind] || seen[b.name] {
			t.Errorf("%s is on the list twice", b.kind)
		}
		seen[b.kind], seen[b.name] = true, true
		if strings.ContainsAny(b.how, "—–;:!.") || len(b.name) > 20 || !slices.Contains(places, b.to) {
			t.Errorf("%s: %q, %q, to %q", b.kind, b.name, b.how, b.to)
		}
		for _, w := range []string{"streak", "in a row", "every day", "miss"} {
			if strings.Contains(strings.ToLower(b.name+b.how), w) {
				t.Errorf("%s reads like a streak: %s", b.kind, b.how)
			}
		}
		if none[i].Got || none[i].Have != 0 {
			t.Errorf("%s on a pond that's done nothing: %+v", b.kind, none[i])
		}
		if !all[i].Got || all[i].Of < 1 || all[i].Have != all[i].Of {
			t.Errorf("%s can't be earned: %+v", b.kind, all[i])
		}
	}
	// Three frogs fit on the pads a pond starts with.
	var pads int
	s, _ := testStore(t)
	_, id := newPond(t, s)
	s.r.QueryRow(`SELECT pads FROM ponds WHERE id = ?`, id).Scan(&pads)
	if _, of := badges[10].at(&tally{}); badges[10].kind != "chorus" || of > pads {
		t.Errorf("a chorus is %d frogs and a pond starts with %d pads", of, pads)
	}
}

func TestABadgeIsEarnedOnce(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `UPDATE ponds SET pebbles = 500 WHERE id = ?`, id)
	if v, _ := s.view(id); badgeOf(v, "bar").Got || v.Landed != nil {
		t.Fatalf("before the bar's built: %+v", badgeOf(v, "bar"))
	}
	v, err := s.build(id, "gravel")
	if err != nil {
		t.Fatal(err)
	}
	if v.Landed == nil || !slices.Contains(v.Landed.Badges, "bar") || badgeOf(v, "bar").Day != "2026-11-10" {
		t.Fatalf("building the bar: %+v", v.Landed)
	}
	g, _ := s.greet(id, false)
	if g.Pond.Landed != nil || !badgeOf(g.Pond, "bar").Got {
		t.Errorf("the greeting after: %+v", g.Pond.Landed)
	}
	// It's kept whatever happens to what earned it.
	exec(t, s, `UPDATE ponds SET gravel_level = 0 WHERE id = ?`, id)
	if v, _ = s.view(id); !badgeOf(v, "bar").Got || badgeOf(v, "bar").Have != 1 {
		t.Errorf("with the bar gone: %+v", badgeOf(v, "bar"))
	}
	var n int
	s.r.QueryRow(`SELECT COUNT(*) FROM badges WHERE pond_id = ? AND kind = 'bar'`, id).Scan(&n)
	if n != 1 {
		t.Errorf("%d rows for the one badge", n)
	}
}

// A pond from before there were badges gets what it's earned at its next
// greeting, all in the one answer.
func TestTheGreetingBackfillsBadges(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	exec(t, s, `DELETE FROM badges`)
	exec(t, s, `UPDATE ponds SET gravel_level = 2, garden_level = 5, days_visited = 40, tried = ?, key_saved_at = 1 WHERE id = ?`, triedStall|triedShare, id)
	exec(t, s, `UPDATE frogs SET hatched_at = hatched_at - 40*86400, bond = 250 WHERE pond_id = ?`, id)
	for h := 1; h <= 12; h++ {
		exec(t, s, `INSERT INTO hats (pond_id, shape, colour, count) VALUES (?, ?, 0, 1)`, id, h)
	}
	exec(t, s, `INSERT INTO fish (pond_id, kind, count, best, first_day) VALUES (?, 'roach', 3, 90, '2026-10-01')`, id)
	exec(t, s, `INSERT INTO cookbook (pond_id, dish, first_at) VALUES (?, 'dish_0_0', 0)`, id)
	exec(t, s, `INSERT INTO neighbours (pond_id, who, points) VALUES (?, 0, ?)`, id, friendAt[1])
	g, err := s.greet(id, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first_fish", "hatch", "grown", "elder", "heart_1", "hats_10", "bar", "garden", "cook_1", "friend_1", "stall", "share", "days_30", "key"}
	if g.Pond.Landed == nil {
		t.Fatal("nothing landed")
	}
	for _, k := range want {
		if !slices.Contains(g.Pond.Landed.Badges, k) {
			t.Errorf("%s didn't land: %v", k, g.Pond.Landed.Badges)
		}
	}
	var n int
	s.r.QueryRow(`SELECT COUNT(*) FROM badges WHERE pond_id = ?`, id).Scan(&n)
	if n != len(g.Pond.Landed.Badges) || n < len(want) {
		t.Errorf("%d rows for %d landed", n, len(g.Pond.Landed.Badges))
	}
	if g, _ = s.greet(id, false); g.Pond.Landed != nil {
		t.Errorf("the second greeting landed %+v", g.Pond.Landed)
	}
}

// A move that earns nothing writes nothing.
func TestLandingNothingWritesNothing(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	s.greet(id, false)
	v, _ := s.view(id)
	if err := s.write(func(tx *sql.Tx) error { return land(tx, v, &pond{ID: id, today: "2026-11-10"}) }); err != nil {
		t.Fatal(err)
	}
	if v.Landed != nil {
		t.Errorf("landed %+v", v.Landed)
	}
}

// The page draws a pin for every badge and a piece of scenery for every set.
func TestThePageDrawsEveryBadgeAndSet(t *testing.T) {
	pins, err := os.ReadFile("frontend/src/pins.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range badges {
		if !strings.Contains(string(pins), "\n  "+b.kind+": [") {
			t.Errorf("%s has no pin in pins.js", b.kind)
		}
	}
	scenery, err := os.ReadFile("frontend/src/scenery.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sets {
		if !strings.Contains(string(scenery), "\n  "+s.scene+": {") {
			t.Errorf("%s has no scenery in scenery.js", s.scene)
		}
	}
}
