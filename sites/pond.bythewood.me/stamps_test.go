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

func TestTheStampBookKeepsTheLatestFromEachPond(t *testing.T) {
	s, clock := testStore(t)
	_, me := newPond(t, s)
	page := publicOf(t, s, me)
	_, a := newPond(t, s)
	_, b := newPond(t, s)
	if _, err := s.stamp(me, publicOf(t, s, a), "fly"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.stamp(a, page, "heart"); err != nil {
		t.Fatal(err)
	}
	if bk, _ := s.book(me); len(bk.Stamps) != 0 || bk.Left != stampsADay-1 {
		t.Fatalf("before the greeting the book has %+v", bk)
	}
	*clock = clock.Add(24 * time.Hour)
	if _, err := s.stamp(a, page, "star"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.stamp(b, page, "wave"); err != nil {
		t.Fatal(err)
	}
	g, err := s.greet(me, false)
	if err != nil {
		t.Fatal(err)
	}
	if !missed(g, "Stamps", "3 stamps on your frog's page, and each brought a piece of the painting") {
		t.Errorf("the away list says %v", g.Missed)
	}
	bk, err := s.book(me)
	if err != nil {
		t.Fatal(err)
	}
	if bk.Ponds != 2 || len(bk.Stamps) != 2 || bk.Left != stampsADay || bk.Paint != g.Pond.Bank.Burrows.Paint {
		t.Fatalf("the book: %+v", bk)
	}
	if bk.Stamps[0].Times != 2 && bk.Stamps[1].Times != 2 {
		t.Errorf("neither pond counts twice: %+v", bk.Stamps)
	}
	for _, st := range bk.Stamps {
		if st.Day != dayOf(*clock) || st.From == nil || (st.Stamp != "star" && st.Stamp != "wave") {
			t.Errorf("a stamp in the book: %+v", st)
		}
	}

	// A fortnight on the stamps are still in the book, and a month on they're
	// gone. A saved key keeps the sweep off the ponds themselves.
	exec(t, s, `UPDATE ponds SET key_saved_at = 1`)
	*clock = clock.Add(14 * 24 * time.Hour)
	if _, err := s.sweep(); err != nil {
		t.Fatal(err)
	}
	if bk, _ := s.book(me); len(bk.Stamps) != 2 {
		t.Errorf("a fortnight on the book has %d", len(bk.Stamps))
	}
	*clock = clock.Add(20 * 24 * time.Hour)
	if _, err := s.sweep(); err != nil {
		t.Fatal(err)
	}
	if bk, _ := s.book(me); len(bk.Stamps) != 0 {
		t.Errorf("a month on the book has %d", len(bk.Stamps))
	}
}

func TestStampsStopBringingPiecesOnceThePaintingsAreDone(t *testing.T) {
	s, clock := testStore(t)
	_, me := newPond(t, s)
	_, a := newPond(t, s)
	exec(t, s, `UPDATE ponds SET paint = ? WHERE id = ?`, paintPieces*paintings, me)
	if _, err := s.stamp(a, publicOf(t, s, me), "leaf"); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Hour)
	g, err := s.greet(me, false)
	if err != nil {
		t.Fatal(err)
	}
	if !missed(g, "Stamps", "1 stamp on your frog's page") {
		t.Errorf("the away list says %v", g.Missed)
	}
}

func TestPiecesSaid(t *testing.T) {
	for _, c := range []struct {
		n, pieces int
		want      string
	}{
		{1, 0, ""},
		{1, 1, ", and it brought a piece of the painting"},
		{4, 4, ", and each brought a piece of the painting"},
		{4, 1, ", and 1 piece of the painting came with them"},
		{4, 3, ", and 3 pieces of the painting came with them"},
	} {
		if got := piecesSaid(c.n, c.pieces); got != c.want {
			t.Errorf("piecesSaid(%d, %d) = %q", c.n, c.pieces, got)
		}
	}
}

func missed(g *greeting, label, line string) bool {
	for _, m := range g.Missed {
		if m[0] == label && m[1] == line {
			return true
		}
	}
	return false
}
