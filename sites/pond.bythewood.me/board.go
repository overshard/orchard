package main

import (
	"math"
	"sort"
	"strings"
	"time"
)

// A day's flow has to be this far off its usual to make the paper.
const boardFlowMin = 0.15

type boardRow struct {
	Name  string  `json:"name"`
	Price int     `json:"price"`
	Pct   float64 `json:"pct"`
}

type boardFlow struct {
	What string `json:"what"`
	Pct  int    `json:"pct"`
}

// board is the paper's market page, the day's biggest movers, the goods and
// whatever the ponds did a lot more or less of yesterday.
type board struct {
	Up    []boardRow  `json:"up"`
	Down  []boardRow  `json:"down"`
	Goods []boardRow  `json:"goods"`
	Flows []boardFlow `json:"flows"`
}

func pctOf(price, prev float64) float64 {
	if prev == 0 {
		return 0
	}
	return math.Round((price-prev)/prev*1000) / 10
}

func boardOf(q querier, firms []firmView, day string) (*board, error) {
	b := &board{Up: []boardRow{}, Down: []boardRow{}, Goods: []boardRow{}, Flows: []boardFlow{}}
	var moved []boardRow
	for _, f := range firms {
		r := boardRow{Name: f.Name, Price: f.Price, Pct: pctOf(f.price, f.prev)}
		if f.Kind == kindGood {
			b.Goods = append(b.Goods, r)
			continue
		}
		moved = append(moved, r)
	}
	sort.SliceStable(moved, func(i, j int) bool { return moved[i].Pct > moved[j].Pct })
	for _, r := range moved {
		if r.Pct > 0 && len(b.Up) < 3 {
			b.Up = append(b.Up, r)
		}
	}
	for i := len(moved) - 1; i >= 0; i-- {
		if r := moved[i]; r.Pct < 0 && len(b.Down) < 3 {
			b.Down = append(b.Down, r)
		}
	}

	book, err := loadFlows(q, day)
	if err != nil {
		return nil, err
	}
	before, seen := dayShift(day, -1), map[string]bool{}
	for _, a := range assets {
		if a.Kind != kindShare || a.flow == "" || seen[a.flow] {
			continue
		}
		seen[a.flow] = true
		m := book.ratio(a.flow, before, time.Time{}, true) - 1
		if math.Abs(m) < boardFlowMin {
			continue
		}
		b.Flows = append(b.Flows, boardFlow{What: strings.ToUpper(a.Moves[:1]) + a.Moves[1:], Pct: int(math.Round(m * 100))})
	}
	sort.SliceStable(b.Flows, func(i, j int) bool { return math.Abs(float64(b.Flows[i].Pct)) > math.Abs(float64(b.Flows[j].Pct)) })
	if len(b.Flows) > 4 {
		b.Flows = b.Flows[:4]
	}
	return b, nil
}
