package main

import (
	"database/sql"
	"strings"
)

// The rock shelf is every rock a pond has had. The dye works boils rocks
// away, so rocks_found keeps the shelf from losing one.
type rock struct {
	kind string
	tier int
}

var rocks = []rock{
	{"agate", 0}, {"quartz", 0}, {"calcite", 1}, {"pyrite", 1}, {"amethyst", 1},
	{"banded", 2}, {"wood", 2}, {"fossil", 3}, {"moonstone", 4},
}

func init() {
	itemNames["wood"] = "Petrified wood"
	itemNames["moonstone"] = "Moonstone"
}

func isRock(kind string) bool {
	for _, r := range rocks {
		if r.kind == kind {
			return true
		}
	}
	return false
}

// backfillRocks puts on the shelf whatever rocks ponds held before it existed.
func backfillRocks(db *sql.DB) error {
	kinds := make([]string, len(rocks))
	args := make([]any, len(rocks))
	for i, r := range rocks {
		kinds[i], args[i] = "?", r.kind
	}
	_, err := db.Exec(`INSERT INTO rocks_found (pond_id, kind)
		SELECT pond_id, kind FROM items WHERE count > 0 AND kind IN (`+strings.Join(kinds, ",")+`)
		ON CONFLICT DO NOTHING`, args...)
	return err
}

type rockView struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Tier  int    `json:"tier"`
	Count int    `json:"count"`
	Found bool   `json:"found"`
}

func rocksOf(q querier, id int64, held map[string]int) ([]rockView, error) {
	found := map[string]bool{}
	rows, err := q.Query(`SELECT kind FROM rocks_found WHERE pond_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		found[k] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]rockView, 0, len(rocks))
	for _, r := range rocks {
		out = append(out, rockView{r.kind, itemNames[r.kind], r.tier, held[r.kind], found[r.kind] || held[r.kind] > 0})
	}
	return out, nil
}
