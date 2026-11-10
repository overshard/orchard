package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The page draws a frog from its seed and the server names and rates it from
// the same seed, so the two ports have to make every draw alike.
func TestGenomeMatchesThePage(t *testing.T) {
	raw, err := os.ReadFile("testdata/genomes.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []genome
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) < 100 {
		t.Fatalf("only %d genomes in the fixture", len(want))
	}
	for _, w := range want {
		got := newGenome(w.Seed)
		got.weight = 0
		if got != w {
			t.Errorf("seed %d: got %+v, want %+v", w.Seed, got, w)
		}
	}
}

func TestGenomeIsDeterministic(t *testing.T) {
	if newGenome(1207) != newGenome(1207) {
		t.Error("the same seed made two frogs")
	}
}

func TestRarityTiers(t *testing.T) {
	cases := []struct {
		g    genome
		n    int
		tier string
	}{
		{genome{weight: 330, Pattern: "SPOTS", Pupil: "BAR"}, 21, "COMMON"},
		{genome{weight: 330, Pattern: "PLAIN", Pupil: "ROUND"}, 85, "UNCOMMON"},
		{genome{weight: 60, Pattern: "BANDS", Pupil: "BAR"}, 233, "RARE"},
		{genome{weight: 20, Pattern: "MASK", Pupil: "BAR"}, 700, "EPIC"},
		{genome{weight: 5, Pattern: "PLAIN", Pupil: "SLIT"}, 5600, "LEGENDARY"},
	}
	for _, c := range cases {
		r := rarityOf(c.g)
		if r.N != c.n || r.Label != c.tier {
			t.Errorf("%+v: got 1 in %d %s, want 1 in %d %s", c.g, r.N, r.Label, c.n, c.tier)
		}
	}
}

func TestStageAtEachBoundary(t *testing.T) {
	day := 24 * time.Hour
	cases := []struct {
		age   time.Duration
		stage string
		day   int
	}{
		{0, "egg", 1},
		{day - time.Second, "egg", 1},
		{day, "tadpole", 2},
		{8*day - time.Second, "tadpole", 8},
		{8 * day, "froglet", 9},
		{12*day - time.Second, "froglet", 12},
		{12 * day, "frog", 13},
		{28*day - time.Second, "frog", 28},
		{28 * day, "elder", 29},
		{400 * day, "elder", 401},
	}
	for _, c := range cases {
		stage, d := stageOf(c.age)
		if stage != c.stage || d != c.day {
			t.Errorf("age %v: got %s day %d, want %s day %d", c.age, stage, d, c.stage, c.day)
		}
	}
}
