package main

import (
	"database/sql"
	"fmt"
)

// goal is a part of the game the pond hasn't used much, shown under what's
// waiting. To is the pond page's view it opens, like a todo's.
type goal struct {
	Kind  string `json:"kind"`
	Icon  string `json:"icon"`
	Label string `json:"label"`
	To    string `json:"to"`
	// Count is how far along it is, for the readout on the row.
	Count string `json:"count,omitempty"`
	Done  bool   `json:"done,omitempty"`
	// First marks a step of the first day list.
	First bool `json:"first,omitempty"`
}

// Listing on the stall and buying a share leave nothing behind once the
// thing sells, so each sets a bit in ponds.tried the first time.
const (
	triedStall = 1
	triedShare = 2
)

const goalsShown = 3

func markTried(tx *sql.Tx, id int64, bit int) error {
	_, err := tx.Exec(`UPDATE ponds SET tried = tried | ? WHERE id = ?`, bit, id)
	return err
}

// goalsOf is the first day's steps until their prize is claimed, and after
// that the first few things down the list the pond has never done. One that
// shares a todo's kind stands in for it on the page.
func goalsOf(v *view, p *pond) []goal {
	out := []goal{}
	if v.Starter != nil {
		for _, st := range v.Starter.Steps {
			out = append(out, goal{Kind: st.Kind, Icon: st.Icon, Label: st.Label, To: st.To, Done: st.Done, First: true})
		}
		return out
	}
	add := func(kind, icon, label, to, count string) {
		if len(out) < goalsShown && v.Open.has(to) {
			out = append(out, goal{Kind: kind, Icon: icon, Label: label, To: to, Count: count})
		}
	}
	saved := func(cost int) string { return fmt.Sprintf("%d/%d", min(v.Pebbles, cost), cost) }
	b := v.Bank

	hands, working := 0, 0
	for _, f := range v.Frogs {
		if f.Work == nil {
			continue
		}
		hands++
		if f.Work.Job != "" {
			working++
		}
	}
	if hands > 0 && working == 0 {
		add("idle", "sticks", "Put a frog to work", "reeds", "")
	}
	if b.Gravel.Level == 0 {
		add("bar", "i_bank", "Build the gravel bar", "bank", saved(b.Gravel.Cost))
	}
	friends := 0
	for _, n := range v.Neighbours.All {
		if n.Points > 0 {
			friends++
		}
	}
	if friends == 0 {
		add("gift", "i_folk", "Give a neighbour a dish", "neighbours", "")
	}
	if p.Tried&triedStall == 0 && len(v.Stall.Rows) == 0 && v.Stall.Takings == 0 {
		add("stall", "i_stall", "List something on the stall", "stall", "")
	}
	played, next := 0, arcadeView{}
	for _, g := range append([]arcadeView{{Key: "dive", Name: "Dive", Best: v.Dive.Best}}, v.Arcade...) {
		if g.Best > 0 {
			played++
		} else if next.Key == "" {
			next = g
		}
	}
	if next.Key != "" {
		add("disk", "i_play", "Try "+next.Name, next.Key, fmt.Sprintf("%d/%d", played, len(v.Arcade)+1))
	}
	if b.Gravel.Level > 0 && b.Garden.Level == 0 {
		add("plots", "watercress", "Build the garden", "garden", saved(b.Garden.Cost))
	}
	if len(v.Frogs) == 1 && v.Frogs[0].LayIn >= 0 {
		add("egg", "f_reeds", "Lay an egg on the bench", "reeds", "")
	}
	if p.Tried&triedShare == 0 && v.Market.In == 0 {
		add("share", "i_market", "Buy a share on the market", "market", "")
	}
	if b.Burrows.Level > 0 && len(b.Burrows.Lodgers) == 0 {
		add("lodger", "i_folk", "Take in a lodger", "bank", "")
	}
	if n := len(v.Kitchen.Cookbook); b.Garden.Level > 0 && n < v.Kitchen.Of {
		add("cook", "bowl", "Cook something new", "garden", fmt.Sprintf("%d/%d", n, v.Kitchen.Of))
	}
	return out
}
