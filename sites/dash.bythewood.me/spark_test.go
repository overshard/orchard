package main

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildSparkNeedsTwoPoints(t *testing.T) {
	for _, closes := range [][]float64{nil, {}, {1}} {
		if s := buildSpark(closes, nil, 1, tradingAxis{}); s.Line != "" {
			t.Errorf("%v drew a line, want nothing to draw", closes)
		}
	}
}

func TestBuildSparkKeepsEverythingInsideTheViewBox(t *testing.T) {
	// A previous close well outside the day's range is the case that puts the
	// baseline off the top of the box when it is not scaled in.
	s := buildSpark([]float64{10, 12, 11, 14}, nil, 5, tradingAxis{})
	if !s.HasBase {
		t.Fatal("a previous close should give the card a baseline")
	}
	if s.Baseline < 0 || s.Baseline > sparkHeight {
		t.Errorf("baseline at y=%v, outside 0..%v", s.Baseline, sparkHeight)
	}

	for _, y := range pathYs(t, s.Line) {
		if y < 0 || y > sparkHeight {
			t.Errorf("point at y=%v, outside 0..%v", y, sparkHeight)
		}
	}
}

func TestBuildSparkHandlesAFlatDay(t *testing.T) {
	s := buildSpark([]float64{7, 7, 7, 7}, nil, 7, tradingAxis{})
	if s.Line == "" {
		t.Fatal("a flat day still has a line")
	}
	ys := pathYs(t, s.Line)
	for _, y := range ys {
		if math.IsNaN(y) || math.IsInf(y, 0) {
			t.Fatalf("flat day produced %v, want a real number", y)
		}
	}
	// Every point is the same price, so every point is at the same height.
	for _, y := range ys[1:] {
		if math.Abs(y-ys[0]) > 0.01 {
			t.Errorf("flat day drew a slope: %v", ys)
			break
		}
	}
}

func TestBuildSparkAreaClosesBackToTheFloor(t *testing.T) {
	s := buildSpark([]float64{1, 2, 3}, nil, 1, tradingAxis{})
	if !strings.HasSuffix(s.Area, "Z") {
		t.Errorf("area path %q does not close", s.Area)
	}
	if !strings.Contains(s.Area, "L0,32") {
		t.Errorf("area path %q does not return along the floor", s.Area)
	}
}

// pathYs pulls the y of every point out of an SVG path built by buildSpark.
func pathYs(t *testing.T, d string) []float64 {
	t.Helper()

	var ys []float64
	for _, seg := range strings.Split(strings.NewReplacer("M", " ", "L", " ").Replace(d), " ") {
		_, y, ok := strings.Cut(seg, ",")
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(y, 64)
		if err != nil {
			t.Fatalf("parsing y from %q: %v", seg, err)
		}
		ys = append(ys, v)
	}
	if len(ys) == 0 {
		t.Fatalf("no points in %q", d)
	}
	return ys
}

func TestFormatNumberGroupsThousands(t *testing.T) {
	cases := []struct {
		in       float64
		decimals int
		want     string
	}{
		{7711.76, 2, "7,711.76"},
		{53569.4, 2, "53,569.40"},
		{77295.123, 0, "77,295"},
		{14.43, 2, "14.43"},
		{999, 0, "999"},
		{1000, 0, "1,000"},
		{-1234.5, 2, "-1,234.50"},
		{0, 2, "0.00"},
	}
	for _, c := range cases {
		if got := formatNumber(c.in, c.decimals); got != c.want {
			t.Errorf("formatNumber(%v, %d) = %q, want %q", c.in, c.decimals, got, c.want)
		}
	}
}

// A card whose whole job is direction has to distinguish a zero move from an
// unset one, so the sign is always written.
func TestSignedAlwaysCarriesASign(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1.5, "+1.50"},
		{-1.5, "-1.50"},
		{0, "+0.00"},
		{-1234.5, "-1,234.50"},
	}
	for _, c := range cases {
		if got := signed(c.in, 2); got != c.want {
			t.Errorf("signed(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDirection(t *testing.T) {
	for in, want := range map[float64]string{1: "up", -1: "down", 0: "flat"} {
		if got := direction(in); got != want {
			t.Errorf("direction(%v) = %q, want %q", in, got, want)
		}
	}
}

// The whole session is the axis, so a line drawn an hour into a six and a half
// hour day covers about a sixth of the card. Spacing the points evenly instead
// is what made a card at 9:35 look like a finished day.
func TestBuildSparkDrawsAgainstTheWholeSession(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)

	var closes []float64
	var times []int64
	for i := range 13 {
		closes = append(closes, float64(100+i))
		times = append(times, open.Add(time.Duration(i)*5*time.Minute).Unix())
	}

	s := buildSpark(closes, times, 100, axisFor("^GSPC", times))
	if !s.Partial {
		t.Error("an hour into the session is a partial day")
	}
	// One hour of a 390 minute session, so a bit over 15%.
	if s.Span < 14 || s.Span > 17 {
		t.Errorf("span = %v, want about 15 across the card", s.Span)
	}
}

func TestBuildSparkFillsTheCardOnAFinishedSession(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)

	var closes []float64
	var times []int64
	for i := range 79 {
		closes = append(closes, float64(100+i%7))
		times = append(times, open.Add(time.Duration(i)*5*time.Minute).Unix())
	}

	s := buildSpark(closes, times, 100, axisFor("^GSPC", times))
	if s.Partial {
		t.Errorf("a full session should fill the card, span = %v", s.Span)
	}
}

// Yahoo returns a calendar day for a futures symbol rather than a CME session,
// so there is no window to lay those bars over and they take the full width.
func TestAxisForFuturesHasNoSession(t *testing.T) {
	et := easternTime()
	morning := time.Date(2026, 8, 31, 9, 0, 0, 0, et)
	if axisFor("ES=F", []int64{morning.Unix()}).ok {
		t.Error("ES=F should draw across the full width")
	}
}

// Crypto never closes, so there is no session to leave room for.
func TestAxisForCryptoHasNoSession(t *testing.T) {
	if axisFor("BTC-USD", []int64{time.Now().Unix()}).ok {
		t.Error("BTC-USD should draw across the full width")
	}
}
