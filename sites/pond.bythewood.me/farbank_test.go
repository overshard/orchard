package main

import (
	"errors"
	"testing"
)

// A patch starts at 500 and each is about 15% dearer than the last, so the
// thirtieth is a long save for a pond that's built everything.
func TestTheFarBanksPriceClimbs(t *testing.T) {
	if farCost(0) != farStart {
		t.Errorf("the first patch is %d", farCost(0))
	}
	total := 0
	for n := range farDrawn {
		total += farCost(n)
		if n == 0 {
			continue
		}
		if r := float64(farCost(n)) / float64(farCost(n-1)); r < 1.13 || r > 1.17 {
			t.Errorf("patch %d is %.3f times the one before", n+1, r)
		}
	}
	t.Logf("the thirtieth patch costs %d and all thirty %d", farCost(farDrawn-1), total)
	if farCost(farMost-1) <= 0 {
		t.Error("the last patch overflowed")
	}
}

func TestAPatchCostsPebblesAndDoesNothingElse(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.buyFar(id); !errors.Is(err, errPoor) {
		t.Fatalf("a patch on a starter purse: %v", err)
	}
	exec(t, s, `UPDATE ponds SET pebbles = 2000 WHERE id = ?`, id)
	before, _ := s.view(id)
	v, err := s.buyFar(id)
	if err != nil {
		t.Fatal(err)
	}
	if v.Pebbles != 2000-farStart || v.Bank.Far.Patches != 1 || v.Bank.Far.Cost != farCost(1) {
		t.Errorf("after one patch: %d pebbles, %+v", v.Pebbles, v.Bank.Far)
	}
	if v.Share.Power.Total != before.Share.Power.Total {
		t.Errorf("a patch moved power from %d to %d", before.Share.Power.Total, v.Share.Power.Total)
	}
}
