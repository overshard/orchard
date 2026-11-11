package main

import (
	"fmt"
	"strings"
)

// todo is one thing worth doing now, shown as a chip on the pond and as a
// line on dash. To is the pond page's view it opens.
type todo struct {
	Kind  string `json:"kind"`
	Icon  string `json:"icon"`
	Label string `json:"label"`
	To    string `json:"to"`
}

// todoOf is what's waiting to be collected and what still pays today. A game
// drops off once its full pay runs are used or it's hit the day's cap, and
// with nothing else it suggests the next thing to save for.
func todoOf(v *view) []todo {
	out := []todo{}
	add := func(kind, icon, label, to string) {
		if v.Open.has(to) {
			out = append(out, todo{kind, icon, label, to})
		}
	}
	b := v.Bank
	if v.Wheel.Free > 0 {
		add("wheel", "i_play", plural(v.Wheel.Free, "free spin"), "wheel")
	}
	if v.Log.Ready {
		add("log", "wood", "Turn the old log", "play")
	}
	if b.Gravel.Held >= 1 {
		add("gravel", "peb", fmt.Sprintf("%d on the gravel bar", b.Gravel.Held), "bank")
	}
	if b.Burrows.Held >= 1 {
		add("rent", "peb", fmt.Sprintf("%d in rent", b.Burrows.Held), "bank")
	}
	for _, pl := range b.Garden.Plots {
		if pl.ReadyIn == 0 {
			add("garden", "watercress", "Garden's ready", "garden")
			break
		}
	}
	if b.Dye.Busy && b.Dye.ReadyIn <= 0 {
		add("dye", "dye_blue", "Dye's ready", "bank")
	}
	full, short := []string{}, []*reedView{}
	for i, f := range v.Frogs {
		if f.Work == nil {
			continue
		}
		switch f.Work.Stop {
		case "full":
			if j, ok := jobOf(f.Work.Job); ok {
				full = append(full, j.keep)
			}
		case "short":
			short = append(short, &v.Frogs[i])
		}
	}
	switch {
	case len(full) == 1:
		add("shift", "sticks", "Basket full, collect to keep "+full[0], "job")
	case len(full) > 1:
		add("shift", "sticks", fmt.Sprintf("%d baskets full, collect to keep them working", len(full)), "job")
	}
	for _, f := range short {
		add("short", f.Work.Short, titled(f.Name)+" ran out of "+f.Work.Short, "job")
	}
	if v.Stall.Takings > 0 {
		add("stall", "i_town", fmt.Sprintf("Stall sold %d", v.Stall.Takings), "town")
	}

	if w := v.Want; w.Ready && v.Pebbles >= w.Price {
		add("wanted", "hat", "Little Tully has your "+strings.ToLower(w.Hat.Name), "town")
	}

	left := func(paid, cap, runs, full int) int {
		if paid >= cap {
			return 0
		}
		return max(0, full-runs)
	}
	// Well rested only says so while some game has room under its cap.
	room := v.Dive.Paid < v.Dive.Cap
	for _, g := range v.Arcade {
		// A daily game that's had its paid go has nothing left to pay more of.
		room = room || (g.Paid < g.Cap && !(g.Daily && g.Runs >= g.Full))
	}
	if v.Rested && room {
		add("rested", "i_rest", "Well rested, next game pays +25%", "play")
	}
	if n := left(v.Dive.Paid, v.Dive.Cap, v.Dive.Runs, v.Dive.Full); n > 0 {
		add("dive", "i_pond", "Dive, "+plural(n, "full pay run")+" left", "dive")
	}
	// One chip for the side games, since one each would push everything else off.
	open, only := 0, arcadeView{}
	for _, g := range v.Arcade {
		if left(g.Paid, g.Cap, g.Runs, g.Full) > 0 {
			open++
			only = g
		}
	}
	if open == 1 {
		add("arcade", "i_play", only.Name+", "+plural(left(only.Paid, only.Cap, only.Runs, only.Full), "full pay run")+" left", only.Key)
	} else if open > 1 {
		add("arcade", "i_play", plural(open, "side game")+" still pay in full", "play")
	}
	if f := v.Neighbours.Favour; !f.Done && v.held[f.Kind] >= f.N {
		add("favour", "i_town", fmt.Sprintf("%s needs %d %s", f.Name, f.N, strings.ToLower(f.What)), "town")
	}
	if hasDish(v.Bag) {
		for _, n := range v.Neighbours.All {
			if !n.Gave {
				add("gift", "i_town", "Bring a neighbour a dish", "town")
				break
			}
		}
	}
	idle, working := []string{}, 0
	for _, f := range v.Frogs {
		if f.Work != nil && f.Work.Job == "" {
			idle = append(idle, titled(f.Name))
		} else if f.Work != nil {
			working++
		}
	}
	// With nobody working the job screen opens on an idle frog, ready to pick.
	switch {
	case len(idle) == 1 && working == 0:
		add("idle", "sticks", "Put "+idle[0]+" to work", "job")
	case len(idle) == 1:
		add("idle", "sticks", idle[0]+" has no job", "reeds")
	case len(idle) > 1:
		add("idle", "sticks", fmt.Sprintf("%d frogs with no job", len(idle)), "reeds")
	}

	if len(out) > 0 {
		return out
	}
	builds := []struct {
		key, name, to string
		level, cost   int
		mats          map[string]int
	}{
		{"gravel", "the gravel bar", "bank", b.Gravel.Level, b.Gravel.Cost, b.Gravel.Mats},
		{"garden", "the garden", "garden", b.Garden.Level, b.Garden.Cost, b.Garden.Mats},
		{"dye", "the dye works", "bank", b.Dye.Level, b.Dye.Cost, b.Dye.Mats},
		{"burrows", "the burrows", "bank", b.Burrows.Level, b.Burrows.Cost, b.Burrows.Mats},
	}
	for _, bd := range builds {
		if v.Open.has(bd.to) && bd.cost > 0 && v.Pebbles >= bd.cost && hasAll(v.held, bd.mats) {
			verb := "Build "
			if bd.level > 0 {
				verb = "Level up "
			}
			add("save", "i_bank", verb+bd.name, bd.to)
			return out
		}
	}
	if v.Pebbles >= v.Box.Cost {
		add("save", "i_town", fmt.Sprintf("Open a hat box for %d", v.Box.Cost), "town")
	}
	return out
}

func hasAll(held, need map[string]int) bool {
	for k, n := range need {
		if held[k] < n {
			return false
		}
	}
	return true
}

func hasDish(bag []bagThing) bool {
	for _, t := range bag {
		if t.Sort == "dish" {
			return true
		}
	}
	return false
}
