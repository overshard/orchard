package main

import (
	"errors"
	"testing"
	"time"
)

func TestBurrowsTakeLodgersAndPayRent(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 300)
	v, err := s.build(id, "burrow")
	if err != nil || v.Bank.Burrows.Level != 1 || v.Pads != startingPads+1 || v.Pebbles != 0 {
		t.Fatalf("first burrow: %+v pads %d %v", v.Bank.Burrows, v.Pads, err)
	}
	if v.PadCost != padCost(startingPads) {
		t.Errorf("the burrow's pad made the next one cost %d", v.PadCost)
	}
	if _, err := s.lodge(id, 0, true); !errors.Is(err, errNotLodge) {
		t.Fatalf("a stranger moved in: %v", err)
	}
	if _, err := s.w.Exec(`INSERT INTO neighbours (pond_id, who, points, gift_day) VALUES (?, 0, 10, ''), (?, 1, 49, '')`, id, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.lodge(id, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.lodge(id, 1, true); !errors.Is(err, errNoBurrow) {
		t.Fatalf("two lodgers in one burrow: %v", err)
	}
	*clock = clock.Add(90 * time.Minute)
	got, err := s.collectRent(id)
	if err != nil || got.Pebbles != 6 || len(got.Prizes) != 0 {
		t.Fatalf("an hour and a half of one lodger: %+v %v", got, err)
	}
	if n := got.Pond.Neighbours.All[0]; n.Points != 11 || !n.Lodger {
		t.Errorf("the lodger has %d points, lodger %v", n.Points, n.Lodger)
	}
	*clock = clock.Add(2 * time.Hour)
	if got, _ := s.collectRent(id); got.Pond.Neighbours.All[0].Points != 11 {
		t.Errorf("a second chat on one day")
	}

	// Moving out pays out what's waiting, so swapping lodgers loses nothing.
	*clock = clock.Add(time.Hour)
	v, err = s.lodge(id, 0, false)
	if err != nil || v.Pebbles != 6+8+4 || len(v.Bank.Burrows.Lodgers) != 0 {
		t.Fatalf("moving out: %d %+v %v", v.Pebbles, v.Bank.Burrows, err)
	}
	if v, _ := s.lodge(id, 1, true); v.Bank.Burrows.Held != 0 {
		t.Errorf("a fresh lodger came with %d rent", v.Bank.Burrows.Held)
	}
	*clock = clock.Add(5 * 24 * time.Hour)
	got, err = s.collectRent(id)
	if err != nil || got.Pebbles != rentRate*8 || len(got.Prizes) != 1 || got.From[0] != 1 {
		t.Fatalf("a full burrow and a heart for the 49 point lodger: %+v %v", got, err)
	}
}

func TestBurrowLevelsCostMaterials(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 1000)
	if _, err := s.build(id, "burrow"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.build(id, "burrow"); !errors.Is(err, errShort) {
		t.Fatalf("level two with no sticks: %v", err)
	}
	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, 'clay', 1000), (?, 'sticks', 1000)`, id, id); err != nil {
		t.Fatal(err)
	}
	for range buildLevels - 1 {
		give(t, s, id, 10000)
		if _, err := s.build(id, "burrow"); err != nil {
			t.Fatal(err)
		}
	}
	give(t, s, id, 10000)
	if _, err := s.build(id, "burrow"); !errors.Is(err, errMaxed) {
		t.Errorf("a sixth burrow: %v", err)
	}
	if v, _ := s.view(id); v.Pads != startingPads+buildLevels || v.PadCost != padCost(startingPads) {
		t.Errorf("five burrows left %d pads and the next at %d", v.Pads, v.PadCost)
	}
}

// Twelve pieces make a painting, four paintings in all, and a visitor and
// every stamp on the frog's page each bring one.
func TestPaintingsFillAPieceAVisitorAndStamp(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	_, other := newPond(t, s)
	page := publicOf(t, s, id)
	done := []int{}
	for day := range 60 {
		*clock = clock.Add(24 * time.Hour)
		if day%2 == 0 {
			if _, err := s.stamp(other, page, "leaf"); err != nil {
				t.Fatal(err)
			}
		}
		before, _ := s.view(id)
		g, err := s.greet(id, false)
		if err != nil {
			t.Fatal(err)
		}
		want := min(before.Bank.Burrows.Paint+1+len(g.Stamps), paintPieces*paintings)
		if got := g.Pond.Bank.Burrows.Paint; got != want {
			t.Fatalf("day %d: %d pieces, want %d", day, got, want)
		}
		if g.Painted >= 0 {
			done = append(done, g.Painted)
			if g.Pond.Bank.Burrows.Paint < (g.Painted+1)*paintPieces {
				t.Errorf("painting %d finished at %d pieces", g.Painted, g.Pond.Bank.Burrows.Paint)
			}
		}
	}
	if len(done) != paintings || done[0] != 0 || done[3] != 3 {
		t.Errorf("paintings finished: %v", done)
	}
}
