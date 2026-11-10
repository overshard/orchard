package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The share card is drawn here and the pond is drawn by the page, so the two
// have to come out cell for cell alike.
func TestSpriteMatchesThePage(t *testing.T) {
	raw, err := os.ReadFile("testdata/sprites.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []struct {
		Seed    uint32
		Stage   string
		Day     int
		Hat     hat
		Cells   string
		Colours map[string]string
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		g := lookOf(w.Seed)
		var b strings.Builder
		for _, v := range spriteCells(g, w.Stage, w.Day, w.Hat.Shape) {
			fmt.Fprintf(&b, "%x", v)
		}
		if got := b.String(); got != w.Cells {
			t.Errorf("seed %d %s in hat %d: cells differ", w.Seed, w.Stage, w.Hat.Shape)
		}
		if w.Colours == nil {
			continue
		}
		col := spriteColours(g, w.Hat)
		for k, css := range w.Colours {
			n, _ := strconv.Atoi(k)
			if got, want := col[uint8(n)], parseCSS(t, css); got != want {
				t.Errorf("seed %d colour %d: got %v, want %v from %s", w.Seed, n, got, want, css)
			}
		}
	}
}

func parseCSS(t *testing.T, css string) any {
	t.Helper()
	if strings.HasPrefix(css, "#") {
		return rgb(css)
	}
	var h, s, l float64
	if _, err := fmt.Sscanf(strings.NewReplacer("%", "").Replace(css), "hsl(%g %g %g)", &h, &s, &l); err != nil {
		t.Fatalf("%s: %v", css, err)
	}
	return hsl(h, s, l)
}
