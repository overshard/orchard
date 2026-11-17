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
// hour day covers about a sixth of the card, and an hour in doesn't look like a
// finished day.
func TestBuildSparkDrawsAgainstTheWholeDay(t *testing.T) {
	et := easternTime()
	start := time.Date(2026, 8, 31, 4, 0, 0, 0, et)

	var closes []float64
	var times []int64
	for i := range 49 {
		closes = append(closes, float64(100+i))
		times = append(times, start.Add(time.Duration(i)*5*time.Minute).Unix())
	}

	s := buildSpark(closes, times, 100, barAxis(times))
	if !s.Partial {
		t.Error("four hours into the day is a partial day")
	}
	// Four hours of a sixteen hour day.
	if s.Span < 24 || s.Span > 26 {
		t.Errorf("span = %v, want about 25 across the card", s.Span)
	}
	// 9:30 and 4pm on a 4am to 8pm day.
	if !s.Session || s.Open < 34 || s.Open > 35 || s.Bell != 75 {
		t.Errorf("session band %v to %v, want about 34.4 to 75", s.Open, s.Bell)
	}
}

func TestBuildSparkFillsTheCardOnAFinishedDay(t *testing.T) {
	et := easternTime()
	start := time.Date(2026, 8, 31, 4, 0, 0, 0, et)

	var closes []float64
	var times []int64
	for i := range 193 {
		closes = append(closes, float64(100+i%7))
		times = append(times, start.Add(time.Duration(i)*5*time.Minute).Unix())
	}

	s := buildSpark(closes, times, 100, barAxis(times))
	if s.Partial {
		t.Errorf("a full day should fill the card, span = %v", s.Span)
	}
}

func TestDayAxisRunsFourToEight(t *testing.T) {
	et := easternTime()
	axis := dayAxis(time.Date(2026, 8, 31, 4, 0, 0, 0, et))

	for _, c := range []struct {
		name string
		got  int64
		want time.Time
	}{
		{"start", axis.start, time.Date(2026, 8, 31, 4, 0, 0, 0, et)},
		{"open", axis.open, time.Date(2026, 8, 31, 9, 30, 0, 0, et)},
		{"bell", axis.bell, time.Date(2026, 8, 31, 16, 0, 0, 0, et)},
		{"end", axis.end, time.Date(2026, 8, 31, 20, 0, 0, 0, et)},
	} {
		if c.got != c.want.Unix() {
			t.Errorf("%s at %v, want %v", c.name, time.Unix(c.got, 0).In(et), c.want)
		}
	}
}

// The day rolls at 4am and not at midnight, so a bar printed at 2am belongs to
// the evening before.
func TestDayStartRollsAtFour(t *testing.T) {
	et := easternTime()
	cases := []struct{ at, want string }{
		{"2026-08-31 03:59", "2026-08-30 04:00"},
		{"2026-08-31 04:00", "2026-08-31 04:00"},
		{"2026-08-31 23:59", "2026-08-31 04:00"},
		{"2026-09-01 02:00", "2026-08-31 04:00"},
	}
	for _, c := range cases {
		in, err := time.ParseInLocation("2006-01-02 15:04", c.at, et)
		if err != nil {
			t.Fatal(err)
		}
		if got := dayStart(in).Format("2006-01-02 15:04"); got != c.want {
			t.Errorf("%s belongs to %s, want %s", c.at, got, c.want)
		}
	}
}

