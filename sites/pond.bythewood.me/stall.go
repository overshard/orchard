package main

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"time"
)

// A stall on the bank where the pond's own things go up for sale. Neighbours
// wander by and buy, sooner when the price is low and later when it's dear.
// What's listed leaves the bag until it sells or comes back off the shelf.
const (
	stallSlots = 3
	// The price can go from half the going rate to three times it.
	stallLow, stallHigh = 0.5, 3.0
	// At the going rate a thing sells in about this long.
	stallDay = 24 * time.Hour
)

// A hat or trinket goes for this share of its rate, low enough that a box's
// spares sold at the going rate don't pay back what the box cost.
const boxedShare = 0.35

var (
	thingWorth = [tierCount]int{10, 40, 100, 300, 900}
	foodWorth  = [3]int{5, 15, 120}
	// Under the 280 Moss asks for a dye going cheap.
	benchWorth = [tierCount]int{10, 40, 100, 200, 200}
)

// worthOf is a thing's going rate on the stall, and false for what can't go
// on it, like furniture.
func worthOf(kind string) (int, bool) {
	if h, ok := hatOfKind(kind); ok {
		return int(float64(hatRate[h.tier()]) * boxedShare), true
	}
	if t, ok := trinketOf(kind); ok {
		return int(float64(hatRate[t.tier]) * boxedShare), true
	}
	if d, ok := dishOfKind(kind); ok {
		return dishWorth[d.tier], true
	}
	if i, ok := ingredientOf(kind); ok {
		return foodWorth[ingredients[i].tier], true
	}
	if t, ok := benchTier[kind]; ok {
		return benchWorth[t], true
	}
	if isMaterial(kind) {
		return materialWorth, true
	}
	if t, ok := wizardTier(kind); ok {
		return thingWorth[t], true
	}
	return 0, false
}

// stallCap is the most a thing can be put up for, three times its going rate
// and always under what any shop asks for it, so nothing bought can be
// resold for more.
func stallCap(kind string, worth int) int {
	hi := int(float64(worth) * stallHigh)
	if p, ok := shopFloor(kind); ok {
		hi = min(hi, p-1)
	}
	return max(hi, max(1, int(math.Ceil(float64(worth)*stallLow))))
}

// shopFloor is the least any shop ever asks for a kind.
func shopFloor(kind string) (int, bool) {
	if h, ok := hatOfKind(kind); ok {
		if p := wrenSells[h.tier()]; p > 0 {
			return p, true
		}
		return 0, false
	}
	for _, r := range mossRocks {
		if r.kind == kind {
			return r.price, true
		}
	}
	switch {
	case strings.HasPrefix(kind, "dye_"):
		return bargainPrice, true
	case strings.HasPrefix(kind, "stamp_"):
		return stampPrice, true
	case strings.HasPrefix(kind, "drops_"):
		return dropsPrice, true
	}
	if i, ok := ingredientOf(kind); ok && !ingredients[i].trinket {
		return salePrice(larderPrice[ingredients[i].tier]), true
	}
	return 0, false
}

// sellsIn is how long a neighbour takes to buy at a price, a day at the
// going rate, hours for a bargain and weeks at three times.
func sellsIn(price, worth int) time.Duration {
	r := float64(price) / float64(worth)
	h := 24 * math.Pow(r, 2.6) * (0.75 + rand.Float64()*0.5)
	return max(time.Hour, time.Duration(h*float64(time.Hour)))
}

