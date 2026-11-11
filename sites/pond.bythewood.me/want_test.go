package main

import (
	"database/sql"
	"errors"
	"net/http"
	"testing"
)

// spares puts n of a found hat in the pond, one to keep and the rest to sell.
func spares(t *testing.T, s *store, id int64, h hat, n int) {
	t.Helper()
	if _, err := s.w.Exec(`INSERT INTO hats (pond_id, shape, colour, count) VALUES (?, ?, ?, ?)`, id, h.Shape, h.Colour, n+1); err != nil {
		t.Fatal(err)
	}
}

func TestTheWantedBarAndPriceByTier(t *testing.T) {
	bars := [tierCount]int{30, 90, 240, 750, 2400}
	prices := [tierCount]int{120, 350, 900, 5000, 12000}
	for tier := range tierCount {
		if wantBar(tier) != bars[tier] || wrenSells[tier] != prices[tier] {
			t.Errorf("a tier %d hat takes %d in spares and costs %d", tier, wantBar(tier), wrenSells[tier])
		}
		// A bar is a handful of spares, and the hat costs more than its bar.
		if wantBar(tier) >= wrenSells[tier] || wantBar(tier) != 6*wrenPays(hatsByTier[tier][0]) {
			t.Errorf("tier %d: a bar of %d against a price of %d", tier, wantBar(tier), wrenSells[tier])
		}
	}
}

func TestAWantedHatFillsFromSparesAndGoesOnTheShelf(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	wanted, spare := hatsByTier[2][0], hatsByTier[2][1]
	spares(t, s, id, spare, 8)

	v, err := s.want(id, wanted)
	if err != nil || v.Want.Hat == nil || v.Want.Hat.hat != wanted || v.Want.Bar != 240 || v.Want.Price != 900 || v.Want.Fill != 0 || v.Want.Ready {
		t.Fatalf("asking for %s: %+v (%v)", wanted.name(), v.Want, err)
	}
	if n := len(v.Shops[shopWren].Lines); n != 4 {
		t.Fatalf("%d lines on the shelf with the bar empty", n)
	}
	for i := range 6 {
		if v, err = s.sell(id, spare); err != nil {
			t.Fatal(err)
		}
		if want := (i + 1) * wrenPays(spare); v.Want.Fill != want {
			t.Fatalf("%d spares sold and the bar reads %d, want %d", i+1, v.Want.Fill, want)
		}
		if v.Want.Ready != (i == 5) {
			t.Fatalf("%d spares sold and ready is %v", i+1, v.Want.Ready)
		}
	}
	lines := v.Shops[shopWren].Lines
	last := lines[len(lines)-1]
	if len(lines) != 5 || !last.Wanted || last.Slot != wantSlot || *last.Hat != wanted || last.Price != 900 {
		t.Fatalf("the shelf with the bar full: %+v", lines)
	}
	if kinds(v.Todo)["wanted"] {
		t.Error("the strip points at a hat the purse can't cover")
	}

	if _, err := s.buy(id, shopWren, wantSlot); !errors.Is(err, errPoor) {
		t.Fatalf("bought a 900 pebble hat on %d: %v", v.Pebbles, err)
	}
	// One more spare goes over the bar and isn't lost.
	if v, err = s.sell(id, spare); err != nil || v.Want.Fill != 280 {
		t.Fatalf("a seventh spare: %d (%v)", v.Want.Fill, err)
	}
	give(t, s, id, 1000)
	if v, _ = s.view(id); !kinds(v.Todo)["wanted"] {
		t.Errorf("nothing on the strip for a wanted hat on the shelf: %+v", v.Todo)
	}
	before := v.Pebbles
	b, err := s.buy(id, shopWren, wantSlot)
	if err != nil {
		t.Fatal(err)
	}
	v = b.Pond
	if v.Pebbles != before-900 || v.Want.Hat != nil || v.Want.Fill != 40 || len(v.Shops[shopWren].Lines) != 4 {
		t.Errorf("after buying it: %d pebbles, %+v", v.Pebbles, v.Want)
	}
	if countOf(v, wanted) != 1 {
		t.Errorf("the pond has %d of %s", countOf(v, wanted), wanted.name())
	}
	if _, err := s.buy(id, shopWren, wantSlot); !errors.Is(err, errNotHeld) {
		t.Errorf("bought it twice: %v", err)
	}
}

func countOf(v *view, h hat) int {
	for _, x := range v.Hats {
		if x.hat == h {
			return x.Count
		}
	}
	return 0
}

