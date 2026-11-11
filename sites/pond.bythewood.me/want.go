package main

import (
	"database/sql"
	"time"
)

// A pond can ask Little Tully to look out for one hat it hasn't found. Every
// spare he buys counts toward it, and once they add up to the hat's bar it's
// on his shelf for that pond at its tier's price, a legendary included.

// wantSlot is the wanted hat's place on the shelf, past any the day deals.
const wantSlot = 9

// wantBar is how much in spares a hat takes, what six spares of its own tier
// sell to him for.
func wantBar(tier int) int { return 6 * (hatRate[tier] / 4) }

type wantView struct {
	// Hat is nil with nothing asked for.
	Hat   *hatView `json:"hat"`
	Fill  int      `json:"fill"`
	Bar   int      `json:"bar"`
	Price int      `json:"price"`
	Ready bool     `json:"ready"`
	// What a hat of each tier takes and costs, for one not picked yet.
	Bars   [tierCount]int `json:"bars"`
	Prices [tierCount]int `json:"prices"`
}

func wantOf(p *pond, held map[hat]int) wantView {
	v := wantView{Fill: p.WantFill, Prices: wrenSells}
	for t := range v.Bars {
		v.Bars[t] = wantBar(t)
	}
	h, ok := hatOfKind(p.WantHat)
	if !ok || !h.valid() || held[h] > 0 {
		return v
	}
	t := h.tier()
	v.Hat = &hatView{hat: h, Name: h.name(), Tier: t, Wren: wrenPays(h)}
	v.Bar, v.Price = wantBar(t), wrenSells[t]
	v.Ready = v.Fill >= v.Bar
	return v
}

// found lets go of the wanted hat when it turns up some other way. What the
// spares have added up to stays for the next one.
func (p *pond) found(h hat) {
	if p.WantHat == hatKind(h) {
		p.WantHat = ""
	}
}

// want asks for a hat, or for nothing with shape 0. Only a hat that comes out
// of a box can be asked for, so never a hood or a site's hat, and only one
// the pond hasn't found.
func (s *store) want(id int64, h hat) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if h.Shape == 0 {
			p.WantHat = ""
			return nil
		}
		if !h.valid() {
			return errNotHeld
		}
		var n int
		err := tx.QueryRow(`SELECT count FROM hats WHERE pond_id = ? AND shape = ? AND colour = ?`, p.ID, h.Shape, h.Colour).Scan(&n)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if n > 0 {
			return errHaveIt
		}
		p.WantHat = hatKind(h)
		return nil
	})
}

func wantLine(h hat) shopLine {
	return shopLine{Slot: wantSlot, Kind: "hat", Name: h.name(), Hat: &h, Tier: h.tier(), Price: wrenSells[h.tier()], Limit: 1, Wanted: true}
}

// buyWanted takes the wanted hat off the shelf. It spends the hat's bar and
// leaves anything over it for the next one.
func buyWanted(tx *sql.Tx, p *pond) (shopLine, error) {
	h, ok := hatOfKind(p.WantHat)
	if !ok || !h.valid() || p.WantFill < wantBar(h.tier()) {
		return shopLine{}, errNotHeld
	}
	line := wantLine(h)
	if p.Pebbles < line.Price {
		return line, errPoor
	}
	n, err := addHat(tx, p.ID, h, p.today)
	if err != nil {
		return line, err
	}
	// It can only be asked for while unfound, so a second one means the
	// column outlived the find and there's nothing to sell.
	if n > 1 {
		return line, errNotHeld
	}
	p.Pebbles -= line.Price
	p.WantFill -= wantBar(h.tier())
	p.WantHat = ""
	line.Bought = 1
	return line, nil
}
