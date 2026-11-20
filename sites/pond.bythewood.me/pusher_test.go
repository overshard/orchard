package main

import (
	"errors"
	"testing"
	"time"
)

// pushLayout is a table of n pebbles with the given prizes on it, wherever.
func pushLayout(n int, prizes ...int) []byte {
	out := []byte{}
	for i := range n {
		out = append(out, byte(i*7), byte(40+i%200), 0)
	}
	for _, id := range prizes {
		out = append(out, 128, 60, byte(id))
	}
	return out
}

func heldIDs(t pushTable) []int {
	var out []int
	for _, z := range t.Prizes {
		out = append(out, z.ID)
	}
	return out
}

// pushIn is a report that adds up, with what's on the table after laid out.
func pushIn(t pushTable, dropped, won, spilled, back int, claimed ...int) pushReport {
	gone := map[int]bool{}
	for _, id := range claimed {
		gone[id] = true
	}
	var left []int
	for _, id := range heldIDs(t) {
		if !gone[id] {
			left = append(left, id)
		}
	}
	after := t.On + dropped + back - won - spilled
	return pushReport{Run: t.Run, Dropped: dropped, Won: won, Spilled: spilled, Back: back, Prizes: claimed, Layout: pushLayout(after, left...)}
}

func pushStarted(t *testing.T, s *store, id int64) pushTable {
	t.Helper()
	st, err := s.pushStartRun(id)
	if err != nil {
		t.Fatal(err)
	}
	return *st
}

func TestANewTableStartsWithTheGift(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	st := pushStarted(t, s, id)
	if st.On != pushStart || st.Spill != pushSpill || st.Drops != pushDaily || len(st.Layout) != 0 {
		t.Fatalf("a new table has %d on, %d spilled, %d drops and a %d byte layout", st.On, st.Spill, st.Drops, len(st.Layout))
	}
	if len(st.Prizes) != 1 || st.Prizes[0].Ripe != pushRipe {
		t.Fatalf("a new table's prizes are %+v, want the day's one sitting %d drops", st.Prizes, pushRipe)
	}
}

func TestAnHonestSavePaysAndKeepsTheTable(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	st := pushStarted(t, s, id)
	before, _ := s.view(id)
	*clock = clock.Add(10 * time.Second)
	in := pushIn(st, 10, 6, 2, 0)
	d, err := s.pushSave(id, in)
	if err != nil || !d.OK {
		t.Fatalf("an honest save didn't take: %v %s", err, d.Why)
	}
	if d.Paid != 6 || d.Pond.Pebbles != before.Pebbles+6 {
		t.Errorf("paid %d and the purse went %d to %d, want 6", d.Paid, before.Pebbles, d.Pond.Pebbles)
	}
	tb := d.Table
	if tb.On != pushStart+2 || tb.Spill != pushSpill+2 || tb.Drops != 10 || string(tb.Layout) != string(in.Layout) || tb.Run == "" || tb.Run == st.Run {
		t.Errorf("after the save the table is %+v", tb)
	}
	if _, err := s.pushSave(id, in); !errors.Is(err, errBadRun) {
		t.Errorf("a run saved twice gave %v", err)
	}
}

// Every way a page can lie about the table saves nothing and spends the run.
func TestAReportThatDoesNotAddUpSavesNothing(t *testing.T) {
	cases := map[string]func(st pushTable) pushReport{
		"more than the table holds": func(st pushTable) pushReport {
			in := pushIn(st, 5, 0, 0, 0)
			in.Won = 20
			return in
		},
		"more drops than the allowance": func(st pushTable) pushReport { return pushIn(st, pushDaily+1, 0, 0, 0) },
		"a shower bigger than the jar":  func(st pushTable) pushReport { return pushIn(st, 4, 0, 0, pushSpill+1) },
		"under the floor":               func(st pushTable) pushReport { return pushIn(st, 10, st.On+10-pushFloor+1, 0, 0) },
		"won more than a push can":      func(st pushTable) pushReport { return pushIn(st, 2, 19, 0, 0) },
		"a prize claimed early":         func(st pushTable) pushReport { return pushIn(st, 10, 3, 0, 0, st.Prizes[0].ID) },
		"a prize that isn't there":      func(st pushTable) pushReport { return pushIn(st, 10, 3, 0, 0, 9) },
		"a layout with one too many": func(st pushTable) pushReport {
			in := pushIn(st, 10, 3, 0, 0)
			in.Layout = append(in.Layout, 1, 1, 0)
			return in
		},
		"a layout missing the prize": func(st pushTable) pushReport {
			in := pushIn(st, 10, 3, 0, 0)
			in.Layout = pushLayout(st.On + 7)
			return in
		},
		"a layout that doesn't read": func(st pushTable) pushReport {
			in := pushIn(st, 10, 3, 0, 0)
			in.Layout = in.Layout[:len(in.Layout)-1]
			return in
		},
		"a count below nothing": func(st pushTable) pushReport { return pushIn(st, 10, 0, -1, 0) },
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			s, clock := testStore(t)
			_, id := newPond(t, s)
			st := pushStarted(t, s, id)
			before, _ := s.view(id)
			*clock = clock.Add(10 * time.Second)
			d, err := s.pushSave(id, bad(st))
			if err != nil {
				t.Fatal(err)
			}
			if d.OK || d.Paid != 0 || d.Pond.Pebbles != before.Pebbles {
				t.Fatalf("saved %v, paid %d", d.OK, d.Paid)
			}
			if d.Table.On != st.On || d.Table.Drops != st.Drops || d.Table.Spill != st.Spill || d.Table.Run != "" {
				t.Errorf("the table moved on a bad save, %+v", d.Table)
			}
			if _, err := s.pushSave(id, pushIn(st, 0, 0, 0, 0)); !errors.Is(err, errBadRun) {
				t.Errorf("the run wasn't spent, %v", err)
			}
		})
	}
}

