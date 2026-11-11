package main

import (
	"fmt"
	"strings"
)

// A mark is one reason somebody is after a thing in the bag, as a line for
// that thing's sheet. kind is a bag kind, an item's or hat_<shape>_<colour>.
type mark struct{ kind, line string }

// markers is everything that can want a thing. Each looks over the finished
// view and says what it's after, and a new source is one more func on the
// list. A mark for a thing the pond doesn't hold is dropped.
var markers = []func(v *view) []mark{favouriteMarks, favourMarks, recipeMarks}

// markBag puts each thing's marks on it, in the order of markers.
func markBag(v *view) {
	at := map[string]int{}
	for i, t := range v.Bag {
		at[t.Kind] = i
	}
	for _, m := range markers {
		for _, k := range m(v) {
			if i, ok := at[k.kind]; ok {
				v.Bag[i].Wanted = append(v.Bag[i].Wanted, k.line)
			}
		}
	}
}

// has says the pond holds both halves of a dish.
func (d dish) has(held map[string]int) bool {
	a, b := ingredients[d.a].kind, ingredients[d.b].kind
	if a == b {
		return held[a] >= 2
	}
	return held[a] > 0 && held[b] > 0
}

// favouriteMarks is a dish a neighbour likes best, and what goes in it once
// the neighbour has said. One that's had its dish today or is already a best
// friend isn't after anything.
func favouriteMarks(v *view) []mark {
	var out []mark
	for _, n := range v.Neighbours.All {
		if n.Gave || n.Level >= len(friendAt)-1 {
			continue
		}
		r := residents[n.ID]
		likes := []struct {
			d    dish
			line string
		}{{r.favDish(), r.name + "'s favourite dish."}}
		if also, ok := r.alsoDish(); ok {
			likes = append(likes, struct {
				d    dish
				line string
			}{also, r.name + " likes this nearly as much as its favourite."})
		}
		for _, l := range likes {
			out = append(out, mark{l.d.kind(), l.line})
			if n.Level < recipeAt || !l.d.has(v.held) {
				continue
			}
			line := fmt.Sprintf("Goes in %s, which %s loves.", strings.ToLower(l.d.name), r.name)
			out = append(out, mark{ingredients[l.d.a].kind, line})
			if l.d.b != l.d.a {
				out = append(out, mark{ingredients[l.d.b].kind, line})
			}
		}
	}
	return out
}

// favourMarks is what today's favour asks for.
func favourMarks(v *view) []mark {
	f := v.Neighbours.Favour
	if f.Done {
		return nil
	}
	line := fmt.Sprintf("%s needs %d today.", f.Name, f.N)
	if have := v.held[f.Kind]; have < f.N {
		line = fmt.Sprintf("%s needs %d today, and you have %d.", f.Name, f.N, have)
	}
	return []mark{{f.Kind, line}}
}

// recipeMarks is every ingredient that would cook into a dish the pond has
// never made with something else it's holding. The line names the other half
// and never the dish.
func recipeMarks(v *view) []mark {
	if !v.Open.has("garden") {
		return nil
	}
	cooked := map[string]bool{}
	for _, d := range v.Kitchen.Cookbook {
		cooked[d.Kind] = true
	}
	with := make([][]string, len(ingredients))
	twice := make([]bool, len(ingredients))
	for a := range ingredients {
		for b := a; b < len(ingredients); b++ {
			d := dishOf(a, b)
			if cooked[d.kind()] || !d.has(v.held) {
				continue
			}
			if a == b {
				twice[a] = true
				continue
			}
			with[a] = append(with[a], strings.ToLower(ingredients[b].name))
			with[b] = append(with[b], strings.ToLower(ingredients[a].name))
		}
	}
	var out []mark
	for i, g := range ingredients {
		if twice[i] {
			out = append(out, mark{g.kind, "Two of these cook into a dish you haven't made."})
		}
		if len(with[i]) > 0 {
			out = append(out, mark{g.kind, "Cooks into a dish you haven't made with " + anyOf(with[i]) + "."})
		}
	}
	return out
}

// anyOf lists up to three names and says when there are more.
func anyOf(names []string) string {
	switch {
	case len(names) == 1:
		return names[0]
	case len(names) <= 3:
		return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
	}
	return strings.Join(names[:3], ", ") + " and more"
}
