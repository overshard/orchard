package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"hash/fnv"
	"time"
)

// keepHome is the setting that keeps a pond's frog off every other pond, as
// a visitor and on a stamp. Off by default, so a frog is out and about.
func (s *store) keepHome(id int64, home bool) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		_, err := tx.Exec(`UPDATE ponds SET hidden = ? WHERE id = ?`, home, p.ID)
		return err
	})
}

// caller is another frog as one pond may see it, its look and name and
// nothing that leads back to its pond.
type caller struct {
	Name  string `json:"name"`
	Seed  uint32 `json:"seed"`
	Mods  *mods  `json:"mods"`
	Stage string `json:"stage"`
	Day   int    `json:"day"`
	Hat   hat    `json:"hat"`
}

func callerOf(f frogRow, now time.Time) caller {
	v := frogViewOf(f, now)
	return caller{Name: titled(v.Name), Seed: f.Seed, Mods: f.Mods, Stage: v.Stage, Day: v.Day, Hat: f.Hat}
}

// frontOf is the frog out front of another pond, as a caller, or nil when
// that pond keeps its frog home.
func frontOf(q querier, id int64, now time.Time) (*caller, error) {
	var f frogRow
	var raw string
	var home bool
	err := q.QueryRow(`SELECT p.hidden, f.seed, f.genome_json, f.hatched_at, f.hat_shape, f.hat_colour, f.name
		FROM ponds p JOIN frogs f ON f.pond_id = p.id AND f.out_front = 1 WHERE p.id = ?`, id).
		Scan(&home, &f.Seed, &raw, &f.HatchedAt, &f.Hat.Shape, &f.Hat.Colour, &f.Name)
	if errors.Is(err, sql.ErrNoRows) || home {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.Mods = parseMods(raw)
	c := callerOf(f, now)
	return &c, nil
}

type visitorView struct {
	caller
	// From is pond for another pond's frog, resident or wild.
	From string `json:"from"`
	Gift prize  `json:"gift"`
}

func parseVisitor(raw string) *visitorView {
	var v visitorView
	if raw == "" || json.Unmarshal([]byte(raw), &v) != nil {
		return nil
	}
	return &v
}

// A visitor leaves something small and common, so a day of them is worth
// less than the old log.
var visitorGifts = []struct{ kind, said string }{
	{"duckweed", "a sprig of duckweed"}, {"watercress", "a bunch of watercress"}, {"cricket", "a cricket"},
	{"agate", "an agate"}, {"quartz", "a bit of quartz"}, {"t_acorn", "an acorn"},
	{"t_pinecone", "a pine cone"}, {"t_marble", "a marble"}, {"t_boat", "a paper boat"},
}

// A frog off a random seed is grown and wears nothing.
const wildDay = 20

// dealVisitor is the frog that came by while the pond was shut, once a day,
// another pond's out of those open yesterday or today and not kept home, or
// else a resident or a frog off a seed.
func dealVisitor(tx *sql.Tx, p *pond, now time.Time) (*visitorView, error) {
	var had string
	if err := tx.QueryRow(`SELECT visitor_day FROM ponds WHERE id = ?`, p.ID).Scan(&had); err != nil || had == p.today {
		return nil, err
	}
	h := fnv.New32a()
	h.Write([]byte("visitor " + p.PublicID + " " + p.today))
	n := h.Sum32()
	days := []any{p.today, dayOf(now.AddDate(0, 0, -1)), p.ID, now.Add(-24 * time.Hour).Unix()}
	const open = ` FROM ponds q JOIN frogs f ON f.pond_id = q.id AND f.out_front = 1
		WHERE q.visit_day IN (?, ?) AND q.id != ? AND q.hidden = 0 AND f.hatched_at <= ?`
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*)`+open, days...).Scan(&count); err != nil {
		return nil, err
	}
	v := &visitorView{}
	switch {
	case count > 0:
		var id int64
		if err := tx.QueryRow(`SELECT q.id`+open+` ORDER BY q.public_id LIMIT 1 OFFSET ?`, append(days, int(n%uint32(count)))...).Scan(&id); err != nil {
			return nil, err
		}
		c, err := frontOf(tx, id, now)
		if err != nil || c == nil {
			return nil, err
		}
		v.caller, v.From = *c, "pond"
	case n%2 == 0:
		r := residents[(n/2)%uint32(len(residents))]
		v.caller = caller{Name: r.name, Seed: r.seed, Stage: "frog", Day: wildDay, Hat: hat{Shape: r.hat}}
		v.From = "resident"
	default:
		seed := n*2654435761 + 1
		v.caller = caller{Name: titled(newGenome(seed).Name), Seed: seed, Stage: "frog", Day: wildDay}
		v.From = "wild"
	}
	gift := visitorGifts[(n/7)%uint32(len(visitorGifts))]
	v.Gift = prize{Item: gift.kind, Text: gift.said}
	if err := award(tx, p, &v.Gift); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`UPDATE ponds SET visitor = ?, visitor_day = ? WHERE id = ?`, string(raw), p.today, p.ID)
	return v, err
}
