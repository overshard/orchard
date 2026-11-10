package main

import (
	"math"
	"strings"
)

// frontend/src/frog.js draws the frog from the same seed, so every draw here
// has to stay in the order that file makes them. The fixture test holds the
// two together.

type morph struct {
	Name    string
	W       int
	H, S, L [2]float64
	Mask    bool
}

// Weights are out of 1000.
var morphs = []morph{
	{"GREEN", 330, [2]float64{95, 135}, [2]float64{42, 58}, [2]float64{38, 46}, false},
	{"OLIVE", 220, [2]float64{58, 82}, [2]float64{34, 48}, [2]float64{36, 44}, false},
	{"WOOD BROWN", 180, [2]float64{20, 34}, [2]float64{34, 48}, [2]float64{34, 42}, true},
	{"GREY", 100, [2]float64{70, 110}, [2]float64{5, 12}, [2]float64{46, 54}, false},
	{"GOLDEN", 60, [2]float64{42, 48}, [2]float64{80, 90}, [2]float64{50, 56}, false},
	{"RED EYE", 50, [2]float64{108, 128}, [2]float64{58, 68}, [2]float64{42, 48}, false},
	{"STRAWBERRY", 35, [2]float64{2, 10}, [2]float64{70, 80}, [2]float64{46, 52}, false},
	{"BLUE", 20, [2]float64{198, 212}, [2]float64{55, 68}, [2]float64{46, 54}, false},
	{"ALBINO", 5, [2]float64{44, 50}, [2]float64{55, 65}, [2]float64{84, 88}, false},
}

var (
	patterns = []string{"PLAIN", "SPOTS", "SPOTS", "BANDS", "SPECKLED", "MASK", "BACK STRIPE"}
	pupils   = []string{"BAR", "BAR", "ROUND", "SLIT"}
	namePre  = []string{"", "", "", "OLD ", "LITTLE ", "BIG "}
	nameBase = []string{"PIP", "BRINDLE", "MOSS", "DOT", "MARSH", "TULLY", "BEAN", "SEDGE", "WREN", "PUDDLE", "FENN", "CLOVER", "RUSH", "NEWT", "BRAMBLE", "OTIS"}
	natures  = []string{"BOLD", "SHY", "NIGHT OWL", "GLUTTON", "SINGER", "DRIFTER"}
	tiers    = []string{"COMMON", "UNCOMMON", "RARE", "EPIC", "LEGENDARY"}
)

type genome struct {
	Seed    uint32 `json:"seed"`
	Name    string `json:"name"`
	Morph   string `json:"morph"`
	Pattern string `json:"pattern"`
	Pupil   string `json:"pupil"`
	Nature  string `json:"nature"`
	Pitch   int    `json:"pitch"`
	Rate    int    `json:"rate"`
	Notes   int    `json:"notes"`

	weight int
	drops  bool
}

func mulberry32(a uint32) func() float64 {
	return func() float64 {
		a += 0x6D2B79F5
		t := (a ^ a>>15) * (1 | a)
		t = (t + (t^t>>7)*(61|t)) ^ t
		return float64(t^t>>14) / 4294967296
	}
}

func pick(r func() float64, list []string) string {
	return list[int(r()*float64(len(list)))]
}

func newGenome(seed uint32) genome {
	r := mulberry32(seed)
	roll, acc := r()*1000, 0
	m := morphs[0]
	for _, c := range morphs {
		acc += c.W
		if roll < float64(acc) {
			m = c
			break
		}
	}
	// Hue, saturation, lightness, then body width and height, eye size and gap,
	// which only the drawing uses.
	for range 7 {
		r()
	}
	g := genome{Seed: seed, Morph: m.Name, weight: m.W}
	g.Pupil = pick(r, pupils)
	if m.Mask && r() < 0.6 {
		g.Pattern = "MASK"
	} else {
		g.Pattern = pick(r, patterns)
	}
	r()
	r()
	g.Nature = pick(r, natures)
	a, b := r(), r()
	// The conversions keep a multiply and an add from being fused into one
	// rounding, which the page cannot do and would put the two a hertz apart.
	g.Pitch = int(math.Floor((260+float64(a*b*2400))/10+0.5)) * 10
	g.Rate = int(math.Floor(9 + float64(r()*34) + 0.5))
	g.Notes = 1 + int(r()*4)
	r()
	g.Name = pick(r, namePre)
	g.Name += pick(r, nameBase)
	return g
}

type rarity struct {
	N     int    `json:"n"`
	Tier  int    `json:"tier"`
	Label string `json:"label"`
}

// rarityOf is how often this colour, back and pupil come out of a plain egg.
// Eye drops never come out of one, so a frog with them counts ten times rarer.
func rarityOf(g genome) rarity {
	pat, pup := 1.0/7, 0.25
	if g.Pattern == "SPOTS" {
		pat = 2.0 / 7
	}
	if g.Pupil == "BAR" {
		pup = 0.5
	}
	n := int(math.Floor(1/(float64(g.weight)/1000*pat*pup) + 0.5))
	if g.drops {
		n *= 10
	}
	t := 4
	switch {
	case n < 60:
		t = 0
	case n < 180:
		t = 1
	case n < 600:
		t = 2
	case n < 2500:
		t = 3
	}
	return rarity{N: n, Tier: t, Label: tiers[t]}
}

// "OLD PIP" to "Old Pip", for a sentence.
func titled(name string) string {
	words := strings.Fields(strings.ToLower(name))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
