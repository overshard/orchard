package main

import (
	"database/sql"
	"time"
)

// A new pond's first day is a short list, spin the wheel, turn the log, race
// once and save the key, with a few pebbles for finishing it. Ponds made
// before the list existed start it already claimed.
const (
	starterSpin  = 1
	starterLog   = 2
	starterDive  = 4
	starterTaken = 16
	starterPrize = 60
)

type starterStep struct {
	Label string `json:"label"`
	To    string `json:"to"`
	Done  bool   `json:"done"`
}

type starterView struct {
	Steps []starterStep `json:"steps"`
	Ready bool          `json:"ready"`
	Prize int           `json:"prize"`
}

func markStarter(tx *sql.Tx, id int64, bit int) error {
	_, err := tx.Exec(`UPDATE ponds SET starter = starter | ? WHERE id = ?`, bit, id)
	return err
}

func starterOf(q querier, p *pond) (*starterView, error) {
	var bits int
	if err := q.QueryRow(`SELECT starter FROM ponds WHERE id = ?`, p.ID).Scan(&bits); err != nil {
		return nil, err
	}
	if bits&starterTaken != 0 {
		return nil, nil
	}
	v := &starterView{Prize: starterPrize, Steps: []starterStep{
		{"Spin the wheel", "wheel", bits&starterSpin != 0},
		{"Turn the old log", "play", bits&starterLog != 0},
		{"Race in Dive", "dive", bits&starterDive != 0},
		{"Save your pond key", "key", p.KeySavedAt.Valid},
	}}
	v.Ready = true
	for _, st := range v.Steps {
		v.Ready = v.Ready && st.Done
	}
	return v, nil
}

func (s *store) starterClaim(id int64) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		st, err := starterOf(tx, p)
		if err != nil {
			return err
		}
		if st == nil || !st.Ready {
			return errNotYet
		}
		p.Pebbles += starterPrize
		return markStarter(tx, p.ID, starterTaken)
	})
}
