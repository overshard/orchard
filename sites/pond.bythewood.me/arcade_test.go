package main

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestArcadePaysAndCaps(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.arcadeFinish(id, "flick", "nope", 10, nil); !errors.Is(err, errBadRun) {
		t.Fatalf("a run never started: %v", err)
	}
	if _, err := s.arcadeStart(id, "pinball"); !errors.Is(err, errNotHeld) {
		t.Errorf("a game that isn't one: %v", err)
	}
	play := func(score int, secs time.Duration) *arcadeDone {
		t.Helper()
		st, err := s.arcadeStart(id, "flick")
		if err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(secs * time.Second)
		d, err := s.arcadeFinish(id, "flick", st.Run, score, nil)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if d := play(500, 5); d.Score != 0 || d.Paid != 0 {
		t.Errorf("an impossible run paid: %+v", d)
	}
	start := play(0, 1).Pond.Pebbles
	d := play(10, 20)
	if d.Paid != 10 || !d.Best || d.Pond.Pebbles != start+10 {
		t.Errorf("a first run: %+v", d)
	}
	play(10, 20)
	if d := play(30, 20); d.Paid != 7 {
		t.Errorf("a fourth run paid %d, want a quarter", d.Paid)
	}
	for range 10 {
		play(60, 20)
	}
	if v, _ := s.view(id); v.Arcade[0].Paid != v.Arcade[0].Cap {
		t.Errorf("past the cap: %+v", v.Arcade[0])
	}
	*clock = clock.Add(24 * time.Hour)
	if v, _ := s.view(id); v.Arcade[0].Paid != 0 || v.Arcade[0].Best != 60 {
		t.Errorf("a new day: %+v", v.Arcade[0])
	}
}

// A crossing run that would have filled the day's cap pays half of it.
func TestCrossingPaysHalf(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	st, err := s.arcadeStart(id, "cross")
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(60 * time.Second)
	d, err := s.arcadeFinish(id, "cross", st.Run, 61, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Paid != 30 {
		t.Errorf("a 61 point crossing paid %d, want 30", d.Paid)
	}
}

// Every game the page can open has to be one the server will start.
func TestNewSideGamesStart(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	for _, k := range []string{"fish", "stack", "shell", "skip"} {
		if _, err := s.arcadeStart(id, k); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
}

// A run that sat paused behind a hidden page for an hour pays like any other.
func TestAPausedRunStillPays(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	st, err := s.arcadeStart(id, "flick")
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Hour)
	if d, err := s.arcadeFinish(id, "flick", st.Run, 12, nil); err != nil || d.Paid != 12 {
		t.Errorf("a paused flick: %v %+v", err, d)
	}
	run, err := s.diveStart(id)
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Hour)
	if d, err := s.diveFinish(id, run.Run, 9, 6); err != nil || d.Score != 9 || d.Paid == 0 {
		t.Errorf("a paused dive: %v %+v", err, d)
	}
}

// The disk of the day pays in full a fourth time, under the same cap, and
// it's the same disk for every pond that day.
func TestDiskOfTheDay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	_, other := newPond(t, s)
	key := diskOfDay(dayOf(*clock))
	g, _ := arcadeOf(key)
	play := func(k string, score int) *arcadeDone {
		t.Helper()
		st, err := s.arcadeStart(id, k)
		if err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(30 * time.Second)
		d, err := s.arcadeFinish(id, k, st.Run, score, nil)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	plain := arcadeGames[0]
	if plain.key == key {
		plain = arcadeGames[1]
	}
	one := 4 * g.per
	for i := 1; i <= 5; i++ {
		want := 4
		if i == 5 {
			want = 1
		}
		if d := play(key, one); d.Paid != want {
			t.Errorf("run %d of the disk of the day paid %d, want %d", i, d.Paid, want)
		}
	}
	for i := 1; i <= 4; i++ {
		want := 4
		if i == 4 {
			want = 1
		}
		if d := play(plain.key, 4*plain.per); d.Paid != want {
			t.Errorf("run %d of %s paid %d, want %d", i, plain.key, d.Paid, want)
		}
	}
	days, total := 0, 0
	for _, pid := range []int64{id, other} {
		v, _ := s.view(pid)
		for _, a := range v.Arcade {
			if a.Day {
				days++
				if a.Key != key || a.Full != arcadeFullRuns+1 || a.Cap != g.cap {
					t.Errorf("the disk of the day on the view: %+v", a)
				}
			}
			total++
		}
	}
	if days != 2 || total != 2*len(arcadeGames) {
		t.Errorf("%d disks of the day across two ponds", days)
	}
	seen := map[string]bool{}
	for i := range 120 {
		seen[diskOfDay(dayOf(clock.Add(time.Duration(i)*24*time.Hour)))] = true
	}
	// Bug sweep pays once a day, so it's never the disk.
	if len(seen) != len(arcadeGames)-1 || seen["sweep"] {
		t.Errorf("%d of %d games were ever the disk of the day", len(seen), len(arcadeGames))
	}
}

// The page runs pebble drop's bounces, so the server only holds the score
// to what five pebbles can make and how fast they can fall.
func TestPebbleDropIsBounded(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	g, _ := arcadeOf("drop")
	play := func(score int, secs time.Duration) *arcadeDone {
		t.Helper()
		st, err := s.arcadeStart(id, "drop")
		if err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(secs * time.Second)
		d, err := s.arcadeFinish(id, "drop", st.Run, score, nil)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if d := play(dropMost+1, 60); d.Score != 0 || d.Paid != 0 {
		t.Errorf("more than five in the middle paid: %+v", d)
	}
	if d := play(dropMost, 3); d.Score != 0 || d.Paid != 0 {
		t.Errorf("five pebbles down in three seconds paid: %+v", d)
	}
	start := play(0, 20).Pond.Pebbles
	if d := play(dropMost, 20); d.Paid != dropMost/g.per {
		t.Errorf("a perfect round paid %d, want %d", d.Paid, dropMost/g.per)
	}
	for range 20 {
		play(dropMost, 20)
	}
	v, _ := s.view(id)
	if got := v.Pebbles - start; got != g.cap || g.cap != 40 {
		t.Errorf("a day of perfect rounds paid %d against a cap of %d", got, g.cap)
	}
}

// The pegs sit the same for every pond on a day and move overnight.
func TestPebbleDropDealIsTheDays(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	_, other := newPond(t, s)
	deal := func(pid int64) []int {
		v, _ := s.view(pid)
		for _, a := range v.Arcade {
			if a.Key == "drop" {
				return a.Deal
			}
		}
		return nil
	}
	today := deal(id)
	if len(today) != dropRows || !slices.Equal(today, deal(other)) {
		t.Fatalf("today's rows: %v and %v", today, deal(other))
	}
	for _, dx := range today {
		if dx < -8 || dx > 8 {
			t.Errorf("a row sits %d off, which can push a peg into the next one's place", dx)
		}
	}
	same := 0
	for i := 1; i <= 30; i++ {
		*clock = clock.Add(24 * time.Hour)
		if slices.Equal(today, deal(id)) {
			same++
		}
	}
	if same > 0 {
		t.Errorf("%d of the next thirty days have today's rows", same)
	}
}

// A perfect song pays the day's cap and no more, and a singer out front
// scores a quarter more without the page saying so.
func TestChorusIsBounded(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	g, _ := arcadeOf("chorus")
	play := func(score int, secs time.Duration) *arcadeDone {
		t.Helper()
		st, err := s.arcadeStart(id, "chorus")
		if err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(secs * time.Second)
		d, err := s.arcadeFinish(id, "chorus", st.Run, score, nil)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	var plain, singer uint32
	for seed := uint32(1); plain == 0 || singer == 0; seed++ {
		if genomeOf(seed, nil).Nature == "SINGER" {
			singer = seed
		} else {
			plain = seed
		}
	}
	exec(t, s, `UPDATE frogs SET seed = ? WHERE pond_id = ?`, plain, id)
	if d := play(chorusMost+1, 200); d.Paid != 0 {
		t.Errorf("more than a perfect song paid: %+v", d)
	}
	if d := play(chorusMost, 30); d.Paid != 0 {
		t.Errorf("a perfect song in thirty seconds paid: %+v", d)
	}
	start := play(0, 120).Pond.Pebbles
	if d := play(chorusMost, 121); d.Paid != g.cap || d.Score != chorusMost || d.Singer {
		t.Errorf("a perfect song paid %d for %d, want the cap of %d", d.Paid, d.Score, g.cap)
	}
	for range 10 {
		play(chorusMost, 121)
	}
	if v, _ := s.view(id); v.Pebbles-start != g.cap {
		t.Errorf("a day of perfect songs paid %d against a cap of %d", v.Pebbles-start, g.cap)
	}
	*clock = clock.Add(24 * time.Hour)
	play(0, 120)
	exec(t, s, `UPDATE frogs SET seed = ? WHERE pond_id = ?`, singer, id)
	if d := play(200, 100); d.Score != 250 || !d.Singer || d.Paid != 250/g.per {
		t.Errorf("a singer's 200: %+v", d)
	}
	if d := play(chorusMost, 30); d.Paid != 0 || d.Singer {
		t.Errorf("a singer still has to sing in time: %+v", d)
	}
}

// Pad putt's holes are the same for every pond on a day and new the next,
// and every hole's flag can be reached in its par on the windiest day.
func TestPuttDealIsTheDays(t *testing.T) {
	day := "2026-11-10"
	a, b := puttDeal(day, 2), puttDeal(day, 2)
	if len(a) != puttHoles || !reflect.DeepEqual(a, b) {
		t.Fatalf("one day dealt twice differs")
	}
	if calm := puttDeal(day, 0); !reflect.DeepEqual(calm[3].Pads, a[3].Pads) || calm[3].Wind != [2]int{} {
		t.Errorf("the wind moved the pads or blew on a calm day: %+v", calm[3])
	}
	pars := map[int]int{}
	for d := range 60 {
		day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, d).Format("2006-01-02")
		holes := puttDeal(day, 2)
		if d > 0 && reflect.DeepEqual(holes[0].Pads, a[0].Pads) {
			t.Errorf("%s has the first hole of %s", day, "2026-11-10")
		}
		for i, h := range holes {
			if math.Hypot(float64(h.Wind[0]), float64(h.Wind[1])) > puttGust+1 {
				t.Errorf("%s hole %d blows %v", day, i+1, h.Wind)
			}
			if safe := puttPar(h.Pads, puttSafe); safe < 1 || h.Par < 1 || h.Par > safe {
				t.Errorf("%s hole %d has par %d and takes %d by the safe hop", day, i+1, h.Par, safe)
			}
			for _, p := range h.Pads {
				if p.X-p.R < 0 || p.X+p.R > puttW || p.Y-p.R < 0 || p.Y+p.R > puttH {
					t.Errorf("%s hole %d has a pad off the water: %+v", day, i+1, p)
				}
			}
			pars[h.Par]++
		}
	}
	t.Logf("pars over sixty days: %v", pars)
}

// A run of birdies pays the cap and nothing past it.
func TestPuttIsBounded(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	g, _ := arcadeOf("putt")
	play := func(score int, secs time.Duration) *arcadeDone {
		t.Helper()
		st, err := s.arcadeStart(id, "putt")
		if err != nil {
			t.Fatal(err)
		}
		if len(st.Putt) != puttHoles {
			t.Fatalf("a start dealt %d holes", len(st.Putt))
		}
		*clock = clock.Add(secs * time.Second)
		d, err := s.arcadeFinish(id, "putt", st.Run, score, nil)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if d := play(puttMost+1, 300); d.Paid != 0 {
		t.Errorf("more than nine birdies paid: %+v", d)
	}
	if d := play(puttMost, 5); d.Paid != 0 {
		t.Errorf("nine holes in five seconds paid: %+v", d)
	}
	start := play(0, 60).Pond.Pebbles
	if d := play(puttMost, 60); d.Paid != min(g.cap, puttMost/g.per) {
		t.Errorf("a perfect round paid %d", d.Paid)
	}
	for range 10 {
		play(puttMost, 60)
	}
	if v, _ := s.view(id); v.Pebbles-start != g.cap {
		t.Errorf("a day of perfect rounds paid %d against a cap of %d", v.Pebbles-start, g.cap)
	}
}
