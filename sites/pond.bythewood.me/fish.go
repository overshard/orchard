package main

import (
	"time"
)

// What fishing has to know about the day. Night, rain, cold and the water
// come off dash, and the moon and frogspawn season off the calendar.
type fishDay struct {
	night, rain, cold, warm, clear, murky, moon, spawn bool
}

// A carp afternoon is this warm or more, between noon and six.
const warmFrom = 70

func fishDayOf(r *reading, now time.Time) fishDay {
	v := dayOfReading(r, now)
	d := fishDay{night: v.Night, rain: v.Rain, cold: v.Cold, clear: v.Water == "clear", murky: v.Water == "murky",
		moon: moonOut(r, now), spawn: spawnOn(r, now)}
	h := now.In(pondZone).Hour()
	d.warm = r != nil && r.HasTemp && r.Temp >= warmFrom && h >= 12 && h < 18 && !v.Night
	return d
}

// when returns a weight for the days f is true on and another for the rest.
func when(f func(fishDay) bool, on, off float64) func(fishDay) float64 {
	return func(d fishDay) float64 {
		if f(d) {
			return on
		}
		return off
	}
}

func always(w float64) func(fishDay) float64 {
	return func(fishDay) float64 { return w }
}

// A fish is worth pts in a round and counts in power by its tier. bite is how
// often it turns up on a given day, and hint says so in the fish book. Sizes
// are in tenths of an inch. The page has the same kinds and points in
// fishes.js and a test holds the two together.
type fishKind struct {
	kind, name string
	pts, tier  int
	bite       func(fishDay) float64
	hint       string
	lo, hi     int
}

var fishKinds = []fishKind{
	{"minnow", "Minnow", 2, 0, always(26), "Bites any time.", 15, 40},
	{"gudgeon", "Gudgeon", 3, 0, always(16), "Bites any time, down on the bottom.", 30, 70},
	{"roach", "Roach", 4, 0, always(20), "Bites any time.", 50, 140},
	{"rudd", "Rudd", 5, 0, always(14), "Bites any time, up near the top.", 60, 150},
	{"perch", "Perch", 6, 0, always(18), "Bites any time.", 60, 180},
	{"bream", "Bream", 9, 0, always(13), "Bites any time.", 100, 260},
	{"trout", "Trout", 12, 1, when(func(d fishDay) bool { return d.rain }, 24, 7), "Bites best in the rain.", 100, 280},
	{"grayling", "Grayling", 13, 1, when(func(d fishDay) bool { return d.clear }, 12, 2), "Bites best when the water runs clear.", 100, 220},
	{"pike", "Pike", 15, 1, when(func(d fishDay) bool { return d.cold }, 20, 6), "Bites best on a cold day.", 180, 440},
	{"eel", "Eel", 16, 1, when(func(d fishDay) bool { return d.night }, 14, 3), "Bites best after dark.", 160, 400},
	{"catfish", "Catfish", 18, 1, when(func(d fishDay) bool { return d.night }, 14, 3), "Bites best after dark.", 150, 400},
	{"stickleback", "Stickleback", 5, 2, when(func(d fishDay) bool { return d.spawn }, 10, 0), "Only in frogspawn season, late winter.", 15, 35},
	{"tench", "Tench", 14, 2, when(func(d fishDay) bool { return d.murky }, 8, 0), "Only when the water is murky.", 120, 260},
	{"zander", "Zander", 22, 2, when(func(d fishDay) bool { return d.night }, 7, 2), "Bites best after dark.", 160, 380},
	{"carp", "Carp", 25, 2, when(func(d fishDay) bool { return d.warm }, 14, 3), "Bites best on a warm afternoon.", 160, 400},
	{"koi", "Golden koi", 40, 3, always(3), "Bites any time, and not often.", 120, 300},
	{"monster", "Monster pike", 50, 3, when(func(d fishDay) bool { return d.cold }, 5, 2.5), "Bites any time, more on a cold day.", 400, 560},
	{"sturgeon", "Sturgeon", 60, 3, always(2.5), "Bites any time, and not often.", 400, 840},
	{"wels", "Giant catfish", 80, 4, when(func(d fishDay) bool { return d.night }, 3, 1.5), "Bites any time, more after dark.", 480, 960},
	{"moonkoi", "Moon koi", 45, 4, when(func(d fishDay) bool { return d.moon }, 3, 0), "Only on the day of a full moon.", 140, 320},
}

func fishOf(kind string) (fishKind, bool) {
	for _, f := range fishKinds {
		if f.kind == kind {
			return f, true
		}
	}
	return fishKind{}, false
}

// fishTable is what's biting, each kind with its weight, and nothing that
// isn't about today. The common fish are in it whatever the day is.
func fishTable(d fishDay) map[string]float64 {
	out := map[string]float64{}
	for _, f := range fishKinds {
		if w := f.bite(d); w > 0 {
			out[f.kind] = w
		}
	}
	return out
}

func (s *store) fishTableAt(at time.Time) map[string]float64 {
	return fishTable(fishDayOf(s.day.reading(at), at))
}
