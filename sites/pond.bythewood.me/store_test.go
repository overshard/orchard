package main

import (
	"database/sql"
	"errors"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A store on a temp file with a clock the test moves by hand.
func testStore(t *testing.T) (*store, *time.Time) {
	t.Helper()
	s, err := openStore(filepath.Join(t.TempDir(), "pond.db"), []byte("a pepper for tests, long enough to pass"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	clock := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	return s, &clock
}

func newPond(t *testing.T, s *store) (string, int64) {
	t.Helper()
	key, id, err := s.create()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.greet(id, true); err != nil {
		t.Fatal(err)
	}
	return key, id
}

// modernc ignores a pragma it does not recognise in the DSN without a word,
// so the settings are read back off the connections themselves.
func TestPragmasTook(t *testing.T) {
	s, _ := testStore(t)
	want := map[string]string{"journal_mode": "wal", "busy_timeout": "5000", "foreign_keys": "1", "synchronous": "1", "temp_store": "2"}
	for name, w := range want {
		var got string
		if err := s.w.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != w {
			t.Errorf("writer %s = %s, want %s", name, got, w)
		}
	}
	var ro string
	if err := s.r.QueryRow("PRAGMA query_only").Scan(&ro); err != nil {
		t.Fatal(err)
	}
	if ro != "1" {
		t.Errorf("the read pool can write, query_only = %s", ro)
	}
	if _, err := s.r.Exec(`DELETE FROM ponds`); err == nil {
		t.Error("a write through the read pool went through")
	}
}

func TestWordList(t *testing.T) {
	if len(wordList) < 1024 || len(wordList)&(len(wordList)-1) != 0 {
		t.Fatalf("%d words, want a power of two of at least 1024", len(wordList))
	}
	if len(wordSet) != len(wordList) {
		t.Errorf("%d of the words are repeats", len(wordList)-len(wordSet))
	}
	for _, w := range wordList {
		if len(w) < 3 || len(w) > 8 || strings.Trim(w, "abcdefghijklmnopqrstuvwxyz") != "" {
			t.Errorf("%q is not three to eight plain letters", w)
		}
	}
}

func TestKeyFormatsAllOpenThePond(t *testing.T) {
	s, _ := testStore(t)
	key, id := newPond(t, s)
	w := strings.Split(key, "-")
	typed := []string{
		key,
		strings.Join(w, " "),
		strings.ToUpper(strings.Join(w, ", ")),
		"  " + strings.Join(w, "\n") + "\n",
		strings.Join(w, " - "),
		"1. " + w[0] + " 2. " + w[1] + " 3. " + w[2] + " 4. " + w[3] + " 5. " + w[4] + " 6. " + w[5],
	}
	for _, in := range typed {
		norm, ok := normalizeKey(in)
		if !ok {
			t.Errorf("%q was refused", in)
			continue
		}
		if got, err := s.idOf(norm); err != nil || got != id {
			t.Errorf("%q opened pond %d (%v), want %d", in, got, err, id)
		}
	}
	for _, in := range []string{"", strings.Join(w[:5], " "), key + " moss", strings.Join(w[:5], " ") + " zzzzz"} {
		if _, ok := normalizeKey(in); ok {
			t.Errorf("%q passed as a key", in)
		}
	}
}

func TestRotateShutsTheOldKey(t *testing.T) {
	s, _ := testStore(t)
	old, id := newPond(t, s)
	fresh, err := s.rotate(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.idOf(old); !errors.Is(err, errNoPond) {
		t.Errorf("the old key still opens something: %v", err)
	}
	if got, err := s.idOf(fresh); err != nil || got != id {
		t.Errorf("the new key opened %d (%v), want %d", got, err, id)
	}
}

func TestKeyHashNeedsThePepper(t *testing.T) {
	s, _ := testStore(t)
	key, _ := newPond(t, s)
	var stored string
	if err := s.r.QueryRow(`SELECT key_hmac FROM ponds`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, strings.Split(key, "-")[0]) {
		t.Error("the database holds the key itself")
	}
	if stored != keyHMAC(s.pepper, key) {
		t.Error("the stored hash is not the key under the pepper")
	}
	if stored == keyHMAC([]byte("some other pepper"), key) {
		t.Error("the hash is the same under a different pepper")
	}
}

func TestPresentRule(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)

	greet := func() *greeting {
		t.Helper()
		g, err := s.greet(id, false)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	*clock = clock.Add(giftGap - time.Minute)
	if g := greet(); g.Present != nil {
		t.Error("a present under four hours")
	}
	*clock = clock.Add(time.Minute)
	if g := greet(); g.Present == nil {
		t.Error("no present at four hours")
	}
	if g := greet(); g.Present != nil {
		t.Error("a second present on a reload")
	}
	*clock = clock.Add(giftGap - time.Minute)
	if g := greet(); g.Present != nil {
		t.Error("the gap was counted from the visit and not from the last present")
	}
}

func TestFreeSpinsComeInAndStopAtTwo(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	free := func() int {
		t.Helper()
		v, err := s.view(id)
		if err != nil {
			t.Fatal(err)
		}
		return v.Wheel.Free
	}
	if free() != 1 {
		t.Fatalf("a new pond has %d free spins, want 1", free())
	}
	*clock = clock.Add(100 * time.Hour)
	if free() != maxFreeSpins {
		t.Fatalf("%d free spins after days away, want %d", free(), maxFreeSpins)
	}
	for range maxFreeSpins {
		if _, err := s.spin(id, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.spin(id, false); !errors.Is(err, errNotYet) {
		t.Errorf("a third free spin: %v", err)
	}
	*clock = clock.Add(spinEvery - time.Minute)
	if free() != 0 {
		t.Error("a free spin came back early")
	}
	*clock = clock.Add(time.Minute)
	if free() != 1 {
		t.Error("no free spin after four hours")
	}
}

func TestPaidSpinsCapADay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.w.Exec(`UPDATE ponds SET pebbles = 10000`); err != nil {
		t.Fatal(err)
	}
	for i := range paidSpinsADay {
		if _, err := s.spin(id, true); err != nil {
			t.Fatalf("paid spin %d: %v", i+1, err)
		}
	}
	if _, err := s.spin(id, true); !errors.Is(err, errNotYet) {
		t.Errorf("a sixth paid spin: %v", err)
	}
	*clock = clock.Add(24 * time.Hour)
	if _, err := s.spin(id, true); err != nil {
		t.Errorf("no paid spin the next day: %v", err)
	}
	if _, err := s.w.Exec(`UPDATE ponds SET pebbles = 24`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.spin(id, true); !errors.Is(err, errPoor) {
		t.Errorf("a paid spin on 24 pebbles: %v", err)
	}
}

func dive(t *testing.T, s *store, clock *time.Time, id int64, score int) *dived {
	t.Helper()
	run, err := s.diveStart(id)
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Duration(score)*time.Second + 5*time.Second)
	d, err := s.diveFinish(id, run.Run, score, 0)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDivePaysToTheCap(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	total := 0
	for i, want := range []int{20, 20, 20, 5, 5} {
		d := dive(t, s, clock, id, 10)
		if d.Paid-len(d.Aims)*aimPebbles != want {
			t.Errorf("run %d paid %d, want %d", i+1, d.Paid, want)
		}
		total += d.Paid
	}
	for range 40 {
		total += dive(t, s, clock, id, 30).Paid
	}
	if total != diveCap {
		t.Errorf("a day of diving paid %d, want the cap of %d", total, diveCap)
	}
	*clock = clock.Add(24 * time.Hour)
	// A day off is a rest, so the first run back is sixty and a quarter more,
	// and so is any goal it met.
	if d := dive(t, s, clock, id, 30); d.Paid != 75+len(d.Aims)*aimPebbles*5/4 || d.Rested != (60+len(d.Aims)*aimPebbles)/4 {
		t.Errorf("the first run of the next day paid %d with %d goals, want 75 and theirs", d.Paid, len(d.Aims))
	}
}

func TestThePaperPrintsYesterdaysBests(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	dive(t, s, clock, id, 10)
	*clock = clock.Add(24 * time.Hour)
	if v := dive(t, s, clock, id, 5).Pond; len(v.Gazette.Records) != 0 {
		t.Fatalf("a first score made the paper: %v", v.Gazette.Records)
	}
	dive(t, s, clock, id, 14)
	v := dive(t, s, clock, id, 17).Pond
	if len(v.Gazette.Records) != 0 {
		t.Fatalf("today's best is in today's paper: %v", v.Gazette.Records)
	}
	*clock = clock.Add(24 * time.Hour)
	v = dive(t, s, clock, id, 3).Pond
	if len(v.Gazette.Records) != 1 || !strings.Contains(v.Gazette.Records[0], "Dive yesterday, 17.") {
		t.Fatalf("the paper says %v", v.Gazette.Records)
	}
}

func TestDiveRefusesWhatCannotBe(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)

	start, _ := s.diveStart(id)
	run := start.Run
	*clock = clock.Add(2 * time.Second)
	d, err := s.diveFinish(id, run, 300, 0)
	if err != nil || d.Paid != 0 || d.Pond.Dive.Best != 0 {
		t.Errorf("300 points in two seconds paid %d, best %d (%v)", d.Paid, d.Pond.Dive.Best, err)
	}
	if _, err := s.diveFinish(id, run, 1, 0); !errors.Is(err, errBadRun) {
		t.Errorf("a run id worked twice: %v", err)
	}
	if _, err := s.diveFinish(id, "made up", 5, 0); !errors.Is(err, errBadRun) {
		t.Errorf("a made up run id worked: %v", err)
	}
}

func TestLogPaysOnceADay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	out, err := s.turnLog(id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Pond.Pebbles; got != starterPebbles+out.Finds[1].Pebbles {
		t.Errorf("purse is %d after a find of %d", got, out.Finds[1].Pebbles)
	}
	if _, err := s.turnLog(id, 0); !errors.Is(err, errNotYet) {
		t.Errorf("the log paid twice in a day: %v", err)
	}
	*clock = clock.Add(24 * time.Hour)
	if _, err := s.turnLog(id, 0); err != nil {
		t.Errorf("the log did not come back the next day: %v", err)
	}
}

func TestHatsBoxWearAndSell(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.box(id); !errors.Is(err, errPoor) {
		t.Fatalf("a box on %d pebbles: %v", starterPebbles, err)
	}
	if _, err := s.w.Exec(`UPDATE ponds SET pebbles = 100000`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.wear(id, hat{1, 0}); !errors.Is(err, errNotHeld) {
		t.Errorf("wore a hat never found: %v", err)
	}

	var spare *hat
	for range 400 {
		out, err := s.box(id)
		if err != nil {
			t.Fatal(err)
		}
		if out.Prize.Spare && spare == nil {
			spare = out.Prize.Hat
		}
	}
	if spare == nil {
		t.Fatal("400 boxes and no spare")
	}
	v, err := s.wear(id, *spare)
	if err != nil || v.Frog.Hat != *spare {
		t.Fatalf("wearing %+v gave %+v (%v)", *spare, v.Frog.Hat, err)
	}

	// Selling runs a hat down to the one copy and stops there, and a day's
	// selling stops at the room.
	before, want, room := v.Pebbles, 0, sellRoom
	for {
		v, err = s.sell(id, *spare)
		if errors.Is(err, errNotHeld) {
			break
		}
		if errors.Is(err, errTullyFull) {
			*clock = clock.Add(24 * time.Hour)
			room = sellRoom
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want += min(wrenPays(*spare), room)
		room = v.Room[shopWren]
	}
	v, _ = s.view(id)
	if v.Pebbles != before+want {
		t.Errorf("sold spares for %d, want %d", v.Pebbles-before, want)
	}
	for _, h := range v.Hats {
		if h.hat == *spare && h.Count != 1 {
			t.Errorf("%d of the hat left after selling the spares, want 1", h.Count)
		}
	}
}

func TestOddsLandNearWhatThePageSays(t *testing.T) {
	const draws = 200000
	var got [tierCount]int
	for range draws {
		got[rollBox().tier()]++
	}
	for t2, want := range boxOdds {
		pct := float64(got[t2]) * 100 / draws
		if pct < float64(want)-0.6 || pct > float64(want)+0.6 {
			t.Errorf("tier %d came up %.2f%% of the time, the page says %d%%", t2, pct, want)
		}
	}

	slices := map[string]int{}
	for _, s := range wheelSlices {
		slices[s]++
	}
	want := map[string]int{"P25": 4, "P40": 3, "CRI": 3, "AGA": 2, "QUA": 1, "GEO": 1, "HAT": 1, "DYE": 1}
	for k, n := range want {
		if slices[k] != n {
			t.Errorf("%d slices of %s on the wheel, want %d", slices[k], k, n)
		}
	}
	if len(wheelSlices) != 16 {
		t.Errorf("%d slices on the wheel, want 16", len(wheelSlices))
	}

	// The hat and the dye hold for a day and change across days.
	hats, dyes := map[hat]bool{}, map[string]bool{}
	day := time.Date(2026, 11, 1, 12, 0, 0, 0, pondZone)
	for i := range 60 {
		d := dayOf(day.AddDate(0, 0, i))
		h, dye := wheelOfDay(d)
		h2, dye2 := wheelOfDay(d)
		if h != h2 || dye != dye2 || !h.valid() || itemNames[dye] == "" || h.tier() > 2 {
			t.Fatalf("%s: wheel of the day %+v %q then %+v %q", d, h, dye, h2, dye2)
		}
		hats[h], dyes[dye] = true, true
	}
	if len(hats) < 15 || len(dyes) < len(wheelDyes) {
		t.Errorf("60 days put %d hats and %d dyes on the wheel", len(hats), len(dyes))
	}

	total := 0
	for _, f := range finds {
		total += f.w
	}
	geode := 0
	for _, g := range geodeInside {
		geode += g.w
	}
	if total != 100 || geode != 100 {
		t.Errorf("log weights add to %d and geode weights to %d, want 100 each", total, geode)
	}
}

func TestHatCatalogue(t *testing.T) {
	n := 0
	for tier, hats := range hatsByTier {
		if len(hats) == 0 {
			t.Errorf("no hats in tier %d, so a box that rolls it has nothing to give", tier)
		}
		n += len(hats)
	}
	if n != hatsInGame {
		t.Errorf("%d hats, want %d", n, hatsInGame)
	}
	// Every golden hat, and the two epic shapes in moss and berry.
	if got := len(hatsByTier[tierCount-1]); got != hatShapes+4 {
		t.Errorf("%d legendary hats, want %d", got, hatShapes+4)
	}
}

// What broke Neopets was money with nowhere to go. A box has to take out more
// than selling everything in it puts back, or boxes mint pebbles.
func TestABoxCostsMoreThanItsSparesSellFor(t *testing.T) {
	back := boxWorth(func(tier int) float64 { return float64(wrenPays(hatsByTier[tier][0])) })
	t.Logf("a box's spares sell back to Little Tully for %.1f", back)
	if back >= boxCost {
		t.Errorf("a box costs %d and sells back for %.1f on average", boxCost, back)
	}
}

// Ninety days of somebody who plays a bit each day, opens a box whenever the
// purse covers one and sells every spare. The purse must not climb.
func TestNinetyDaysDoNotRunAway(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	minted := 0
	for day := range 90 {
		*clock = clock.Add(20 * time.Hour)
		before, _ := s.view(id)
		if _, err := s.greet(id, false); err != nil {
			t.Fatal(err)
		}
		if _, err := s.turnLog(id, day%3); err != nil && !errors.Is(err, errNotYet) {
			t.Fatal(err)
		}
		for range 2 {
			if _, err := s.spin(id, false); err != nil && !errors.Is(err, errNotYet) {
				t.Fatal(err)
			}
		}
		for range 4 {
			dive(t, s, clock, id, 12+day%9)
		}
		for _, k := range []int{day, day + 5, day + 9} {
			playForAims(t, s, clock, id, aimedGames[k%len(aimedGames)])
		}
		playMarket(t, s, id)
		after, _ := s.view(id)
		minted += after.Pebbles - before.Pebbles

	sell:
		for _, h := range after.Hats {
			for n := h.Count; n > 1; n-- {
				_, err := s.sell(id, h.hat)
				if errors.Is(err, errTullyFull) {
					break sell
				}
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		after, _ = s.view(id)
		for after.Pebbles >= boxCost {
			if _, err := s.box(id); err != nil {
				t.Fatal(err)
			}
			after, _ = s.view(id)
		}
	}
	v, _ := s.view(id)
	perDay := minted / 90
	t.Logf("about %d pebbles a day earned, %d in the purse and %d hats found at day 90", perDay, v.Pebbles, countFound(v))
	if perDay > 250 {
		t.Errorf("a casual player earns %d pebbles a day, which is more than six boxes", perDay)
	}
	if v.Pebbles > 2*perDay+boxCost {
		t.Errorf("the purse is at %d after 90 days with boxes to spend it on", v.Pebbles)
	}
}

// The side games that take their goals' counts from the page.
var aimedGames = []string{"flick", "cross", "raft", "leap", "school", "stack", "shell", "skip", "drop", "chorus", "putt", "jar"}

// playForAims plays one run of a game that meets both its goals for the day
// with the least score that can.
func playForAims(t *testing.T, s *store, clock *time.Time, id int64, game string) *arcadeDone {
	t.Helper()
	g, _ := arcadeOf(game)
	counts, score := map[string]int{}, 0
	for _, a := range aimsOf(dayOf(*clock), game) {
		for k, n := range a.need {
			counts[k] = max(counts[k], n)
		}
		for _, k := range a.none {
			counts[k] = 0
		}
		score = max(score, a.score)
	}
	least := 0
	for k, n := range counts {
		least += n * aimWorth[game][k]
	}
	score = min(g.most, max(score, least))
	st, err := s.arcadeStart(id, game)
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Duration(float64(score)/g.rate+2) * time.Second)
	d, err := s.arcadeFinish(id, game, st.Run, score, nil, counts)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Aims) != len(aimsOf(dayOf(*clock), game)) {
		t.Fatalf("%s met %v with %v at %d", game, d.Aims, counts, score)
	}
	return d
}

// playMarket locks the day's prices, collects dividends, sells anything
// that's up and buys one of something.
func playMarket(t *testing.T, s *store, id int64) {
	t.Helper()
	if err := s.lockMarket(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.collectDividends(id); err != nil {
		t.Fatal(err)
	}
	v, _ := s.view(id)
	for _, f := range v.Market.Firms {
		if f.Held > 0 && f.Kind != kindGood && f.Worth > f.Cost {
			if _, err := s.trade(id, f.Key, f.Held, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	f := v.Market.Firms[rand.IntN(len(v.Market.Firms))]
	if _, err := s.trade(id, f.Key, 1, false); err != nil && !errors.Is(err, errPoor) && !errors.Is(err, errCap) {
		t.Fatal(err)
	}
}

func countFound(v *view) int {
	n := 0
	for _, h := range v.Hats {
		if h.Count > 0 {
			n++
		}
	}
	return n
}

func TestSweepTakesOnlyEggsNobodyCameBackTo(t *testing.T) {
	s, clock := testStore(t)
	_, left := newPond(t, s)
	_, played := newPond(t, s)
	_, returned := newPond(t, s)
	if _, err := s.turnLog(played, 0); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(3 * 24 * time.Hour)
	if _, err := s.greet(returned, false); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(6 * 24 * time.Hour)
	_, young := newPond(t, s)

	n, err := s.sweep()
	if err != nil || n != 1 {
		t.Fatalf("swept %d (%v), want 1", n, err)
	}
	if _, err := loadPond(s.r, left); !errors.Is(err, errNoPond) {
		t.Error("the egg nobody came back to is still there")
	}
	for name, id := range map[string]int64{"played": played, "returned": returned, "young": young} {
		if _, err := loadPond(s.r, id); err != nil {
			t.Errorf("the %s pond was swept: %v", name, err)
		}
	}
	var frogs int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM frogs`).Scan(&frogs); err != nil || frogs != 3 {
		t.Errorf("%d frogs left (%v), want 3", frogs, err)
	}
}

func TestNewPondsHaveACeiling(t *testing.T) {
	s, clock := testStore(t)
	for range pondsAnHour {
		if _, _, err := s.create(); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.create(); !errors.Is(err, errFull) {
		t.Errorf("pond %d in an hour was laid: %v", pondsAnHour+1, err)
	}
	*clock = clock.Add(time.Hour + time.Second)
	if _, _, err := s.create(); err != nil {
		t.Errorf("still full an hour later: %v", err)
	}
}

func TestGrowingShowsInTheGreeting(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	*clock = clock.Add(30 * time.Hour)
	g, err := s.greet(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if g.Pond.Frog.Stage != "tadpole" || g.Pond.Frog.Day != 2 {
		t.Errorf("after 30 hours it is a %s on day %d", g.Pond.Frog.Stage, g.Pond.Frog.Day)
	}
	grew := false
	for _, m := range g.Missed {
		if m[0] == "Grew" && m[1] == "from egg to tadpole" {
			grew = true
		}
	}
	if !grew {
		t.Errorf("the greeting did not say it grew: %v", g.Missed)
	}
}

func TestDivePodium(t *testing.T) {
	if got := divePlace(10, []int{3, 10, 11, 29}); got != 3 {
		t.Errorf("ten gates among 3, 10, 11 and 29 placed %d, want 3", got)
	}
	s, clock := testStore(t)
	_, id := newPond(t, s)
	form := diveFormStart
	finish := func(gates int) *dived {
		t.Helper()
		start, err := s.diveStart(id)
		if err != nil || len(start.Ghosts) != diveGhosts {
			t.Fatalf("start: %+v %v", start, err)
		}
		for _, c := range start.Ghosts {
			if c < 0 || float64(c) > max(form, diveFormFloor)*1.15 {
				t.Fatalf("a ghost dealt %d gates", c)
			}
		}
		*clock = clock.Add(2 * time.Minute)
		d, err := s.diveFinish(id, start.Run, gates, gates)
		if err != nil {
			t.Fatal(err)
		}
		form = diveForm(form, gates)
		return d
	}

	// Clearing more gates than any ghost can is first whatever the deal was,
	// and the ghosts are dealt around recent form, so each first raises it.
	before, _ := s.view(id)
	d := finish(20)
	if d.Place != 1 || d.Bonus != podiumPebbles[0] || d.Hat == nil || d.Hat.HatName == "" || d.Had {
		t.Fatalf("first place: %+v", d)
	}
	if got := d.Pond.Pebbles - before.Pebbles; got != d.Paid+d.Bonus {
		t.Errorf("first place moved the purse by %d, want %d", got, d.Paid+d.Bonus)
	}
	if !d.Pond.Dive.Podium[0] || d.Pond.Dive.Podium[1] {
		t.Errorf("podium after a first: %v", d.Pond.Dive.Podium)
	}
	if d = finish(30); d.Place != 1 || d.Bonus != 0 || d.Hat != nil || !d.Had {
		t.Errorf("a second first place the same day: %+v", d)
	}
	*clock = clock.Add(24 * time.Hour)
	if d = finish(40); d.Bonus != podiumPebbles[0] || d.Hat == nil {
		t.Errorf("first place the next day: %+v", d)
	}

	// More gates than points is not a run, and pays nothing.
	start, _ := s.diveStart(id)
	*clock = clock.Add(2 * time.Minute)
	if d, _ := s.diveFinish(id, start.Run, 5, 30); d.Place != 0 || d.Bonus != 0 || d.Paid != 0 {
		t.Errorf("thirty gates on five points: %+v", d)
	}
}

func TestGhostDeal(t *testing.T) {
	// Matching your form should win about half the time, and a run at three
	// quarters of it should usually still make the podium.
	const best, n = 24, 4000
	firsts, podiums := 0, 0
	for range n {
		ghosts := dealGhosts(best)
		if divePlace(best, ghosts) == 1 {
			firsts++
		}
		if divePlace(best*3/4, ghosts) <= 3 {
			podiums++
		}
	}
	if firsts < n*35/100 || firsts > n*75/100 {
		t.Errorf("matching the best was first %d times in %d", firsts, n)
	}
	if podiums < n*55/100 {
		t.Errorf("three quarters of the best made the podium %d times in %d", podiums, n)
	}
}

func TestFormFollowsTheRuns(t *testing.T) {
	form := 20.0
	for range 6 {
		form = diveForm(form, 2)
	}
	if form > 5 {
		t.Errorf("six runs out at gate two left form at %.1f", form)
	}
	for range 6 {
		form = diveForm(form, 30)
	}
	if form < 25 {
		t.Errorf("six runs of thirty gates left form at %.1f", form)
	}
	for _, c := range dealGhosts(0) {
		if float64(c) > diveFormFloor*1.15 {
			t.Errorf("a ghost dealt %d under the floor", c)
		}
	}
}

func TestAPanicInAWriteLeavesTheStoreWritable(t *testing.T) {
	s, _ := testStore(t)
	func() {
		defer func() { _ = recover() }()
		_ = s.write(func(tx *sql.Tx) error {
			if _, err := tx.Exec(`UPDATE ponds SET pebbles = pebbles`); err != nil {
				return err
			}
			panic("boom")
		})
	}()
	done := make(chan error, 1)
	go func() {
		done <- s.write(func(tx *sql.Tx) error { _, err := tx.Exec(`UPDATE ponds SET pebbles = pebbles`); return err })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		// A Fatal would hang in cleanup, since closing the store waits on the stuck connection.
		panic("the write after a panic never got the connection")
	}
}
