package main

import (
	"errors"
	"testing"
	"time"
)

func publicOf(t *testing.T, s *store, id int64) string {
	t.Helper()
	var code string
	if err := s.r.QueryRow(`SELECT public_id FROM ponds WHERE id = ?`, id).Scan(&code); err != nil {
		t.Fatal(err)
	}
	return code
}

func TestAStampIsOnceADayAPageAndNeverYourOwn(t *testing.T) {
	s, clock := testStore(t)
	_, a := newPond(t, s)
	_, b := newPond(t, s)
	page := publicOf(t, s, a)
	if st, _ := s.stampsFor(0, page); st.State != "none" {
		t.Errorf("a visitor with no pond sees %q", st.State)
	}
	if st, _ := s.stampsFor(a, page); st.State != "own" {
		t.Errorf("the owner sees %q", st.State)
	}
	if _, err := s.stamp(a, page, "heart"); !errors.Is(err, errOwnPage) {
		t.Errorf("stamped its own page: %v", err)
	}
	if st, _ := s.stampsFor(b, page); st.State != "open" {
		t.Errorf("another pond sees %q", st.State)
	}
	if _, err := s.stamp(b, page, "kiss"); err == nil {
		t.Error("a stamp that isn't one of the six went on")
	}
	if st, err := s.stamp(b, page, "star"); err != nil || st.State != "done" || st.Stamp != "star" {
		t.Fatalf("stamping: %+v %v", st, err)
	}
	if _, err := s.stamp(b, page, "leaf"); !errors.Is(err, errStamped) {
		t.Errorf("a second stamp the same day: %v", err)
	}
	if st, _ := s.stampsFor(b, page); st.State != "done" || st.Stamp != "star" {
		t.Errorf("after stamping the page says %+v", st)
	}

	// The owner sees who, once.
	*clock = clock.Add(3 * 24 * time.Hour)
	g, _ := s.greet(a, false)
	bv, _ := s.view(b)
	if len(g.Stamps) != 1 || g.Stamps[0].Stamp != "star" || g.Stamps[0].From == nil || g.Stamps[0].From.Seed != bv.Frog.Seed {
		t.Fatalf("the owner's greeting: %+v", g.Stamps)
	}
	if g, _ := s.greet(a, false); len(g.Stamps) != 0 {
		t.Error("the same stamp came twice")
	}

	// A new day is a new stamp, and a frog kept home stamps without a face.
	if _, err := s.keepHome(b, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.stamp(b, page, "wave"); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.greet(a, false); len(g.Stamps) != 1 || g.Stamps[0].From != nil {
		t.Errorf("a stamp from a pond kept home: %+v", g.Stamps)
	}
}

func TestAPondCanOnlyStampSoManyPagesADay(t *testing.T) {
	s, clock := testStore(t)
	_, me := newPond(t, s)
	var pages []string
	for range stampsADay + 2 {
		_, id := newPond(t, s)
		pages = append(pages, publicOf(t, s, id))
	}
	for i, page := range pages {
		_, err := s.stamp(me, page, "fly")
		if i < stampsADay && err != nil {
			t.Fatalf("stamp %d: %v", i, err)
		}
		if i >= stampsADay && !errors.Is(err, errStampsDone) {
			t.Fatalf("stamp %d went on past the day's %d: %v", i, stampsADay, err)
		}
	}
	if st, _ := s.stampsFor(me, pages[len(pages)-1]); st.State != "spent" {
		t.Errorf("past the limit the page says %q", st.State)
	}
	*clock = clock.Add(24 * time.Hour)
	if _, err := s.stamp(me, pages[len(pages)-1], "fly"); err != nil {
		t.Errorf("a new day didn't open stamping again: %v", err)
	}
}

func TestAPageHoldsSoManyStampsAndTheSweepClearsThem(t *testing.T) {
	s, clock := testStore(t)
	_, page := newPond(t, s)
	code := publicOf(t, s, page)
	for i := range stampsHeld {
		exec(t, s, `INSERT INTO ponds (key_hmac, public_id, created_at, last_seen_at, last_gift_at, pebbles, free_spins, spin_at)
			VALUES (?, ?, 0, 0, 0, 0, 0, 0)`, "k"+string(rune('A'+i)), "x"+string(rune('A'+i)))
		exec(t, s, `INSERT INTO stamps (page_id, from_id, day, stamp) VALUES (?, last_insert_rowid(), ?, 0)`, page, dayOf(*clock))
	}
	_, me := newPond(t, s)
	if _, err := s.stamp(me, code, "heart"); !errors.Is(err, errStampsFull) {
		t.Fatalf("a full page took another: %v", err)
	}
	*clock = clock.Add(40 * 24 * time.Hour)
	if _, err := s.sweep(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM stamps`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d stamps after a month unseen", n)
	}
}
