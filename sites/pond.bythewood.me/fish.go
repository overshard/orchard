package main

import (
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"strings"
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

// A fish in the book goes by this in front of its kind wherever found things
// of every sort are kept together, since a fish isn't an item.
const fishHad = "fish_"

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
	// Record is one that took the pond record, off the neighbour in From.
	Record bool   `json:"record,omitempty"`
	From   string `json:"from,omitempty"`
}

// recordGame is a pond record's row in records, with the kind after it and
// the size as its score.
const recordGame = "rec_"

// pondRecord is the size to beat for a kind and the neighbour who holds it,
// the same in every pond. Sedge fishes all day, so most of them are Sedge's.
func pondRecord(f fishKind) (size, who int) {
	h := fnv.New32a()
	h.Write([]byte("record " + f.kind))
	n := h.Sum32()
	size = f.lo + (f.hi-f.lo)*(800+int(n%150))/1000
	if (n>>8)%5 >= 3 {
		who = 1 + int((n>>12)%uint32(len(residents)-1))
	}
	return size, who
}

// sedgeSays is Sedge on losing a record, or on somebody else losing one.
func sedgeSays(f fishKind, who int) string {
	fish := strings.ToLower(f.name)
	lines := []string{
		"That was my " + fish + " record. Fine, it's yours, so look after it.",
		"I held that " + fish + " record a long time. Don't let it go to your head.",
		strings.ToUpper(aFish(f)[:1]) + aFish(f)[1:] + " that size, off my pond. I suppose somebody had to.",
	}
	if who != 0 {
		name := residents[who].name
		lines = []string{
			"Bigger than " + name + "'s " + fish + ". I never liked that record anyway.",
			name + " won't enjoy hearing about that " + fish + ", so I'll be the one to say.",
			"That beats " + name + "'s " + fish + ". Not bad, and I don't say that.",
		}
	}
	return lines[seedPick(uint32(f.lo+f.hi), "sedge "+f.kind, len(lines))]
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
		got := caught{Kind: f.kind, Name: f.name, Tier: f.tier, N: c, Size: size, First: had == 0, Best: had > 0 && size > had}
		if rec, who := pondRecord(f); size > rec && had <= rec {
			got.Record, got.From = true, residents[who].name
			if _, err := tx.Exec(`INSERT INTO records (pond_id, game, day, score) VALUES (?, ?, ?, ?)
				ON CONFLICT (pond_id, game, day) DO UPDATE SET score = MAX(score, excluded.score)`,
				p.ID, recordGame+f.kind, p.today, size); err != nil {
				return nil, err
			}
		}
		out = append(out, got)
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
	// The pond record, who holds it, and whether this pond has beaten it.
	Record int    `json:"record"`
	Holder string `json:"holder"`
	Yours  bool   `json:"yours,omitempty"`
}

// inches is a size the way the paper prints it.
func inches(size int) string { return fmt.Sprintf("%d.%d", size/10, size%10) }

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
		rec, who := pondRecord(f)
		v.All = append(v.All, fishSlot{f.kind, f.name, f.tier, r.count, r.best, r.first, f.hint, w, w > f.bite(fishDay{}),
			rec, residents[who].name, r.best > rec})
	}
	return v, nil
}