// The index cards hold the last weekday through the weekend, so Saturday and
// Sunday night both show Friday until Monday's pre-market.
func TestEquityDayHoldsFridayOverTheWeekend(t *testing.T) {
	et := easternTime()
	// 2026-08-28 is a Friday.
	friday := time.Date(2026, 8, 28, 4, 0, 0, 0, et)
	for _, at := range []time.Time{
		time.Date(2026, 8, 28, 21, 0, 0, 0, et),
		time.Date(2026, 8, 29, 12, 0, 0, 0, et),
		time.Date(2026, 8, 30, 19, 0, 0, 0, et),
		time.Date(2026, 8, 31, 3, 59, 0, 0, et),
	} {
		if got := equityDay(at); !got.Equal(friday) {
			t.Errorf("%v shows %v, want Friday", at, got.In(et))
		}
	}
	if got := equityDay(time.Date(2026, 8, 31, 4, 0, 0, 0, et)); got.Weekday() != time.Monday {
		t.Errorf("4am Monday shows %v, want Monday", got.In(et))
	}
}

// Clamping yesterday's bars onto x=0 draws a vertical smear up the left of the
// card instead of a line.
func TestDayBarsDropsAnythingBeforeTheDay(t *testing.T) {
	et := easternTime()
	start := time.Date(2026, 8, 31, 4, 0, 0, 0, et)

	var closes []float64
	var times []int64
	for i := range 10 {
		closes = append(closes, float64(i))
		times = append(times, start.Add(time.Duration(i-5)*30*time.Minute).Unix())
	}

	kept, kepts, axis := dayBars(closes, times, barAxis(times))
	if !axis.ok {
		t.Fatal("bars inside the day should keep the window")
	}
	if len(kept) != 5 || len(kepts) != 5 {
		t.Fatalf("kept %d bars, want the 5 from 4am on", len(kept))
	}
	if kepts[0] != start.Unix() {
		t.Errorf("first kept bar is %v, want 4am", time.Unix(kepts[0], 0).In(et))
	}
}

// Yahoo dates bitcoin's day by UTC and a future's by its contract, so the close
// each card measures from has to come off the bars instead.
func TestPreviousSessionCloseIsTheLastPrintAtTheCut(t *testing.T) {
	et := easternTime()
	prevClose := time.Date(2026, 8, 30, 16, 0, 0, 0, et)

	var closes []float64
	var times []int64
	for at := prevClose.Add(-3 * time.Hour); at.Before(prevClose.Add(20 * time.Hour)); at = at.Add(30 * time.Minute) {
		closes = append(closes, float64(at.Unix()))
		times = append(times, at.Unix())
	}

	got, found := previousSessionClose(closes, times, lastBell(time.Date(2026, 8, 31, 4, 0, 0, 0, et)))
	if !found {
		t.Fatal("a full previous day should have a close in it")
	}
	if int64(got) != prevClose.Unix() {
		t.Errorf("previous close taken from %v, want %v", time.Unix(int64(got), 0).In(et), prevClose)
	}
}

// A future trades until 5pm Friday, so Monday measures from Friday's 4pm and
// not from whatever printed last before Sunday's.
func TestWeekdayBellSkipsTheWeekend(t *testing.T) {
	et := easternTime()
	got := weekdayBell(time.Date(2026, 8, 31, 4, 0, 0, 0, et))
	want := time.Date(2026, 8, 28, 16, 0, 0, 0, et)
	if !got.Equal(want) {
		t.Errorf("Monday measures from %v, want Friday 4pm", got.In(et))
	}
}

// 4pm is 4pm on both sides of a daylight saving change, so the cut is built in
// Eastern instead of by subtracting a flat 24 hours.
func TestLastBellHoldsAcrossDaylightSaving(t *testing.T) {
	et := easternTime()
	// The Sunday US clocks go back in 2026.
	got := lastBell(time.Date(2026, 11, 2, 7, 0, 0, 0, et))
	want := time.Date(2026, 11, 1, 16, 0, 0, 0, et)
	if !got.Equal(want) {
		t.Errorf("cut at %v, want %v", got.In(et), want)
	}
}

func TestPreviousSessionCloseMissingIsReported(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)
	times := []int64{open.Unix(), open.Add(time.Hour).Unix()}

	if _, found := previousSessionClose([]float64{1, 2}, times, lastBell(open)); found {
		t.Error("bars that all fall after the cut carry no previous close")
	}
}
