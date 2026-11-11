package main

import (
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"sort"
	"time"
)

// Three shops put out stock that is the same for everybody, dealt from the
// day, and each pond can buy what's on a line up to its limit.
const (
	shopWren    = "wren"
	shopBrindle = "brindle"
	shopMoss    = "moss"
	shopFenn    = "fenn"
)

// Tansy's furniture is for the whole pond, sold once each and drawn on the
// water for good. The page draws each by its kind.
var furniture = []struct {
	kind, name string
	price      int
}{
	{"f_lilypad", "A second lily pad", 90},
	{"f_reeds", "Bulrushes", 150},
	{"f_toadstool", "Toadstool to sit under", 240},
	{"f_stones", "Stepping stones", 380},
	{"f_lantern", "Jam jar lantern", 600},
	{"f_teacup", "A cracked teacup", 1500},
}

// What Little Tully asks for a hat, by tier. Legendary hats are never on the shelf.
var wrenSells = [tierCount]int{120, 350, 900, 5000, 0}

// What Big Bean asks for an ingredient, by its tier. A pair of anything costs
// more than the dish it makes is worth, even on a sale, and a test holds that.
var larderPrice = []int{18, 50, 420}

const (
	larderEach = 3
	// A red close on the S&P is a sale at the larder.
	larderSale = 0.75
	// Moth restocks this many times a day at hours nobody can predict, plus
	// once at midnight.
	mossRestocks = 3
)

// Eye drops and the colour they turn an egg's eyes.
var drops = map[string]string{"drops_red": "#e2412d", "drops_pale": "#9fd8e8"}

func randomDrops() string {
	if rand.IntN(2) == 0 {
		return drops["drops_red"]
	}
	return drops["drops_pale"]
}

var stamps = map[string]string{
	"stamp_spots": "SPOTS", "stamp_bands": "BANDS", "stamp_speckled": "SPECKLED",
	"stamp_mask": "MASK", "stamp_stripe": "BACK STRIPE",
}

func init() {
	for _, f := range furniture {
		itemNames[f.kind] = f.name
	}
	itemNames["stamp_spots"] = "Spots stamp"
	itemNames["stamp_bands"] = "Bands stamp"
	itemNames["stamp_speckled"] = "Speckled stamp"
	itemNames["stamp_mask"] = "Mask stamp"
	itemNames["stamp_stripe"] = "Back stripe stamp"
	itemNames["drops_red"] = "Red eye drops"
	itemNames["drops_pale"] = "Pale eye drops"
}

var mossRocks = []struct {
	kind  string
	price int
	w     int
}{
	{"agate", 30, 30}, {"quartz", 60, 24}, {"geode", 90, 20},
	{"amethyst", 140, 12}, {"banded", 220, 9}, {"wood", 300, 7}, {"fossil", 600, 5},
}

const (
	stampPrice = 450
	dropsPrice = 650
	dyePrice   = 900
	// Now and then Moth has a dye going cheap.
	bargainPrice = 280
	bargainOdds  = 4
)

