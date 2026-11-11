package main

import (
	"math"
	"testing"
)

// boxWorth is what a box hands back on average over a long run of them, with
// the meter in it, when a thing of each tier is worth what worth says. The
// meter sits at n with the odds of n plain boxes in a row.
func boxWorth(worth func(tier int) float64) float64 {
	plain := float64(boxOdds[0]+boxOdds[1]) / 100
	sum, weight := 0.0, 0.0
	for n := 0; n <= pityAt; n++ {
		at := math.Pow(plain, float64(n))
		for t, o := range boxOdds {
			got := t
			if n == pityAt {
				got = max(t, tierRare)
			}
			sum += at * float64(o) / 100 * worth(got)
		}
		weight += at
	}
	return sum / weight
}

func TestTheMeterCountsAndResets(t *testing.T) {
	plain, sure := 0, 0
	for range 20000 {
		tier, after := boxTier(3)
		if tier < tierRare && after != 4 {
			t.Fatalf("a tier %d box moved the meter from 3 to %d", tier, after)
		}
		if tier >= tierRare && after != 0 {
			t.Fatalf("a tier %d box left the meter at %d", tier, after)
		}
		if tier < tierRare {
			plain++
		}
		if tier, after := boxTier(pityAt); tier < tierRare || after != 0 {
			t.Fatalf("a full meter gave tier %d and read %d after", tier, after)
		} else if tier > tierRare {
			sure++
		}
	}
	if plain < 15000 || plain > 16200 {
		t.Errorf("%d of 20000 boxes were plain, the odds say 15600", plain)
	}
	// The sure box is still an epic or a legendary as often as any other.
	if want := 200 * (boxOdds[3] + boxOdds[4]); sure < want*8/10 || sure > want*12/10 {
		t.Errorf("%d of 20000 sure boxes were better than rare, the odds say %d", sure, want)
	}
	if v := pityOf(0); v.In != pityAt+1 || v.At != pityAt {
		t.Errorf("an empty meter: %+v", v)
	}
	if v := pityOf(pityAt); v.In != 1 || v.Count != pityAt {
		t.Errorf("a full meter: %+v", v)
	}
}

// The meter makes rares a little more common, so the sums that keep a box
// from paying for itself have to hold with it counted.
func TestTheMeterAgreesWithItsSums(t *testing.T) {
	const boxes = 400000
	rare, pity := 0, 0
	for range boxes {
		var tier int
		tier, pity = boxTier(pity)
		if tier >= tierRare {
			rare++
		}
	}
	want := boxWorth(func(tier int) float64 {
		if tier >= tierRare {
			return 1
		}
		return 0
	})
	got := float64(rare) / boxes
	t.Logf("%.2f%% of boxes are a rare or better with the meter, %d%% without", 100*want, boxOdds[2]+boxOdds[3]+boxOdds[4])
	if math.Abs(got-want) > 0.004 {
		t.Errorf("%.4f of boxes were a rare or better, the sums say %.4f", got, want)
	}
}

func TestBothBoxesKeepAMeter(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.w.Exec(`UPDATE ponds SET pebbles = 100000, hat_pity = ?, trinket_pity = ?`, pityAt, pityAt-1); err != nil {
		t.Fatal(err)
	}
	v, _ := s.view(id)
	if v.Box.Pity.In != 1 || v.Trinkets.Pity.In != 2 {
		t.Fatalf("the meters read %+v and %+v", v.Box.Pity, v.Trinkets.Pity)
	}
	out, err := s.box(id)
	if err != nil || out.Prize.HatTier < tierRare || out.Pond.Box.Pity.Count != 0 {
		t.Fatalf("a full hat meter gave %+v and reads %+v: %v", out.Prize, out.Pond.Box.Pity, err)
	}
	if out.Pond.Trinkets.Pity.Count != pityAt-1 {
		t.Errorf("a hat box moved the trinket meter to %d", out.Pond.Trinkets.Pity.Count)
	}
	before := pityAt - 1
	for range 60 {
		out, err := s.trinketBox(id)
		if err != nil {
			t.Fatal(err)
		}
		want := before + 1
		if out.Prize.HatTier >= tierRare {
			want = 0
		}
		if before == pityAt && out.Prize.HatTier < tierRare {
			t.Fatalf("a full trinket meter gave a tier %d", out.Prize.HatTier)
		}
		if got := out.Pond.Trinkets.Pity.Count; got != want {
			t.Fatalf("the trinket meter went from %d to %d on a tier %d", before, got, out.Prize.HatTier)
		}
		before = want
	}
	// A hat from anywhere else leaves the meter alone.
	if _, err := s.w.Exec(`UPDATE ponds SET hat_pity = 4`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.spin(id, false); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.view(id); v.Box.Pity.Count != 4 {
		t.Errorf("a spin moved the hat meter to %d", v.Box.Pity.Count)
	}
}