func TestDropsCantComeFasterThanAHand(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	st := pushStarted(t, s, id)
	*clock = clock.Add(3 * time.Second)
	if d, _ := s.pushSave(id, pushIn(st, 20, 0, 0, 0)); d.OK {
		t.Error("twenty drops saved in three seconds")
	}
	st = pushStarted(t, s, id)
	*clock = clock.Add(3 * time.Second)
	if d, _ := s.pushSave(id, pushIn(st, 10, 0, 0, 0)); !d.OK {
		t.Errorf("ten drops in four seconds didn't save, %s", d.Why)
	}
}

func TestAPrizeComesOffOnceItsSatLongEnough(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	st := pushStarted(t, s, id)
	z := st.Prizes[0]
	for range 2 {
		*clock = clock.Add(24 * time.Hour)
		st = pushStarted(t, s, id)
		*clock = clock.Add(10 * time.Second)
		d, err := s.pushSave(id, pushIn(st, 20, 20, 0, 0))
		if err != nil || !d.OK {
			t.Fatalf("%v %s", err, d.Why)
		}
	}
	st = pushStarted(t, s, id)
	if st.Prizes[0].Ripe != 0 {
		t.Fatalf("after 40 drops the first prize still has %d to sit", st.Prizes[0].Ripe)
	}
	*clock = clock.Add(10 * time.Second)
	d, err := s.pushSave(id, pushIn(st, 1, 1, 0, 0, z.ID))
	if err != nil || !d.OK {
		t.Fatalf("%v %s", err, d.Why)
	}
	if len(d.Got) != 1 || d.Got[0].Item != z.Kind {
		t.Errorf("got %+v, want the %s", d.Got, z.Kind)
	}
	for _, left := range d.Table.Prizes {
		if left.ID == z.ID {
			t.Error("the prize is still on the table after it came off")
		}
	}
}

func TestTheAllowanceTopsUpAndStopsAtSixty(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if st := pushStarted(t, s, id); st.Drops != pushDaily {
		t.Fatalf("day one has %d drops", st.Drops)
	}
	*clock = clock.Add(24 * time.Hour)
	if st := pushStarted(t, s, id); st.Drops != 2*pushDaily {
		t.Errorf("a day later there are %d drops, want %d", st.Drops, 2*pushDaily)
	}
	pushStarted(t, s, id)
	if st := pushStarted(t, s, id); st.Drops != 2*pushDaily {
		t.Errorf("starting again the same day made %d drops", st.Drops)
	}
	*clock = clock.Add(5 * 24 * time.Hour)
	v, _ := s.view(id)
	if v.Pusher.Drops != pushKeep || !v.Pusher.Full {
		t.Errorf("the view after five days away says %d drops, full %v", v.Pusher.Drops, v.Pusher.Full)
	}
	if st := pushStarted(t, s, id); st.Drops != pushKeep {
		t.Errorf("five days away gives %d drops, want %d", st.Drops, pushKeep)
	}
}

// A page that always claims the most a save allows can only take what the
// allowance put on the table, plus the start, a bit sooner than the pile
// would give it up.
func TestThePusherCantPayMoreThanItsGiven(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	paid, prizes := 0, 0
	const days = 90
	for range days {
		for range 2 {
			st := pushStarted(t, s, id)
			*clock = clock.Add(time.Minute)
			var claim []int
			for _, z := range st.Prizes {
				if z.Ripe <= st.Drops {
					claim = append(claim, z.ID)
				}
			}
			dropped := st.Drops
			won := min(3*dropped+12, st.On+dropped+st.Spill-pushFloor)
			in := pushIn(st, dropped, won, 0, st.Spill, claim...)
			d, err := s.pushSave(id, in)
			if err != nil {
				t.Fatal(err)
			}
			if !d.OK {
				t.Fatalf("the greediest save that adds up didn't take, %s", d.Why)
			}
			paid += d.Paid
			prizes += len(d.Got)
		}
		*clock = clock.Add(24*time.Hour - 2*time.Minute)
	}
	most := pushStart + pushSpill + (days+1)*pushDaily
	t.Logf("a greedy page took %d pebbles and %d prizes in %d days, the most it could is %d", paid, prizes, days, most)
	if paid > most {
		t.Errorf("the pusher paid %d in %d days, over the %d it was given", paid, days, most)
	}
	if prizes > days {
		t.Errorf("%d prizes in %d days", prizes, days)
	}
}

func TestTheTodoSaysWhenTheDropsAreFull(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	*clock = clock.Add(4 * 24 * time.Hour)
	v, _ := s.view(id)
	for _, d := range todoOf(v) {
		if d.Kind == "pusher" && d.To == "pusher" {
			return
		}
	}
	t.Errorf("no pusher line with %d drops waiting, %+v", v.Pusher.Drops, todoOf(v))
}
