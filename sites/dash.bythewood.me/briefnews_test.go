package main

import (
	"slices"
	"testing"
)

func TestWidelyCarriedKeepsSinglesOnAQuietDay(t *testing.T) {
	two := cluster{stories: []story{{source: "AP"}, {source: "FOX"}}}
	one := cluster{stories: []story{{source: "NPR"}}}

	quiet := []cluster{two, one, one}
	if len(widelyCarried(quiet)) != 3 {
		t.Error("dropped singles with too few shared events to replace them")
	}

	var busy []cluster
	for i := 0; i < 24; i++ {
		busy = append(busy, two)
	}
	busy = append(busy, one)
	if got := widelyCarried(busy); len(got) != 24 {
		t.Errorf("kept %d, want the 24 shared", len(got))
	}
}

func TestMostImpact(t *testing.T) {
	trial := cluster{stories: []story{{source: "AP", title: "Court denies stay"}, {source: "CBS"}}}
	clancy := cluster{stories: []story{{source: "AP"}, {source: "REUTERS"}, {source: "WSJ"}, {source: "HILL"},
		{source: "NPR"}, {source: "CBS"}, {source: "FOX"}}}
	fed := cluster{stories: []story{{source: "AP", title: "Fed holds"}, {source: "WSJ"}}}
	poland := cluster{stories: []story{{source: "REUTERS", title: "One dead, two injured in school attack"}}}
	envoy := cluster{stories: []story{{source: "NPR", title: "Envoy recalled"}}}
	column := cluster{stories: []story{{source: "EXAMINER", title: "A column"}}}
	skipped := cluster{stories: []story{{source: "BBC", title: "Not rated"}}}

	events := []cluster{trial, clancy, fed, poland, envoy, column, skipped}
	ratings := []eventRating{
		{1, "crime_or_court", 2}, {2, "crime_or_court", 2}, {3, "economy", 5},
		{4, "world", 1}, {5, "world", 4}, {6, "us_politics", 3}, {},
	}
	got := mostImpact(events, ratings, 3)
	var titles []string
	for _, c := range got {
		titles = append(titles, c.lead().title)
	}
	want := []string{"Fed holds", "Envoy recalled", "Not rated"}
	if !slices.Equal(titles, want) {
		t.Errorf("got %q, want %q", titles, want)
	}
	if got := mostImpact(events, ratings, 10); len(got) != 4 {
		t.Errorf("kept %d, want the trial everyone ran too but not the trial, the attack or the column", len(got))
	}
}