// Spares count whether or not a hat's been asked for, and changing the one
// asked for keeps what they've added up to.
func TestTheBarIsNeverLost(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	spare := hatsByTier[3][0]
	spares(t, s, id, spare, 3)
	v, err := s.sell(id, spare)
	if err != nil || v.Want.Fill != 125 || v.Want.Hat != nil {
		t.Fatalf("a spare sold with nothing asked for: %+v (%v)", v.Want, err)
	}
	common, legend := hatsByTier[0][0], hatsByTier[4][0]
	if v, _ = s.want(id, legend); v.Want.Fill != 125 || v.Want.Bar != 2400 || v.Want.Price != 12000 || v.Want.Ready {
		t.Errorf("asking for a legendary: %+v", v.Want)
	}
	if v, _ = s.want(id, common); v.Want.Fill != 125 || v.Want.Bar != 30 || !v.Want.Ready {
		t.Errorf("changing to a common: %+v", v.Want)
	}
	if v, _ = s.want(id, hat{}); v.Want.Hat != nil || v.Want.Fill != 125 {
		t.Errorf("asking for nothing: %+v", v.Want)
	}
	// Finding it some other way lets go of it and keeps the bar.
	if _, err := s.want(id, common); err != nil {
		t.Fatal(err)
	}
	err = s.write(func(tx *sql.Tx) error {
		p, err := loadPond(tx, id)
		if err != nil {
			return err
		}
		if err := award(tx, p, &prize{Hat: &common}); err != nil {
			return err
		}
		return savePond(tx, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ = s.view(id); v.Want.Hat != nil || v.Want.Fill != 125 || len(v.Shops[shopWren].Lines) != 4 {
		t.Errorf("after finding it in a box: %+v", v.Want)
	}
}

func TestOnlyAnUnfoundBoxHatCanBeWanted(t *testing.T) {
	b, s, _ := testSite(t)
	b.post("/api/greet", "{}", nil)
	var id int64
	if err := s.r.QueryRow(`SELECT id FROM ponds`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, h := range []hat{hoodFor(0), siteHatFor(0), {Shape: 99}, {Shape: 1, Colour: 9}} {
		if _, err := s.want(id, h); !errors.Is(err, errNotHeld) {
			t.Errorf("asked for %+v: %v", h, err)
		}
	}
	if code := b.post("/api/hats/want", `{"shape":101,"colour":0}`, nil); code != http.StatusBadRequest {
		t.Errorf("asking for a hood over the wire: %d", code)
	}
	if code := b.post("/api/hats/want", `{"shape":201,"colour":0}`, nil); code != http.StatusBadRequest {
		t.Errorf("asking for a site's hat over the wire: %d", code)
	}
	have := hatsByTier[1][0]
	spares(t, s, id, have, 0)
	if _, err := s.want(id, have); !errors.Is(err, errHaveIt) {
		t.Errorf("asked for a hat it has: %v", err)
	}
	var v view
	if code := b.post("/api/hats/want", `{"shape":3,"colour":3}`, &v); code != http.StatusOK || v.Want.Hat == nil || v.Want.Hat.Tier != 4 {
		t.Errorf("asking for a golden crown: %d %+v", code, v.Want)
	}
}

// The wanted hat is the only one of its kind in the pond, so it can't be
// sold on, and at every tier it costs more than a spare could ever fetch.
func TestAWantedHatCantBeTurnedIntoPebbles(t *testing.T) {
	for tier, hats := range hatsByTier {
		h := hats[0]
		w, _ := worthOf(hatKind(h))
		if top := stallCap(hatKind(h), w); top >= wrenSells[tier] || wrenPays(h) >= wrenSells[tier] {
			t.Errorf("a tier %d hat costs %d, the stall takes up to %d and Little Tully pays %d", tier, wrenSells[tier], top, wrenPays(h))
		}
	}
	s, _ := testStore(t)
	_, id := newPond(t, s)
	wanted := hatsByTier[4][0]
	if _, err := s.w.Exec(`UPDATE ponds SET pebbles = 12000, want_fill = 2400, want_hat = ?`, hatKind(wanted)); err != nil {
		t.Fatal(err)
	}
	b, err := s.buy(id, shopWren, wantSlot)
	if err != nil || b.Pond.Pebbles != 0 || b.Pond.Want.Fill != 0 {
		t.Fatalf("buying a legendary: %v", err)
	}
	if _, err := s.sell(id, wanted); !errors.Is(err, errNotHeld) {
		t.Errorf("sold it back: %v", err)
	}
	if _, err := s.stallList(id, hatKind(wanted), 560); !errors.Is(err, errNotHeld) {
		t.Errorf("put it on the stall: %v", err)
	}
	if _, err := s.wizard(id, hatKind(wanted)); !errors.Is(err, errNotHeld) {
		t.Errorf("took it to the wizard: %v", err)
	}
	if v, _ := s.view(id); v.Pebbles != 0 || countOf(v, wanted) != 1 {
		t.Errorf("the pond has %d pebbles and %d of the hat", v.Pebbles, countOf(v, wanted))
	}
}
