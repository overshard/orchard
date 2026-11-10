package main

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func stock(t *testing.T, s *store, id int64, kind string, n int) {
	t.Helper()
	if err := s.write(func(tx *sql.Tx) error {
		for range n {
			if err := giveThing(tx, id, kind); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWizardPoolIsWhole(t *testing.T) {
	for tier, sorts := range wizardPool {
		if len(sorts) == 0 {
			t.Fatalf("nothing comes out at tier %d", tier)
		}
		for _, kinds := range sorts {
			for _, k := range kinds {
				got, ok := wizardTier(k)
				if !ok || got != tier {
					t.Errorf("%s is in tier %d and says %d", k, tier, got)
				}
				if n := thingName(k); n == "" || n == k {
					t.Errorf("%s has no name", k)
				}
				if _, ok := worthOf(k); !ok {
					t.Errorf("%s has no worth", k)
				}
			}
		}
	}
	h := hat{Shape: 8, Colour: 3}
	if got, ok := hatOfKind(hatKind(h)); !ok || got != h {
		t.Errorf("hat kind didn't come back, %v", got)
	}
	for _, bad := range []string{"hat_0_0", "hat_99_1", "hat_1", "hat_x_y"} {
		if _, ok := hatOfKind(bad); ok {
			t.Errorf("%s read as a hat", bad)
		}
	}
}

// Three in, one out, so over many goes the wizard always hands back less
// than it took, whatever tier goes in.
func TestWizardNeverMakesMore(t *testing.T) {
	const n = 40000
	for tier := range tierCount {
		up, same, worthOut := 0, 0, 0.0
		for range n {
			got, kind := rollWizard(tier)
			switch {
			case got > tier:
				up++
			case got == tier:
				same++
			}
			if kind != "" {
				w, _ := worthOf(kind)
				worthOut += float64(w)
			}
		}
		if tier < tierCount-1 {
			if f := float64(up) / n; f < 0.13 || f > 0.17 {
				t.Errorf("tier %d went up %.3f of the time", tier, f)
			}
		}
		avgIn := 0.0
		for _, kinds := range wizardPool[tier] {
			sum := 0
			for _, k := range kinds {
				w, _ := worthOf(k)
				sum += w
			}
			avgIn += float64(sum) / float64(len(kinds)) / float64(len(wizardPool[tier]))
		}
		if out := worthOut / n; out >= 3*avgIn {
			t.Errorf("tier %d puts out %.0f for three worth %.0f", tier, out, 3*avgIn)
		}
	}
}

func TestWizardTakesThreeAndKeepsTheLastHat(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	stock(t, s, id, "agate", 2)
	if _, err := s.wizard(id, "agate"); !errors.Is(err, errNotHeld) {
		t.Fatalf("worked with two, %v", err)
	}
	stock(t, s, id, "agate", 1)
	w, err := s.wizard(id, "agate")
	if err != nil {
		t.Fatal(err)
	}
	if w.Went == "" || (w.Went != "dust" && w.Kind == "") {
		t.Fatalf("came back with %+v", w)
	}
	if w.Pond.held["agate"] != 0 && w.Kind != "agate" {
		t.Fatalf("left %d agate", w.Pond.held["agate"])
	}
	h := hatKind(hat{Shape: 4, Colour: 0})
	stock(t, s, id, h, 3)
	if _, err := s.wizard(id, h); !errors.Is(err, errNotHeld) {
		t.Fatalf("took the last hat, %v", err)
	}
	stock(t, s, id, h, 1)
	if _, err := s.wizard(id, h); err != nil {
		t.Fatal(err)
	}
	if _, err := s.wizard(id, "f_lantern"); !errors.Is(err, errNotHeld) {
		t.Fatalf("magicked the furniture, %v", err)
	}
}

func TestStallSellsAndCollects(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	stock(t, s, id, "fossil", 4)
	w, _ := worthOf("fossil")
	if _, err := s.stallList(id, "fossil", w*4); !errors.Is(err, errBadPrice) {
		t.Fatalf("listed at four times, %v", err)
	}
	for i := range stallSlots {
		if _, err := s.stallList(id, "fossil", w/2+i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.stallList(id, "fossil", w); !errors.Is(err, errStallFull) {
		t.Fatalf("a fourth went up, %v", err)
	}
	v, err := s.stallTake(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v.held["fossil"] != 2 || len(v.Stall.Rows) != stallSlots-1 {
		t.Fatalf("taking it back left %d held and %d up", v.held["fossil"], len(v.Stall.Rows))
	}
	before := v.Pebbles
	if _, err := s.stallCollect(id); !errors.Is(err, errNotYet) {
		t.Fatalf("collected before a sale, %v", err)
	}
	*clock = clock.Add(7 * time.Hour)
	v, err = s.stallCollect(id)
	if err != nil {
		t.Fatal(err)
	}
	if v.Pebbles != before+w/2+1+w/2+2 || len(v.Stall.Rows) != 0 {
		t.Fatalf("pebbles %d from %d, %d still up", v.Pebbles, before, len(v.Stall.Rows))
	}

	h := hatKind(hat{Shape: 4, Colour: 0})
	stock(t, s, id, h, 1)
	hw, _ := worthOf(h)
	if _, err := s.stallList(id, h, hw); !errors.Is(err, errNotHeld) {
		t.Fatalf("sold the only hat, %v", err)
	}
	if _, err := s.stallList(id, "f_lantern", 10); !errors.Is(err, errNotHeld) {
		t.Fatalf("sold the furniture, %v", err)
	}
}

func TestStallTimesFollowThePrice(t *testing.T) {
	for range 200 {
		cheap, fair, dear := sellsIn(50, 100), sellsIn(100, 100), sellsIn(300, 100)
		if cheap > 6*time.Hour || fair < 17*time.Hour || fair > 31*time.Hour || dear < 10*24*time.Hour {
			t.Fatalf("cheap %v, fair %v, dear %v", cheap, fair, dear)
		}
	}
}

func TestStallSparesDontPayForTheBox(t *testing.T) {
	sum := 0.0
	for tier, o := range boxOdds {
		sum += float64(o) / 100 * float64(int(float64(hatRate[tier])*boxedShare))
	}
	if sum >= boxCost || sum >= trinketBoxCost {
		t.Fatalf("a box's spares fetch %.1f on the stall", sum)
	}
}

func TestStallSellsUnderTheShops(t *testing.T) {
	for _, r := range mossRocks {
		if w, _ := worthOf(r.kind); w >= r.price {
			t.Errorf("%s goes for %d on the stall and %d at Moss's", r.kind, w, r.price)
		}
	}
	for _, in := range ingredients {
		if in.trinket {
			continue
		}
		if w, _ := worthOf(in.kind); w >= salePrice(larderPrice[in.tier]) {
			t.Errorf("%s goes for %d on the stall and %d on sale at Brindle's", in.kind, w, salePrice(larderPrice[in.tier]))
		}
	}
	for k, price := range map[string]int{"stamp_spots": stampPrice, "drops_red": dropsPrice, "dye_blue": bargainPrice, "dye_gold": bargainPrice} {
		if w, _ := worthOf(k); w >= price {
			t.Errorf("%s goes for %d on the stall and %d at Moss's", k, w, price)
		}
	}
	for t2, price := range wrenSells[:3] {
		if w := int(float64(hatRate[t2]) * boxedShare); w >= price {
			t.Errorf("a tier %d hat goes for %d on the stall and %d at Wren's", t2, w, price)
		}
	}
}

func TestStallCapsUnderEveryShop(t *testing.T) {
	kinds := []string{}
	for _, sorts := range wizardPool {
		for _, ks := range sorts {
			kinds = append(kinds, ks...)
		}
	}
	for _, k := range kinds {
		w, _ := worthOf(k)
		hi := stallCap(k, w)
		if p, ok := shopFloor(k); ok && hi >= p {
			t.Errorf("%s can go up for %d and a shop sells it for %d", k, hi, p)
		}
	}
	for _, r := range mossRocks {
		if _, ok := shopFloor(r.kind); !ok {
			t.Errorf("%s is at Moss's and has no floor", r.kind)
		}
	}
	s, _ := testStore(t)
	_, id := newPond(t, s)
	stock(t, s, id, "dye_blue", 1)
	if _, err := s.stallList(id, "dye_blue", bargainPrice); !errors.Is(err, errBadPrice) {
		t.Fatalf("a blue dye went up for Moss's bargain price, %v", err)
	}
}