type shopLine struct {
	Slot    int    `json:"slot"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Hat     *hat   `json:"hat,omitempty"`
	Tier    int    `json:"tier"`
	Price   int    `json:"price"`
	Was     int    `json:"was,omitempty"`
	Limit   int    `json:"limit"`
	Bought  int    `json:"bought"`
	Bargain bool   `json:"bargain,omitempty"`
}

func shopRand(key string) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(key))
	n := h.Sum64()
	return rand.New(rand.NewPCG(n, n^0x9e3779b97f4a7c15))
}

// wrenStock is two common hats, an uncommon and a rare, and one day in eight
// the rare is an epic.
func wrenStock(day string) []shopLine {
	r := shopRand("wren " + day)
	tiers := []int{0, 0, 1, 2}
	if r.IntN(8) == 0 {
		tiers[3] = 3
	}
	var out []shopLine
	seen := map[hat]bool{}
	for i, t := range tiers {
		pool := hatsByTier[t]
		h := pool[r.IntN(len(pool))]
		for seen[h] {
			h = pool[r.IntN(len(pool))]
		}
		seen[h] = true
		out = append(out, shopLine{Slot: i, Kind: "hat", Name: h.name(), Hat: &h, Tier: t, Price: wrenSells[t], Limit: 1})
	}
	return out
}

// larderStock is two plain things, a better one and a dear one, cheaper on a
// red day.
func larderStock(day string, sale bool) []shopLine {
	r := shopRand("brindle " + day)
	var by [3][]int
	for i, g := range ingredients {
		if !g.trinket {
			by[g.tier] = append(by[g.tier], i)
		}
	}
	pick := func(t int, not int) int {
		for {
			i := by[t][r.IntN(len(by[t]))]
			if i != not {
				return i
			}
		}
	}
	a := pick(0, -1)
	idx := []int{a, pick(0, a), pick(1, -1), pick(2, -1)}
	var out []shopLine
	for slot, i := range idx {
		g := ingredients[i]
		l := shopLine{Slot: slot, Kind: g.kind, Name: g.name, Tier: g.tier, Price: larderPrice[g.tier], Limit: larderEach}
		if sale {
			l.Was, l.Price = l.Price, salePrice(l.Price)
		}
		out = append(out, l)
	}
	return out
}

func salePrice(p int) int { return (p*int(larderSale*100) + 99) / 100 }

// mossRestock is which stock Moth has out at a moment, as a key, and when
// the next one comes in.
func mossRestock(now time.Time) (string, time.Time) {
	local := now.In(pondZone)
	day := local.Format("2006-01-02")
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, pondZone)
	r := shopRand("moss hours " + day)
	at := make([]int, mossRestocks)
	for i := range at {
		at[i] = 60 + r.IntN(22*60)
	}
	sort.Ints(at)
	mins := int(local.Sub(midnight) / time.Minute)
	n := 0
	for _, m := range at {
		if mins >= m {
			n++
		}
	}
	next := midnight.AddDate(0, 0, 1)
	if n < len(at) {
		next = midnight.Add(time.Duration(at[n]) * time.Minute)
	}
	return fmt.Sprintf("%s#%d", day, n), next
}

// mossStock is a rock and a stamp, eye drops or a dye, and sometimes a dye
// going cheap.
func mossStock(key string) []shopLine {
	r := shopRand("moss " + key)
	sum := 0
	for _, x := range mossRocks {
		sum += x.w
	}
	roll := r.IntN(sum)
	rock := mossRocks[0]
	for _, x := range mossRocks {
		if roll < x.w {
			rock = x
			break
		}
		roll -= x.w
	}
	out := []shopLine{{Slot: 0, Kind: rock.kind, Name: itemNames[rock.kind], Price: rock.price, Limit: 1}}
	switch r.IntN(3) {
	case 0:
		keys := sortedKeys(stamps)
		k := keys[r.IntN(len(keys))]
		out = append(out, shopLine{Slot: 1, Kind: k, Name: itemNames[k], Price: stampPrice, Limit: 1})
	case 1:
		keys := sortedKeys(drops)
		k := keys[r.IntN(len(keys))]
		out = append(out, shopLine{Slot: 1, Kind: k, Name: itemNames[k], Price: dropsPrice, Limit: 1})
	default:
		k := wheelDyes[r.IntN(len(wheelDyes))]
		out = append(out, shopLine{Slot: 1, Kind: k, Name: itemNames[k], Price: dyePrice, Limit: 1})
	}
	if r.IntN(bargainOdds) == 0 {
		k := wheelDyes[r.IntN(len(wheelDyes))]
		out = append(out, shopLine{Slot: 2, Kind: k, Name: itemNames[k], Price: bargainPrice, Was: dyePrice, Limit: 1, Bargain: true})
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// stockOf is a shop's lines as they stand, and the key its purchases are
// counted under.
func (s *store) stockOf(shop string, now time.Time) ([]shopLine, string) {
	day := dayOf(now)
	switch shop {
	case shopWren:
		return wrenStock(day), "wren " + day
	case shopBrindle:
		sale := dayOfReading(s.day.reading(now), now).Water == "murky"
		return larderStock(day, sale), "brindle " + day
	case shopMoss:
		key, _ := mossRestock(now)
		return mossStock(key), "moss " + key
	case shopFenn:
		var out []shopLine
		for i, f := range furniture {
			out = append(out, shopLine{Slot: i, Kind: f.kind, Name: f.name, Price: f.price, Limit: 1})
		}
		return out, ""
	}
	return nil, ""
}

// boughtOf counts what a pond took off a stock, and for Tansy, whose stock
// never changes, what it owns.
func boughtOf(q querier, id int64, stock string) (map[int]int, error) {
	if stock == "" {
		out := map[int]int{}
		for i, f := range furniture {
			var n int
			err := q.QueryRow(`SELECT count FROM items WHERE pond_id = ? AND kind = ?`, id, f.kind).Scan(&n)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	}
	rows, err := q.Query(`SELECT slot, n FROM bought WHERE pond_id = ? AND stock = ?`, id, stock)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]int{}
	for rows.Next() {
		var slot, n int
		if err := rows.Scan(&slot, &n); err != nil {
			return nil, err
		}
		out[slot] = n
	}
	return out, rows.Err()
}

type bought struct {
	Line  shopLine `json:"line"`
	Spare bool     `json:"spare,omitempty"`
	Pond  *view    `json:"pond"`
}

func (s *store) buy(id int64, shop string, slot int) (*bought, error) {
	out := &bought{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		lines, stock := s.stockOf(shop, now)
		var line *shopLine
		for i := range lines {
			if lines[i].Slot == slot {
				line = &lines[i]
			}
		}
		if line == nil {
			return errNotHeld
		}
		had, err := boughtOf(tx, p.ID, stock)
		if err != nil {
			return err
		}
		if had[slot] >= line.Limit {
			return errSoldOut
		}
		if p.Pebbles < line.Price {
			return errPoor
		}
		p.Pebbles -= line.Price
		// Tansy's stock has no key, owning the thing is the record.
		if stock != "" {
			if _, err := tx.Exec(`INSERT INTO bought (pond_id, stock, slot, n, day) VALUES (?, ?, ?, 1, ?)
				ON CONFLICT (pond_id, stock, slot) DO UPDATE SET n = n + 1`, p.ID, stock, slot, dayOf(now)); err != nil {
				return err
			}
		}
		if line.Hat != nil {
			n, err := addHat(tx, p.ID, *line.Hat, p.today)
			if err != nil {
				return err
			}
			out.Spare = n > 1
		} else if err := addItem(tx, p.ID, line.Kind, 1); err != nil {
			return err
		}
		line.Bought = had[slot] + 1
		out.Line = *line
		return nil
	})
	out.Pond = v
	return out, err
}

type shopView struct {
	Lines []shopLine `json:"lines"`
	// Seconds until the stock changes.
	NextIn int64 `json:"next_in"`
	Sale   bool  `json:"sale,omitempty"`
}

func (s *store) shopsOf(q querier, id int64, now time.Time) (map[string]shopView, error) {
	out := map[string]shopView{}
	local := now.In(pondZone)
	tomorrow := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, pondZone)
	for _, shop := range []string{shopWren, shopBrindle, shopMoss, shopFenn} {
		lines, stock := s.stockOf(shop, now)
		had, err := boughtOf(q, id, stock)
		if err != nil {
			return nil, err
		}
		sv := shopView{Lines: lines, NextIn: int64(tomorrow.Sub(now) / time.Second)}
		for i := range sv.Lines {
			sv.Lines[i].Bought = had[sv.Lines[i].Slot]
			if sv.Lines[i].Was > 0 && shop == shopBrindle {
				sv.Sale = true
			}
		}
		if shop == shopMoss {
			_, next := mossRestock(now)
			sv.NextIn = int64(next.Sub(now) / time.Second)
		}
		out[shop] = sv
	}
	return out, nil
}
