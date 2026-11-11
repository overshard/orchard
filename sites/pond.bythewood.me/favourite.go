package main

import (
	"database/sql"
	"errors"
	"hash/fnv"
	"strings"
	"time"
)

// By the time it's a frog it has a favourite dish and a favourite hat, the
// ones it has eaten and worn most, or one off its seed when it's had none.
// They're settled once and kept, and the dish keeps it full for longer.
const favFull = 1.5

func seedPick(seed uint32, salt string, n int) int {
	h := fnv.New32a()
	h.Write([]byte{byte(seed), byte(seed >> 8), byte(seed >> 16), byte(seed >> 24)})
	h.Write([]byte(salt))
	return int(h.Sum32() % uint32(n))
}

// seedDish is one a garden can make, so a favourite off the seed can be cooked.
func seedDish(seed uint32) string {
	var plain []int
	for i, in := range ingredients {
		if in.grows() {
			plain = append(plain, i)
		}
	}
	var all []dish
	for i, a := range plain {
		for _, b := range plain[i:] {
			all = append(all, dishOf(a, b))
		}
	}
	return all[seedPick(seed, "dish", len(all))].kind()
}

// seedHat is one of the hats a box mostly turns up.
func seedHat(seed uint32) string {
	var all []hat
	for t := range 3 {
		all = append(all, hatsByTier[t]...)
	}
	return hatKind(all[seedPick(seed, "hat", len(all))])
}

// noteTaste counts a dish eaten or a day in a hat toward a frog's favourite.
func noteTaste(tx *sql.Tx, frogID int64, kind string) error {
	_, err := tx.Exec(`INSERT INTO tastes (frog_id, kind, n) VALUES (?, ?, 1)
		ON CONFLICT (frog_id, kind) DO UPDATE SET n = n + 1`, frogID, kind)
	return err
}

func topTaste(tx *sql.Tx, frogID int64, prefix string) (string, error) {
	var kind string
	err := tx.QueryRow(`SELECT kind FROM tastes WHERE frog_id = ? AND substr(kind, 1, ?) = ?
		ORDER BY n DESC, kind LIMIT 1`, frogID, len(prefix), prefix).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return kind, err
}

// settleFavourites gives every grown frog that has none its favourites.
func settleFavourites(tx *sql.Tx, frogs []frogRow, now time.Time) error {
	for i := range frogs {
		f := &frogs[i]
		if stage, _ := stageOf(now.Sub(time.Unix(f.HatchedAt, 0))); !grown(stage) || f.FavDish != "" {
			continue
		}
		dish, err := topTaste(tx, f.ID, "dish_")
		if err != nil {
			return err
		}
		worn, err := topTaste(tx, f.ID, "hat_")
		if err != nil {
			return err
		}
		// A frog from before tastes were counted goes by what it has on it.
		if _, ok := dishOfKind(f.Fed); dish == "" && ok {
			dish = f.Fed
		}
		if worn == "" && f.Hat.valid() {
			worn = hatKind(f.Hat)
		}
		if dish == "" {
			dish = seedDish(f.Seed)
		}
		if worn == "" {
			worn = seedHat(f.Seed)
		}
		f.FavDish, f.FavHat = dish, worn
		if _, err := tx.Exec(`UPDATE frogs SET fav_dish = ?, fav_hat = ? WHERE id = ?`, dish, worn, f.ID); err != nil {
			return err
		}
	}
	return nil
}

type favView struct {
	Dish dishView `json:"dish"`
	Hat  hat      `json:"hat"`
	Name string   `json:"hat_name"`
	Tier int      `json:"hat_tier"`
}

func favOf(f frogRow) *favView {
	d, ok := dishOfKind(f.FavDish)
	h, ok2 := hatOfKind(f.FavHat)
	if !ok || !ok2 {
		return nil
	}
	return &favView{dishViewOf(d, 0), h, h.name(), h.tier()}
}

// favLine is what the bubble says about a favourite on the days it does,
// which is one day in three and the same all day.
func favLine(f frogRow, pondID int64, day string, now time.Time) string {
	fav := favOf(f)
	if fav == nil || seedPick(uint32(pondID), "fav "+day, 3) != 0 {
		return ""
	}
	dish, hatName := strings.ToLower(fav.Dish.Name), strings.ToLower(fav.Name)
	full := f.Fed == f.FavDish && fullUntil(f) > now.Unix()
	wearing := hatKind(f.Hat) == f.FavHat
	lines := []string{
		"It's been thinking about " + dish + " all day, its favourite.",
		"It says nothing on the pond beats " + dish + ".",
		"It likes the " + hatName + " best of all its hats.",
		"It would wear the " + hatName + " every day if it could.",
	}
	switch {
	case full && wearing:
		lines = []string{"It's full of " + dish + " with the " + hatName + " on, its favourite of both."}
	case full:
		lines = []string{
			"It's full of " + dish + ", its favourite, and will be a good while yet.",
			"It had " + dish + ", its favourite, and it's very pleased with you.",
		}
	case wearing:
		lines = []string{
			"It has the " + hatName + " on, the one it likes best.",
			"It's wearing the " + hatName + ", its favourite, and keeps checking the water.",
		}
	}
	return lines[seedPick(uint32(pondID), "which "+day, len(lines))]
}
