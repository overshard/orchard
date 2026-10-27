package main

import (
	"testing"
	"time"
)

func TestSteamLookupsAreReusedThenExpire(t *testing.T) {
	m := map[int]steamLookup[reviewSummary]{}
	var r reviewSummary
	r.Summary.Total = 400
	steamKeep(m, 730, r)

	got, ok := steamCached(m, 730)
	if !ok || got.Summary.Total != 400 {
		t.Fatalf("a fresh lookup was not reused, got %+v %v", got, ok)
	}

	m[730] = steamLookup[reviewSummary]{at: time.Now().Add(-steamLookupFor - time.Minute), v: r}
	if _, ok := steamCached(m, 730); ok {
		t.Error("a lookup past steamLookupFor was reused")
	}
	steamKeep(m, 440, r)
	if _, ok := m[730]; ok {
		t.Error("an expired lookup was kept after the next write")
	}
}