type stallRow struct {
	Slot   int    `json:"slot"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Tier   int    `json:"tier"`
	Hat    *hat   `json:"hat,omitempty"`
	Price  int    `json:"price"`
	Worth  int    `json:"worth"`
	Sold   bool   `json:"sold"`
	Buyer  string `json:"buyer,omitempty"`
	Listed int64  `json:"listed"`
	A      string `json:"a,omitempty"`
	B      string `json:"b,omitempty"`
}

type stallView struct {
	Slots int        `json:"slots"`
	Rows  []stallRow `json:"rows"`
	// Pebbles waiting from what's sold.
	Takings int `json:"takings"`
}

// bagThing is anything a pond holds, hats included, with what can be done
// with it. Free is how many can go to the stall or the wizard, which keep
// the last hat or trinket back for the collection.
type bagThing struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Tier  int    `json:"tier"`
	Sort  string `json:"sort"`
	Count int    `json:"count"`
	Free  int    `json:"free"`
	Worth int    `json:"worth"`
	Max   int    `json:"max"`
	Hat   *hat   `json:"hat,omitempty"`
	// A dish is drawn from its two ingredients.
	A string `json:"a,omitempty"`
	B string `json:"b,omitempty"`
}

func dishParts(kind string) (string, string) {
	if d, ok := dishOfKind(kind); ok {
		return ingredients[d.a].kind, ingredients[d.b].kind
	}
	return "", ""
}

func sortOf(kind string) string {
	switch {
	case strings.HasPrefix(kind, "hat_"):
		return "hat"
	case strings.HasPrefix(kind, "t_"):
		return "trinket"
	case strings.HasPrefix(kind, "dish_"):
		return "dish"
	case isRock(kind):
		return "rock"
	case isMaterial(kind):
		return "material"
	}
	if _, ok := ingredientOf(kind); ok {
		return "food"
	}
	return "thing"
}

func bagOf(held map[string]int, hats []hatView, hoods []hoodView) []bagThing {
	out := []bagThing{}
	add := func(kind string, n int, h *hat) {
		t, _ := wizardTier(kind)
		w, ok := worthOf(kind)
		if !ok || n <= 0 {
			return
		}
		free := n
		if keepsOne(kind) {
			free--
		}
		a, b := dishParts(kind)
		out = append(out, bagThing{kind, thingName(kind), t, sortOf(kind), n, free, w, stallCap(kind, w), h, a, b})
	}
	for k, n := range held {
		add(k, n, nil)
	}
	for _, h := range hats {
		hh := h.hat
		add(hatKind(hh), h.Count, &hh)
	}
	for _, h := range hoods {
		if h.Got {
			hh := h.hat
			out = append(out, bagThing{Kind: hatKind(hh), Name: h.Name, Tier: hh.tier(), Sort: "hat", Count: 1, Hat: &hh})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Sort != out[b].Sort {
			return out[a].Sort < out[b].Sort
		}
		if out[a].Tier != out[b].Tier {
			return out[a].Tier > out[b].Tier
		}
		return out[a].Name < out[b].Name
	})
	return out
}

func stallOf(q querier, id int64, now time.Time) (stallView, error) {
	v := stallView{Slots: stallSlots, Rows: []stallRow{}}
	rows, err := q.Query(`SELECT slot, kind, price, listed_at, sells_at, buyer FROM stall WHERE pond_id = ? ORDER BY slot`, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var r stallRow
		var sells int64
		if err := rows.Scan(&r.Slot, &r.Kind, &r.Price, &r.Listed, &sells, &r.Buyer); err != nil {
			return v, err
		}
		r.Name = thingName(r.Kind)
		r.A, r.B = dishParts(r.Kind)
		r.Tier, _ = wizardTier(r.Kind)
		r.Worth, _ = worthOf(r.Kind)
		if h, ok := hatOfKind(r.Kind); ok {
			r.Hat = &h
		}
		r.Sold = sells <= now.Unix()
		if r.Sold {
			v.Takings += r.Price
		} else {
			r.Buyer = ""
		}
		v.Rows = append(v.Rows, r)
	}
	return v, rows.Err()
}

func takeThing(tx *sql.Tx, id int64, kind string) error {
	need := 1
	if keepsOne(kind) {
		need = 2
	}
	if h, ok := hatOfKind(kind); ok {
		res, err := tx.Exec(`UPDATE hats SET count = count - 1 WHERE pond_id = ? AND shape = ? AND colour = ? AND count >= ?`,
			id, h.Shape, h.Colour, need)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotHeld
		}
		return nil
	}
	res, err := tx.Exec(`UPDATE items SET count = count - 1 WHERE pond_id = ? AND kind = ? AND count >= ?`, id, kind, need)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotHeld
	}
	return nil
}

func giveThing(tx *sql.Tx, id int64, kind string) error {
	if h, ok := hatOfKind(kind); ok {
		_, err := addHat(tx, id, h)
		return err
	}
	if strings.HasPrefix(kind, "t_") {
		_, err := addTrinket(tx, id, kind)
		return err
	}
	return addItem(tx, id, kind, 1)
}

func (s *store) stallList(id int64, kind string, price int) (*view, error) {
	worth, ok := worthOf(kind)
	if !ok || strings.HasPrefix(kind, "f_") {
		return nil, errNotHeld
	}
	lo, hi := max(1, int(math.Ceil(float64(worth)*stallLow))), stallCap(kind, worth)
	if price < lo || price > hi {
		return nil, errBadPrice
	}
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		used := map[int]bool{}
		rows, err := tx.Query(`SELECT slot FROM stall WHERE pond_id = ?`, p.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n int
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			used[n] = true
		}
		if err := rows.Close(); err != nil {
			return err
		}
		slot := -1
		for i := range stallSlots {
			if !used[i] {
				slot = i
				break
			}
		}
		if slot < 0 {
			return errStallFull
		}
		if err := takeThing(tx, p.ID, kind); err != nil {
			return err
		}
		sells := now.Add(sellsIn(price, worth)).Unix()
		_, err = tx.Exec(`INSERT INTO stall (pond_id, slot, kind, price, listed_at, sells_at, buyer) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.ID, slot, kind, price, now.Unix(), sells, residents[rand.IntN(len(residents))].name)
		return err
	})
}

