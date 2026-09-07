package tools

import (
	"encoding/json"
	"testing"
)

func chartJSON(t *testing.T, price, prev float64, closes []any, stamps []int64) chartPayload {
	t.Helper()
	body := map[string]any{"chart": map[string]any{"result": []any{map[string]any{
		"meta": map[string]any{"currency": "USD", "symbol": "VTI",
			"regularMarketPrice": price, "chartPreviousClose": prev, "shortName": "Vanguard"},
		"timestamp":  stamps,
		"indicators": map[string]any{"quote": []any{map[string]any{"close": closes}}},
	}}}}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var p chartPayload
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// The intraday chart is the only one measured against yesterday's close. Every
// longer span is measured against its own first bar, because "up 18% this year"
// is a claim about a year ago and not about yesterday.
func TestBuildSeriesBaseline(t *testing.T) {
	closes := []any{100.0, 105.0, 110.0}
	stamps := []int64{1, 2, 3}

	day, err := buildSeries(chartJSON(t, 110, 50, closes, stamps), RangeByKey("1d"), "VTI")
	if err != nil {
		t.Fatal(err)
	}
	if day.Previous != 50 || day.Percent != 120 {
		t.Fatalf("intraday should measure from the previous close: prev=%v pct=%v", day.Previous, day.Percent)
	}

	year, err := buildSeries(chartJSON(t, 110, 50, closes, stamps), RangeByKey("1y"), "VTI")
	if err != nil {
		t.Fatal(err)
	}
	if year.Previous != 100 || year.Percent != 10 {
		t.Fatalf("a year should measure from its own first bar: prev=%v pct=%v", year.Previous, year.Percent)
	}
}

// A null close is a bar the exchange never printed. Zeroing one draws a spike
// to the floor of the chart, so it has to be dropped along with its timestamp.
func TestBuildSeriesDropsNullBars(t *testing.T) {
	s, err := buildSeries(
		chartJSON(t, 0, 0, []any{100.0, nil, 102.0}, []int64{10, 20, 30}),
		RangeByKey("1d"), "VTI")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Points) != 2 {
		t.Fatalf("want 2 points, got %d: %+v", len(s.Points), s.Points)
	}
	if s.Points[1].T != 30 || s.Points[1].C != 102 {
		t.Fatalf("the surviving bar kept the wrong timestamp: %+v", s.Points[1])
	}
	// An index carries no quote price, so the last bar has to become it.
	if s.Price != 102 {
		t.Fatalf("want the last bar as the price, got %v", s.Price)
	}
}

func TestBuildSeriesNoPrices(t *testing.T) {
	if _, err := buildSeries(chartJSON(t, 0, 0, []any{nil}, []int64{1}), RangeByKey("1d"), "VTI"); err == nil {
		t.Fatal("a chart with no printed bar should be an error, not an empty chart")
	}
}

func TestRangeByKeyFallsBackToTheDay(t *testing.T) {
	if got := RangeByKey("wat"); got.Key != "1d" {
		t.Fatalf("an unknown range should fall back to the day, got %q", got.Key)
	}
	for _, k := range []string{"1d", "1w", "1m", "1y"} {
		if RangeByKey(k).Key != k {
			t.Fatalf("%s did not resolve to itself", k)
		}
	}
}

// The bands are what the tile prints, and an off-by-one at a breakpoint is the
// difference between "good" and "moderate" on the same reading.
func TestBands(t *testing.T) {
	for _, c := range []struct {
		v    float64
		want string
	}{{50, "good"}, {51, "moderate"}, {100, "moderate"}, {101, "unhealthy for some"},
		{301, "hazardous"}} {
		if got := aqiBand(c.v); got != c.want {
			t.Errorf("aqiBand(%v) = %q, want %q", c.v, got, c.want)
		}
	}
	for _, c := range []struct {
		v    float64
		want string
	}{{0, "low"}, {2.4, "low-medium"}, {4.8, "medium"}, {7.2, "medium-high"}, {9.7, "high"}} {
		if got := pollenBand(c.v); got != c.want {
			t.Errorf("pollenBand(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

// A sink is per turn and a model that asks for the same symbol twice should not
// stack two identical charts.
func TestSinkIgnoresARepeat(t *testing.T) {
	s := NewSink()
	s.Add(Widget{Kind: "ticker", Symbol: "VTI"})
	s.Add(Widget{Kind: "ticker", Symbol: "VTI"})
	s.Add(Widget{Kind: "ticker", Symbol: "VOO"})
	s.Add(Widget{Kind: "weather", Place: "Boone"})
	if got := len(s.List()); got != 3 {
		t.Fatalf("want 3 widgets, got %d: %+v", got, s.List())
	}
}

// A nil sink is what a tool sees outside a turn, and it must not panic.
func TestNilSinkIsSafe(t *testing.T) {
	var s *Sink
	s.Add(Widget{Kind: "ticker", Symbol: "VTI"})
	if s.List() != nil {
		t.Fatal("a nil sink should list nothing")
	}
}
