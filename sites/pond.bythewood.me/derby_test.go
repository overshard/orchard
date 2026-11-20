package main

import (
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

var derbyPepper = []byte("a pepper for tests, long enough to pass")

func TestEverySnailHasAnOwner(t *testing.T) {
	if len(snails) != len(residents) || len(residents) != derbyRaces*derbyField {
		t.Fatalf("%d snails for %d residents in %d races of %d", len(snails), len(residents), derbyRaces, derbyField)
	}
	for _, day := range []string{"2026-11-19", "2026-12-25"} {
		d := derbyDraw(derbyPepper, day)
		if s := slices.Sorted(slices.Values(d)); !slices.Equal(s, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}) {
			t.Errorf("%s draws %v, want every snail once", day, d)
		}
	}
}

// The odds come off the same race the pond backs, so a lot of races run
// the way the board said they would.
func TestTheOddsAreTheRace(t *testing.T) {
	for i, day := range []string{"2026-11-19", "2026-11-20", "2026-11-21"} {
		for n := 1; n <= derbyRaces; n++ {
			c := dealCard(derbyPepper, day, n, i)
			if f := c.chance[c.fav()]; f < 0.35 || f > 0.45 {
				t.Errorf("%s race %d: the favourite's chance is %.3f", day, n, f)
			}
			r := rand.New(rand.NewPCG(uint64(i), uint64(n)))
			wins := make([]int, derbyField)
			const runs = 20000
			for range runs {
				wins[runDerby(c, r, false).Win]++
			}
			for k, w := range wins {
				if got := float64(w) / runs; got < c.chance[k]-0.01 || got > c.chance[k]+0.01 {
					t.Errorf("%s race %d: %s won %.3f against %.3f priced", day, n, snails[c.snails[k]].name, got, c.chance[k])
				}
				o := c.odds[k]
				if fair := (1 - c.chance[k]) / c.chance[k]; float64(o[0])/float64(o[1]) > fair+1e-9 {
					t.Errorf("%s at %s is better than fair", snails[c.snails[k]].name, oddsSaid(o))
				}
			}
		}
	}
}

func TestAScriptRunsToTheLine(t *testing.T) {
	c := dealCard(derbyPepper, "2026-11-19", 2, 1)
	r := rand.New(rand.NewPCG(7, 7))
	photos := 0
	for range 500 {
		x := runDerby(c, r, true)
		if x.Ticks != len(x.Pos[0])-1 || x.Order[0] != x.Win {
			t.Fatalf("ticks %d over %d positions, order %v and win %d", x.Ticks, len(x.Pos[0]), x.Order, x.Win)
		}
		if x.Pos[x.Win][x.Ticks] < derbyLine {
			t.Fatalf("the winner stopped at %d", x.Pos[x.Win][x.Ticks])
		}
		for _, p := range x.Pos {
			if !slices.IsSorted(p) {
				t.Fatalf("a snail went backwards: %v", p)
			}
		}
		calls := derbyCalls(c, x, r)
		if calls[0].T != 0 || calls[len(calls)-1].T != x.Ticks {
			t.Fatalf("the calls run %d to %d over %d ticks", calls[0].T, calls[len(calls)-1].T, x.Ticks)
		}
		for _, l := range calls {
			if strings.Contains(l.Line, "{") {
				t.Fatalf("a slot was left in %q", l.Line)
			}
		}
		if x.Photo {
			photos++
		}
	}
	if photos == 0 || photos > 150 {
		t.Errorf("%d photo finishes in 500", photos)
	}
}

func TestTheCommentaryPassesTheNightlyCheck(t *testing.T) {
	seen := map[string]bool{}
	for k, pool := range derbyLines {
		for _, l := range pool {
			ask := taleAsk{kind: "derby", allowed: []string{"{snail}", "{owner}"}}
			if strings.Contains(l, "{snail}") {
				ask.must = "{snail}"
			}
			if _, ok := checkLine(l, ask, seen); !ok {
				t.Errorf("%s: %q", k, l)
			}
		}
	}
	for _, l := range goingSaid {
		if _, ok := checkLine(l, taleAsk{kind: "derby"}, seen); !ok {
			t.Errorf("going: %q", l)
		}
	}
}

func TestTheGoingIsYesterdaysRain(t *testing.T) {
	s, clock := testStore(t)
	day := dayOf(*clock)
	start, _ := time.ParseInLocation("2006-01-02", day, pondZone)
	put := func(at time.Time, wet int) {
		if _, err := s.w.Exec(`INSERT INTO rain (slot, wet) VALUES (?, ?)`, rainSlot(at), wet); err != nil {
			t.Fatal(err)
		}
	}
	if g, _ := goingOf(s.r, day); g != 0 {
		t.Errorf("a dry day is %s", goings[g])
	}
	put(start.Add(-2*time.Hour), 2)
	put(start.Add(time.Hour), 3)
	if g, _ := goingOf(s.r, day); g != 1 {
		t.Errorf("one shower is %s", goings[g])
	}
	for i := range 20 {
		put(start.Add(-time.Duration(3+i)*10*time.Minute-3*time.Hour), 2)
	}
	if g, _ := goingOf(s.r, day); g != 3 {
		t.Errorf("a wet night is %s", goings[g])
	}
}

