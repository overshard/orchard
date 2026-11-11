package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// ---------------------------------------------------------------- ingredients

type ingredient struct {
	kind string
	name string
	tier int
	bug  bool
	// A trinket that can go in the pot. It never grows or sells at the larder.
	trinket bool
}

// The order is part of every dish's kind, so new ones only ever go on the end.
var ingredients = []ingredient{
	{"duckweed", "Duckweed", 0, false, false},
	{"watercress", "Watercress", 0, false, false},
	{"cricket", "Cricket", 0, true, false},
	{"mayfly", "Mayfly", 1, true, false},
	{"lotus", "Lotus seed", 1, false, false},
	{"marigold", "Marsh marigold", 1, false, false},
	{"firefly", "Firefly", 2, true, false},
	{"morel", "Morel", 2, false, false},
	{"t_button", "Button mushroom", 0, false, true},
	{"t_chanterelle", "Chanterelle", 1, false, true},
	{"t_goldmorel", "Golden morel", 4, false, true},
}

func ingredientOf(kind string) (int, bool) {
	for i, g := range ingredients {
		if g.kind == kind {
			return i, true
		}
	}
	return 0, false
}

// seasonWeights is how likely each ingredient is to come up in a plot this
// month, in the order of ingredients. What grows follows the real year.
func seasonWeights(now time.Time, rain bool) []int {
	m := now.In(pondZone).Month()
	in := func(ms ...time.Month) bool {
		for _, x := range ms {
			if m == x {
				return true
			}
		}
		return false
	}
	w := make([]int, len(ingredients))
	copy(w, []int{30, 8, 5, 0, 0, 2, 0, 1})
	if in(time.March, time.April, time.May, time.September, time.October, time.November) {
		w[1] = 25
	}
	if in(time.June, time.July, time.August, time.September, time.October) {
		w[2] = 20
	}
	if in(time.April, time.May, time.June, time.July) {
		w[3] = 15
	}
	if in(time.June, time.July, time.August, time.September) {
		w[4] = 12
	}
	if in(time.March, time.April, time.May, time.June) {
		w[5] = 12
	}
	if in(time.June, time.July) {
		w[6] = 6
	}
	switch {
	case in(time.March, time.April, time.May):
		w[7] = 5
	case in(time.October, time.November) && rain:
		w[7] = 9
	case in(time.October, time.November):
		w[7] = 3
	}
	return w
}

func rollCrop(now time.Time, rain bool) int {
	w := seasonWeights(now, rain)
	sum := 0
	for _, x := range w {
		sum += x
	}
	r := rand.IntN(sum)
	for i, x := range w {
		if r < x {
			return i
		}
		r -= x
	}
	return 0
}

// ---------------------------------------------------------------- dishes

// Any two ingredients make a dish, so eight make 36. A few pairs have a name
// of their own and a better tier.
var namedDishes = map[[2]int]struct {
	name string
	tier int
}{
	{0, 0}:   {"Pond soup", 0},
	{1, 2}:   {"Cricket sandwich", 1},
	{3, 4}:   {"Mayfly brittle", 2},
	{4, 6}:   {"Lantern cake", 3},
	{5, 7}:   {"Bank holiday pie", 3},
	{6, 7}:   {"Midnight broth", 4},
	{6, 6}:   {"Midsummer jam", 4},
	{2, 7}:   {"Cricket and morel pasty", 3},
	{7, 10}:  {"Morel king's supper", 4},
	{10, 10}: {"Golden mushroom pie", 4},
	{8, 9}:   {"Forest floor soup", 2},
}

var dishCount = len(ingredients) * (len(ingredients) + 1) / 2

// What Big Bean pays for a dish, by tier.
var dishWorth = [tierCount]int{8, 25, 70, 200, 600}

type dish struct {
	a, b int
	name string
	tier int
}

func dishOf(a, b int) dish {
	if a > b {
		a, b = b, a
	}
	if n, ok := namedDishes[[2]int{a, b}]; ok {
		return dish{a, b, n.name, n.tier}
	}
	A, B := ingredients[a], ingredients[b]
	form := "stew"
	switch {
	case A.bug && B.bug:
		form = "skewer"
	case !A.bug && !B.bug && a == b:
		form = "tea"
	case !A.bug && !B.bug:
		form = "salad"
	}
	name := A.name + " and " + strings.ToLower(B.name) + " " + form
	if a == b {
		name = "Double " + strings.ToLower(A.name) + " " + form
	}
	t := max(A.tier, B.tier)
	// Only a rare trinket makes a dish past epic.
	if t >= 3 {
		return dish{a, b, name, t}
	}
	if A.tier > 0 && B.tier > 0 {
		t++
	}
	return dish{a, b, name, min(t, 3)}
}

func (d dish) kind() string { return fmt.Sprintf("dish_%d_%d", d.a, d.b) }

func dishOfKind(kind string) (dish, bool) {
	var a, b int
	if _, err := fmt.Sscanf(kind, "dish_%d_%d", &a, &b); err != nil {
		return dish{}, false
	}
	if a < 0 || b < a || b >= len(ingredients) || kind != fmt.Sprintf("dish_%d_%d", a, b) {
		return dish{}, false
	}
	return dishOf(a, b), true
}
