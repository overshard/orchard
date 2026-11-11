package main

import (
	"database/sql"
	"math"
	"time"
)

// The far bank is the one thing to buy that never runs out. Each patch puts
// one more bit of scenery behind the frog and does nothing else, and each
// costs about 15% more than the one before, so it's where a big purse goes.
const (
	farStart = 500
	farStep  = 1.15
	// farDrawn is how many patches have a piece of their own on the page.
	// Past it they only add to the count on the sign.
	farDrawn = 30
	// A patch past this costs more pebbles than anybody will ever hold.
	farMost = 200
)

func farCost(n int) int {
	return int(math.Round(farStart*math.Pow(farStep, float64(n))/10)) * 10
}

type farView struct {
	Patches int `json:"patches"`
	Cost    int `json:"cost"`
	Drawn   int `json:"drawn"`
}

func farOf(q querier, id int64) (farView, error) {
	v := farView{Drawn: farDrawn}
	err := q.QueryRow(`SELECT far FROM ponds WHERE id = ?`, id).Scan(&v.Patches)
	if v.Patches < farMost {
		v.Cost = farCost(v.Patches)
	}
	return v, err
}

func (s *store) buyFar(id int64) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		f, err := farOf(tx, p.ID)
		if err != nil {
			return err
		}
		if f.Cost == 0 {
			return errMaxed
		}
		if p.Pebbles < f.Cost {
			return errPoor
		}
		p.Pebbles -= f.Cost
		_, err = tx.Exec(`UPDATE ponds SET far = far + 1 WHERE id = ?`, p.ID)
		return err
	})
}