// stallTake takes a thing back off the stall, or collects for it once sold.
func (s *store) stallTake(id int64, slot int) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		var kind string
		var price int
		var sells int64
		err := tx.QueryRow(`SELECT kind, price, sells_at FROM stall WHERE pond_id = ? AND slot = ?`, p.ID, slot).Scan(&kind, &price, &sells)
		if err == sql.ErrNoRows {
			return errNotHeld
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM stall WHERE pond_id = ? AND slot = ?`, p.ID, slot); err != nil {
			return err
		}
		if sells <= now.Unix() {
			p.Pebbles += price
			return nil
		}
		return giveThing(tx, p.ID, kind)
	})
}

// stallCollect takes the pebbles for everything sold, and every neighbour who
// bought something gets a little friendlier for it.
func (s *store) stallCollect(id int64) (*paidOut, error) {
	out := &paidOut{Prizes: []prize{}, From: []int{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		rows, err := tx.Query(`SELECT price, buyer FROM stall WHERE pond_id = ? AND sells_at <= ? ORDER BY slot`, p.ID, now.Unix())
		if err != nil {
			return err
		}
		var buyers []int
		for rows.Next() {
			var price int
			var buyer string
			if err := rows.Scan(&price, &buyer); err != nil {
				rows.Close()
				return err
			}
			out.Pebbles += price
			if who := residentNamed(buyer); who >= 0 {
				buyers = append(buyers, who)
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if out.Pebbles == 0 {
			return errNotYet
		}
		if _, err := tx.Exec(`DELETE FROM stall WHERE pond_id = ? AND sells_at <= ?`, p.ID, now.Unix()); err != nil {
			return err
		}
		p.Pebbles += out.Pebbles
		out.Line = fmt.Sprintf("+%d pebbles.", out.Pebbles)
		for _, who := range buyers {
			zs, err := befriend(tx, p, who, 1, "")
			if err != nil {
				return err
			}
			for _, z := range zs {
				out.Prizes = append(out.Prizes, z)
				out.From = append(out.From, who)
			}
		}
		return nil
	})
	out.Pond = v
	return out, err
}
