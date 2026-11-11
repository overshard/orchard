package main

import "strings"

// Two more of the neighbours keep a small shop each. Sedge sells bait off
// the old log and Marsh frames for the postcard.
const (
	shopSedge = "sedge"
	shopMarsh = "marsh"
)

// A bait goes on the hook for one round of fishing and makes the fish of a
// tier and up bite more often. It adds less to a round than it costs, so it
// only helps fill the fish book.
var baits = []struct {
	kind, name string
	price      int
	from       int
	limit      int
	by         float64
}{
	{"bait_worms", "Tin of worms", 30, 1, 3, 1.5},
	{"bait_lure", "Glow lure", 80, 2, 1, 2},
}

// A frame is a backdrop for the postcard, sold once each.
var frames = []struct {
	kind, name string
	price      int
}{
	{"pc_lily", "Lily border", 150},
	{"pc_stamp", "Postage stamp", 300},
	{"pc_night", "Starry night", 450},
	{"pc_gilt", "Gilt frame", 900},
}

func init() {
	for _, b := range baits {
		itemNames[b.kind] = b.name
	}
	for _, f := range frames {
		itemNames[f.kind] = f.name
	}
}

func sedgeStock() []shopLine {
	var out []shopLine
	for i, b := range baits {
		out = append(out, shopLine{Slot: i, Kind: b.kind, Name: b.name, Tier: b.from, Price: b.price, Limit: b.limit})
	}
	return out
}

func marshStock() []shopLine {
	var out []shopLine
	for i, f := range frames {
		out = append(out, shopLine{Slot: i, Kind: f.kind, Name: f.name, Price: f.price, Limit: 1})
	}
	return out
}

// baited is what's biting with a bait on, fish of its tier and up more
// often, and nothing new biting that wasn't.
func baited(table map[string]float64, kind string) map[string]float64 {
	from, by := -1, 1.0
	for _, b := range baits {
		if b.kind == kind {
			from, by = b.from, b.by
		}
	}
	out := make(map[string]float64, len(table))
	for k, w := range table {
		if f, _ := fishOf(k); from >= 0 && f.tier >= from {
			w *= by
		}
		out[k] = w
	}
	return out
}

// bestBait is the best bait the pond holds, or nothing.
func bestBait(held func(string) int) string {
	for i := len(baits) - 1; i >= 0; i-- {
		if held(baits[i].kind) > 0 {
			return baits[i].kind
		}
	}
	return ""
}

func isFrame(kind string) bool { return strings.HasPrefix(kind, "pc_") }
