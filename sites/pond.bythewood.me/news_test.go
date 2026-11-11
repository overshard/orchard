package main

import (
	"math"
	"testing"
	"time"
)

// bugDays is a week of usual bug catching and then a day before of n.
func bugDays(day string, n int) flowBook {
	b := flowBook{"job:bugs": {}}
	for i := 2; i <= flowWeek+1; i++ {
		b["job:bugs"][dayShift(day, -i)] = &[24]int{12: 200}
	}
	b["job:bugs"][dayShift(day, -1)] = &[24]int{12: n}
	b["job:bugs"][day] = &[24]int{}
	return b
}

func TestTheNewsMovesAPrice(t *testing.T) {
	day := "2026-11-10"
	start, _ := dayBounds(day)
	guild, _ := assetOf("guild")
	if n := bugDays(day, 600).newsOn(day); n == nil || n.Key != "guild" || !n.Up || n.Line != newsLines["guild"][0] {
		t.Fatalf("a record haul: %+v", n)
	}
	if n := bugDays(day, 40).newsOn(day); n == nil || n.Key != "guild" || n.Up {
		t.Fatalf("a dead day: %+v", n)
	}
	if n := bugDays(day, 230).newsOn(day); n != nil {
		t.Fatalf("an ordinary day made the paper: %+v", n)
	}
	// The story moves the share in over the morning, then holds.
	quiet := bugDays(day, 200)
	for _, c := range []struct {
		h    int
		want float64
	}{{0, 0}, {3, newsBump / 2}, {6, newsBump}, {20, newsBump}} {
		at := start.Add(time.Duration(c.h) * time.Hour)
		up, _ := priceAt(guild, flat, bugDays(day, 600), day, 40, at, false)
		same, _ := priceAt(guild, flat, quiet, day, 40, at, false)
		if got := math.Log(up / same); math.Abs(got-c.want) > 0.002 {
			t.Errorf("at %d:00 the news moved the guild %.4f, want %.4f", c.h, got, c.want)
		}
	}
	// A dead day pulls it down, a little less by the close since the week it's
	// weighed against was quieter too.
	down, _ := priceAt(guild, flat, bugDays(day, 40), day, 40, start.Add(23*time.Hour), true)
	same, _ := priceAt(guild, flat, quiet, day, 40, start.Add(23*time.Hour), true)
	if down > same*math.Exp(-newsBump/2) {
		t.Errorf("a dead day closed the guild at %.2f against %.2f", down, same)
	}
	// Buying on the story and selling once it's in loses the fees.
	if newsBump >= 2*shareFee {
		t.Errorf("a story moves a share %.2f and a trade each way costs %.2f", newsBump, 2*shareFee)
	}
}

func TestThePaperRunsTheStory(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	day := dayOf(*clock)
	for i := 2; i <= flowWeek+1; i++ {
		exec(t, s, `INSERT INTO market_flow (key, day, hour, n) VALUES ('job:bugs', ?, 12, 100)`, dayShift(day, -i))
	}
	exec(t, s, `INSERT INTO market_flow (key, day, hour, n) VALUES ('job:bugs', ?, 12, 500)`, dayShift(day, -1))
	lock(t, s)
	v, _ := s.view(id)
	if v.Market.News == nil || v.Market.News.Key != "guild" || v.Gazette.Business != newsLines["guild"][0] {
		t.Errorf("the paper says %q with %+v", v.Gazette.Business, v.Market.News)
	}
}