func TestBackingASnail(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	day := dayOf(*clock)
	v, _ := s.view(id)
	if v.Derby.Race != 1 || len(v.Derby.Card) != derbyField || len(v.Derby.Aims) != 2 {
		t.Fatalf("a new day's derby is %+v", v.Derby)
	}
	if _, err := s.backSnail(id, day, 2, 0); !errors.Is(err, errRaceGone) {
		t.Errorf("backed race 2 before race 1: %v", err)
	}
	tomorrow := dayOf(clock.Add(24 * time.Hour))
	if _, err := s.backSnail(id, tomorrow, 1, 0); !errors.Is(err, errRaceGone) {
		t.Errorf("backed tomorrow's race: %v", err)
	}
	fav := slices.IndexFunc(v.Derby.Card, func(r derbyRunner) bool { return r.Fav })
	b, err := s.backSnail(id, day, 1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if b.Picked || b.Pick != fav {
		t.Errorf("no pick backed lane %d, the favourite is %d", b.Pick, fav)
	}
	if _, err := s.backSnail(id, day, 1, 1); !errors.Is(err, errRaceGone) {
		t.Errorf("backed race 1 twice: %v", err)
	}
	for n := 2; n <= derbyRaces; n++ {
		if _, err := s.backSnail(id, day, n, 3); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.backSnail(id, day, derbyRaces+1, 0); !errors.Is(err, errRaceGone) {
		t.Errorf("backed a fourth race: %v", err)
	}
	v, _ = s.view(id)
	if v.Derby.Race != 0 || len(v.Derby.Card) != 0 || len(v.Derby.Ran) != derbyRaces {
		t.Errorf("after three races the derby is %+v", v.Derby)
	}
	*clock = clock.Add(24 * time.Hour)
	if v, _ = s.view(id); v.Derby.Race != 1 || len(v.Derby.Ran) != 0 {
		t.Errorf("the next day starts at race %d with %d run", v.Derby.Race, len(v.Derby.Ran))
	}
}

// A winning slip pays its odds and the slip back, a losing one nothing, and
// the aims go in on top.
func TestASlipPaysItsOdds(t *testing.T) {
	if slipPays([2]int{1, 1}) != 12 || slipPays([2]int{8, 1}) != 54 || slipPays([2]int{6, 4}) != 15 {
		t.Error("evens, 8 to 1 and 6 to 4 pay 12, 54 and 15")
	}
	s, clock := testStore(t)
	won, lost := 0, 0
	for range 40 {
		_, id := newPond(t, s)
		before, _ := s.view(id)
		b, err := s.backSnail(id, dayOf(*clock), 1, rand.IntN(derbyField))
		if err != nil {
			t.Fatal(err)
		}
		after, _ := s.view(id)
		want := 0
		if b.Script.Win == b.Pick {
			want = slipPays(oddsFor(t, b.Card[b.Pick].Odds))
			won++
		} else {
			lost++
		}
		if b.Won != want || after.Pebbles-before.Pebbles != b.Paid || b.Paid < want {
			t.Fatalf("lane %d won by %d paid %d of %d, the purse moved %d", b.Pick, b.Script.Win, b.Paid, want, after.Pebbles-before.Pebbles)
		}
		if b.Paid > want && len(b.Aims) == 0 && b.Rested == 0 {
			t.Fatalf("paid %d over the slip's %d with no aim", b.Paid, want)
		}
	}
	if won == 0 || lost == 0 {
		t.Errorf("won %d and lost %d of 40", won, lost)
	}
}

func oddsFor(t *testing.T, said string) [2]int {
	for _, o := range derbyLadder {
		if oddsSaid(o) == said {
			return o
		}
	}
	t.Fatalf("no odds of %q", said)
	return [2]int{}
}

// playDerby backs each race left today on a snail picked at random.
func playDerby(t *testing.T, s *store, id int64) {
	t.Helper()
	for {
		v, _ := s.view(id)
		if v.Derby.Race == 0 {
			return
		}
		if _, err := s.backSnail(id, v.Derby.Day, v.Derby.Race, rand.IntN(derbyField)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDerbyAims(t *testing.T) {
	day := "2026-11-19"
	met := func(counts map[string]int) []string {
		var out []string
		for _, a := range aimsOf(day, "derby") {
			if countsFit("derby", 0, counts) && a.met(0, counts) {
				out = append(out, a.key)
			}
		}
		return out
	}
	if got := met(map[string]int{"wins": 1}); len(got) != 0 {
		t.Errorf("one winner met %v", got)
	}
	if got := met(map[string]int{"wins": 1, "long": 1}); !slices.Equal(got, []string{"dy_long"}) {
		t.Errorf("a long shot met %v", got)
	}
	if got := met(map[string]int{"wins": 2}); !slices.Equal(got, []string{"dy_two"}) {
		t.Errorf("two winners met %v", got)
	}
}
