package main

import (
	"database/sql"
	"math/rand/v2"
	"time"
)

// The dye works boils three rocks down into a dye for the egg bench. Each
// level is quicker and can make rarer colours, and it holds one batch.
var (
	dyeCost  = []int{200, 450, 1000, 2000, 4000}
	dyeHours = []int{24, 20, 16, 12, 8}
	// The colours a level can make, rarest last.
	dyePool = [][]string{
		{"dye_olive", "dye_brown", "dye_grey"},
		{"dye_olive", "dye_brown", "dye_grey", "dye_gold"},
		{"dye_olive", "dye_brown", "dye_grey", "dye_gold", "dye_berry"},
		{"dye_olive", "dye_brown", "dye_grey", "dye_gold", "dye_berry", "dye_blue"},
		{"dye_olive", "dye_brown", "dye_grey", "dye_gold", "dye_berry", "dye_blue"},
	}
)

// Which way each rock leans the pot. Every colour a level can make keeps a
// little chance whatever goes in.
var rockLeans = map[string]map[string]int{
	"agate":     {"dye_brown": 2, "dye_berry": 1},
	"quartz":    {"dye_grey": 2, "dye_olive": 1},
	"calcite":   {"dye_grey": 1, "dye_olive": 2},
	"pyrite":    {"dye_gold": 3},
	"amethyst":  {"dye_blue": 2, "dye_berry": 1},
	"banded":    {"dye_brown": 1, "dye_berry": 2},
	"fossil":    {"dye_gold": 2, "dye_blue": 2},
	"wood":      {"dye_brown": 3, "dye_olive": 1},
	"moonstone": {"dye_blue": 3, "dye_grey": 2},
}

const rocksABatch = 3

func dyeWeights(level int, rocks []string) map[string]int {
	w := map[string]int{}
	pool := dyePool[level-1]
	for i, d := range pool {
		w[d] = 1
		// The top level leans toward its rarer half.
		if level == buildLevels && i >= len(pool)/2 {
			w[d] = 2
		}
	}
	for _, r := range rocks {
		for d, n := range rockLeans[r] {
			if _, ok := w[d]; ok {
				w[d] += n * 2
			}
		}
	}
	return w
}

func rollDye(level int, rocks []string) string {
	w := dyeWeights(level, rocks)
	pool := dyePool[level-1]
	sum := 0
	for _, d := range pool {
		sum += w[d]
	}
	r := rand.IntN(sum)
	for _, d := range pool {
		if r < w[d] {
			return d
		}
		r -= w[d]
	}
	return pool[0]
}

func batchTime(level int) time.Duration { return time.Duration(dyeHours[level-1]) * time.Hour }

func (s *store) dyeLoad(id int64, rocks []string) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if p.DyeLevel < 1 || len(rocks) != rocksABatch {
			return errNotHeld
		}
		if p.DyeBatch != "" {
			return errNotYet
		}
		need := map[string]int{}
		for _, r := range rocks {
			if _, ok := rockLeans[r]; !ok {
				return errNotHeld
			}
			need[r]++
		}
		for r, n := range need {
			if err := takeItem(tx, p.ID, r, n); err != nil {
				return err
			}
		}
		p.DyeBatch, p.DyeAt = rollDye(p.DyeLevel, rocks), now.Unix()
		return nil
	})
}

type dyed struct {
	Dye  itemView `json:"dye"`
	Pond *view    `json:"pond"`
}

func (s *store) dyeCollect(id int64) (*dyed, error) {
	out := &dyed{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if p.DyeBatch == "" {
			return errNotHeld
		}
		if now.Before(time.Unix(p.DyeAt, 0).Add(batchTime(p.DyeLevel))) {
			return errNotYet
		}
		if err := addItem(tx, p.ID, p.DyeBatch, 1); err != nil {
			return err
		}
		out.Dye = itemView{Kind: p.DyeBatch, Name: itemNames[p.DyeBatch], Count: 1}
		p.DyeBatch, p.DyeAt = "", 0
		return nil
	})
	out.Pond = v
	return out, err
}

type dyeView struct {
	Level int            `json:"level"`
	Cost  int            `json:"cost"`
	Mats  map[string]int `json:"mats,omitempty"`
	Hours int            `json:"hours"`
	Busy  bool           `json:"busy"`
	// Seconds until the batch is done, 0 once it is.
	ReadyIn int64                     `json:"ready_in"`
	Pool    []string                  `json:"pool"`
	Rocks   []itemView                `json:"rocks"`
	Leans   map[string]map[string]int `json:"leans"`
}

func dyeViewOf(p *pond, now time.Time, held map[string]int) dyeView {
	d := dyeView{Level: p.DyeLevel, Rocks: []itemView{}, Pool: []string{}, Leans: rockLeans}
	if d.Level < buildLevels {
		d.Cost = dyeCost[d.Level]
	}
	d.Mats = matsFor("dye", d.Level)
	if d.Level > 0 {
		d.Hours = dyeHours[d.Level-1]
		d.Pool = dyePool[d.Level-1]
	}
	if p.DyeBatch != "" {
		d.Busy = true
		d.ReadyIn = max(0, int64(time.Unix(p.DyeAt, 0).Add(batchTime(p.DyeLevel)).Sub(now)/time.Second))
	}
	for _, r := range rocks {
		d.Rocks = append(d.Rocks, itemView{Kind: r.kind, Name: itemNames[r.kind], Count: held[r.kind]})
	}
	return d
}
