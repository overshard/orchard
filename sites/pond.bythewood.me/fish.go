package main

import (
	"database/sql"
	"errors"
	"math"
	"math/rand/v2"
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

// rollSize is a fish's length in tenths of an inch. Most come out near the
// small end, so a big one is something to see.
func rollSize(f fishKind) int {
	return f.lo + int(math.Round(float64(f.hi-f.lo)*math.Pow(rand.Float64(), 2.2)))
}

// More fish than a round has time to land.
const fishMost = 40

// caught is one kind out of a round, with the biggest of them.
type caught struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Tier int    `json:"tier"`
	N    int    `json:"n"`
	Size int    `json:"size"`
	// First is the first of its kind, and Best one bigger than the book had.
	First bool `json:"first,omitempty"`
	Best  bool `json:"best,omitempty"`
}

// landFish puts a round's catch in the fish book and rolls each one's size.
// The book pays nothing, so the only check is that the fish's points fit
// under the score. A kind that wasn't biting is left out.
func landFish(tx *sql.Tx, p *pond, landed map[string]int, score int, tables []map[string]float64) ([]caught, error) {
	pts, n := 0, 0
	for _, f := range fishKinds {
		c := landed[f.kind]
		if c < 0 {
			return nil, nil
		}
		pts += c * f.pts
		n += c
	}
	if pts > score || n > fishMost {
		return nil, nil
	}
	var out []caught
	for _, f := range fishKinds {
		c := landed[f.kind]
		biting := false
		for _, t := range tables {
			biting = biting || t[f.kind] > 0
		}
		if c == 0 || !biting {
			continue
		}
		size := 0
		for range c {
			size = max(size, rollSize(f))
		}
		var had int
		err := tx.QueryRow(`SELECT best FROM fish WHERE pond_id = ? AND kind = ?`, p.ID, f.kind).Scan(&had)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if _, err := tx.Exec(`INSERT INTO fish (pond_id, kind, count, best, first_day) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (pond_id, kind) DO UPDATE SET count = count + excluded.count, best = MAX(best, excluded.best)`,
			p.ID, f.kind, c, size, p.today); err != nil {
			return nil, err
		}
		out = append(out, caught{Kind: f.kind, Name: f.name, Tier: f.tier, N: c, Size: size, First: had == 0, Best: had > 0 && size > had})
	}
	return out, nil
}

// fishSlot is a fish in the book. W is how often it's biting right now, and
// Up says the day is one it likes.
type fishSlot struct {
	Kind  string  `json:"kind"`
	Name  string  `json:"name"`
	Tier  int     `json:"tier"`
	Count int     `json:"count"`
	Best  int     `json:"best"`
	First string  `json:"first"`
	Hint  string  `json:"hint"`
	W     float64 `json:"w"`
	Up    bool    `json:"up,omitempty"`
}

type fishingView struct {
	All   []fishSlot `json:"all"`
	Found int        `json:"found"`
}

func fishingOf(q querier, id int64, d fishDay) (fishingView, error) {
	v := fishingView{}
	type row struct {
		count, best int
		first       string
	}
	got := map[string]row{}
	rows, err := q.Query(`SELECT kind, count, best, first_day FROM fish WHERE pond_id = ?`, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var r row
		if err := rows.Scan(&k, &r.count, &r.best, &r.first); err != nil {
			return v, err
		}
		got[k] = r
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	for _, f := range fishKinds {
		r, w := got[f.kind], f.bite(d)
		if r.count > 0 {
			v.Found++
		}
		v.All = append(v.All, fishSlot{f.kind, f.name, f.tier, r.count, r.best, r.first, f.hint, w, w > f.bite(fishDay{})})
	}
	return v, nil
}
