package main

import (
	"strings"
	"testing"
	"time"
)

// A cut down copy of what dash's /api/state answers with.
const dashSample = `{
  "market": {"cards": [{"key": "sp500", "percent": "-1.24%"}, {"key": "vix", "percent": "+9.00%"}]},
  "signal": {"conditions": [{"label": "VIX", "value": "29.4", "known": true}]},
  "briefs": {"news": {"read": {"level": "major"}}},
  "hn_pulse": {"level": "hot"},
  "weather": {"temperature": "38", "rain": "70%", "condition": "Light Rain", "sunrise": "6:54am", "sunset": "5:18pm"},
  "alerts": [{"event": "Frost Advisory", "severity": "Minor"}, {"event": "Tornado Warning", "severity": "Extreme"}],
  "systems": {"rows": [{"label": "Blog", "state": "up"}, {"label": "Chat", "state": "down"}]},
  "ahead": [{"label": "CPI", "when": "TODAY 8:30AM"}],
  "anything": "else"
}`

func TestReadingTheRealDay(t *testing.T) {
	now := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	r, err := readDash([]byte(dashSample), now)
	if err != nil {
		t.Fatal(err)
	}
	want := reading{At: now, Temp: 38, HasTemp: true, Cond: "Light Rain", Rain: 70, Sunrise: "6:54am",
		Sunset: "5:18pm", SP: -1.24, HasSP: true, VIX: 29.4, Front: "CPI today", Swarm: true,
		Major: true, Down: "Chat", Alert: "Tornado Warning"}
	if r != want {
		t.Fatalf("got %+v\nwant %+v", r, want)
	}

	// Ten in the morning in New York is day, and every flag shows.
	v := dayOfReading(&r, now)
	if v.Night || !v.Cold || !v.Rain || v.Water != "murky" || v.Wind != 2 || !v.Swarm || !v.Dragonfly || !v.Heron || !v.Shelter {
		t.Errorf("day: %+v", v)
	}
	if len(v.Lines) != 8 || !strings.Contains(v.Lines[0][1], "38°") {
		t.Errorf("lines: %v", v.Lines)
	}
	if v := dayOfReading(&r, now.Add(9*time.Hour)); !v.Night {
		t.Error("midnight in New York was not night")
	}
	if v := dayOfReading(nil, now); v.Known || len(v.Lines) != 0 {
		t.Errorf("no reading: %+v", v)
	}
}

func TestAnOldReadingIsDropped(t *testing.T) {
	var a almanac
	now := time.Now()
	a.last.Store(&reading{At: now.Add(-dayStale - time.Minute)})
	if a.reading(now) != nil {
		t.Error("a stale reading was used")
	}
}

func TestSeasonLines(t *testing.T) {
	at := func(s string) time.Time { d, _ := time.ParseInLocation("2006-01-02 15", s, pondZone); return d }
	var v dayView
	season(&v, at("2026-06-21 12"))
	if !v.Fireflies || len(v.Lines) != 2 {
		t.Fatalf("midsummer = %+v, want fireflies and the longest day", v)
	}
	v = dayView{}
	season(&v, at("2026-11-24 21"))
	if !v.Moon || v.Fireflies || v.Spawn {
		t.Fatalf("the full moon on 2026-11-24 = %+v", v)
	}
	v = dayView{}
	season(&v, at("2026-03-01 12"))
	if !v.Spawn {
		t.Fatal("no frogspawn on 1 March")
	}
}
