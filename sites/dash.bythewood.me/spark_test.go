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

	s := buildSpark(closes, times, 100, sessionAxis(times))
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

	s := buildSpark(closes, times, 100, sessionAxis(times))
	if s.Partial {
		t.Errorf("a full session should fill the card, span = %v", s.Span)
	}
}

// Every card is on the New York trading day now, so a future and bitcoin lay
// their bars over the same 9:30 to 16:00 window a cash index does. They used to
// take the full width, which made an hour of bitcoin look like a whole day
// beside an hour of the S&P.
func TestSessionAxisIsTheSameForEveryInstrument(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)

	var times []int64
	for i := range 13 {
		times = append(times, open.Add(time.Duration(i)*5*time.Minute).Unix())
	}

	axis := sessionAxis(times)
	if !axis.ok {
		t.Fatal("a morning of bars should have a session to draw against")
	}
	if got := time.Unix(axis.start, 0).In(et); !got.Equal(open) {
		t.Errorf("axis opens at %v, want %v", got, open)
	}
	if got := time.Unix(axis.end, 0).In(et); !got.Equal(open.Add(regularHours)) {
		t.Errorf("axis shuts at %v, want 16:00", got)
	}
}

// The trading day rolls at the open and not at midnight, so a bar printed at 2am
// belongs to the session that started the morning before.
func TestSessionStartRollsAtTheOpen(t *testing.T) {
	et := easternTime()
	cases := []struct{ at, want string }{
		{"2026-08-31 09:29", "2026-08-30 09:30"},
		{"2026-08-31 09:30", "2026-08-31 09:30"},
		{"2026-08-31 23:59", "2026-08-31 09:30"},
		{"2026-09-01 02:00", "2026-08-31 09:30"},
	}
	for _, c := range cases {
		in, err := time.ParseInLocation("2006-01-02 15:04", c.at, et)
		if err != nil {
			t.Fatal(err)
		}
		if got := sessionStart(in).Format("2006-01-02 15:04"); got != c.want {
			t.Errorf("%s belongs to session %s, want %s", c.at, got, c.want)
		}
	}
}

// The VIX prints from 3:15am and those bars used to be clamped onto x=0, which
// drew a vertical smear up the left of the card instead of a line.
func TestSessionBarsDropsAnythingBeforeTheOpen(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)

	var closes []float64
	var times []int64
	for i := range 10 {
		closes = append(closes, float64(i))
		times = append(times, open.Add(time.Duration(i-5)*30*time.Minute).Unix())
	}

	kept, kepts, axis := sessionBars(closes, times, sessionAxis(times))
	if !axis.ok {
		t.Fatal("bars inside the session should keep the window")
	}
	if len(kept) != 5 || len(kepts) != 5 {
		t.Fatalf("kept %d bars, want the 5 from the open on", len(kept))
	}
	if kepts[0] != open.Unix() {
		t.Errorf("first kept bar is %v, want the open", time.Unix(kepts[0], 0).In(et))
	}
}

// Futures reopen at 6pm Sunday, hours before the Monday open their session
// belongs to, so there is nothing to lay over Sunday's window and they take the
// full width the way everything used to.
func TestSessionBarsFallsBackWhenNothingPrintedInTheSession(t *testing.T) {
	et := easternTime()
	// 2026-08-30 is a Sunday.
	reopen := time.Date(2026, 8, 30, 18, 0, 0, 0, et)

	var closes []float64
	var times []int64
	for i := range 12 {
		closes = append(closes, float64(i))
		times = append(times, reopen.Add(time.Duration(i)*5*time.Minute).Unix())
	}

	if _, _, axis := sessionBars(closes, times, sessionAxis(times)); axis.ok {
		t.Error("an evening reopen has no session box, so it should draw full width")
	}
}

// Yahoo dates bitcoin's day by UTC and a future's by its contract, so the close
// each card measures from has to come off the bars instead.
func TestPreviousSessionCloseIsFourPMTheDayBefore(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)
	prevClose := time.Date(2026, 8, 30, 16, 0, 0, 0, et)

	var closes []float64
	var times []int64
	// Every half hour across the previous day and into the session.
	for at := prevClose.Add(-3 * time.Hour); at.Before(open.Add(time.Hour)); at = at.Add(30 * time.Minute) {
		closes = append(closes, float64(at.Unix()))
		times = append(times, at.Unix())
	}

	got, found := previousSessionClose(closes, times, open)
	if !found {
		t.Fatal("a full previous day should have a close in it")
	}
	if int64(got) != prevClose.Unix() {
		t.Errorf("previous close taken from %v, want %v",
			time.Unix(int64(got), 0).In(et), prevClose)
	}
}

func TestPreviousSessionCloseMissingIsReported(t *testing.T) {
	et := easternTime()
	open := time.Date(2026, 8, 31, 9, 30, 0, 0, et)
	times := []int64{open.Unix(), open.Add(time.Hour).Unix()}

	if _, found := previousSessionClose([]float64{1, 2}, times, open); found {
		t.Error("bars that all fall inside the session carry no previous close")
	}
}
