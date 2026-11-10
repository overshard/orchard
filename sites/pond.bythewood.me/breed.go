package main

import (
	"database/sql"
	"encoding/json"
	"math/rand/v2"
	"time"
)

// A frog off the egg bench is its parent with the numbers nudged, which its
// seed can't rebuild, so what changed is kept beside the seed as mods. The
// seed still gives it a name, a pupil, a nature and a call of its own.
type mods struct {
	Morph   string   `json:"morph"`
	Pattern string   `json:"pattern"`
	Hue     float64  `json:"hue"`
	Sat     float64  `json:"sat"`
	Lit     float64  `json:"lit"`
	BW      float64  `json:"bw"`
	BH      float64  `json:"bh"`
	Eye     float64  `json:"eye"`
	Took    []string `json:"took,omitempty"`
}

func parseMods(raw string) *mods {
	if raw == "" {
		return nil
	}
	var m mods
	if json.Unmarshal([]byte(raw), &m) != nil || morphNamed(m.Morph) == nil {
		return nil
	}
	return &m
}

func morphNamed(name string) *morph {
	for i := range morphs {
		if morphs[i].Name == name {
			return &morphs[i]
		}
	}
	return nil
}

func genomeOf(seed uint32, m *mods) genome {
	g := newGenome(seed)
	if m != nil {
		g.Morph, g.weight, g.Pattern = m.Morph, morphNamed(m.Morph).W, m.Pattern
	}
	return g
}

func lookFor(seed uint32, m *mods) look {
	l := lookOf(seed)
	if m != nil {
		l.morph, l.pattern = *morphNamed(m.Morph), m.Pattern
		l.hue, l.sat, l.lit, l.bw, l.bh, l.eye = m.Hue, m.Sat, m.Lit, m.BW, m.BH, m.Eye
	}
	return l
}

// Which morph each dye on the wheel leans an egg toward.
var dyeMorph = map[string]string{
	"dye_blue": "BLUE", "dye_gold": "GOLDEN", "dye_berry": "STRAWBERRY",
	"dye_olive": "OLIVE", "dye_brown": "WOOD BROWN", "dye_grey": "GREY",
}

const (
	// A dye takes about two times in three, so it leans the dice and never
	// sets them, and about one egg in sixteen throws back to anything at all.
	dyeTakes  = 0.67
	throwback = 0.0625

	layEvery     = 7 * 24 * time.Hour
	startingPads = 3
	mostPads     = 8
)

// padCost doubles with every pad past the three a pond starts with.
func padCost(have int) int { return 400 << (have - startingPads) }

func clamp(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }

// breed lays an egg from a parent with a new seed, and dye is "" or one of
// dyeMorph's keys.
func breed(parent look, seed uint32, dye string) mods {
	child := lookOf(seed)
	nudge := func(by float64) float64 { return (rand.Float64()*2 - 1) * by }
	m := mods{
		Morph: parent.morph.Name, Pattern: parent.pattern, Sat: parent.sat,
		Hue: parent.hue + nudge(7), Lit: parent.lit + nudge(3),
		BW: clamp(parent.bw+nudge(0.8), 10, 15.5), BH: clamp(parent.bh+nudge(0.6), 6.6, 10.4),
		Eye: clamp(parent.eye+nudge(0.3), 2.3, 4.4),
	}
	if rand.Float64() < 0.2 {
		m.Pattern = child.pattern
	}
	if rand.Float64() < throwback {
		m.Morph, m.Hue, m.Sat, m.Lit = child.morph.Name, child.hue, child.sat, child.lit
		m.Took = append(m.Took, "throwback")
	}
	if name, ok := dyeMorph[dye]; ok && rand.Float64() < dyeTakes {
		d := morphNamed(name)
		m.Morph, m.Hue, m.Sat, m.Lit = d.Name, (d.H[0]+d.H[1])/2, (d.S[0]+d.S[1])/2, (d.L[0]+d.L[1])/2
		m.Took = append(m.Took, "dye")
	}
	// Colour drifts over generations but stays near what its morph looks like.
	d := morphNamed(m.Morph)
	m.Hue = clamp(m.Hue, d.H[0]-15, d.H[1]+15)
	m.Lit = clamp(m.Lit, d.L[0]-6, d.L[1]+6)
	return m
}

// ---------------------------------------------------------------- the reeds

type frogRow struct {
	ID        int64
	Seed      uint32
	Mods      *mods
	HatchedAt int64
	LaidAt    int64
	Front     bool
	Hat       hat
}

func loadFrogs(q querier, pondID int64) ([]frogRow, error) {
	rows, err := q.Query(`SELECT id, seed, genome_json, hatched_at, laid_at, out_front, hat_shape, hat_colour
		FROM frogs WHERE pond_id = ? ORDER BY id`, pondID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []frogRow
	for rows.Next() {
		var f frogRow
		var raw string
		if err := rows.Scan(&f.ID, &f.Seed, &raw, &f.HatchedAt, &f.LaidAt, &f.Front, &f.Hat.Shape, &f.Hat.Colour); err != nil {
			return nil, err
		}
		f.Mods = parseMods(raw)
		out = append(out, f)
	}
	return out, rows.Err()
}

func grown(stage string) bool { return stage == "frog" || stage == "elder" }

// layIn is how long until a frog can lay, zero when it can now, and -1 when
// it isn't grown yet.
func layIn(f frogRow, now time.Time) time.Duration {
	if stage, _ := stageOf(now.Sub(time.Unix(f.HatchedAt, 0))); !grown(stage) {
		return -1
	}
	return max(0, time.Unix(f.LaidAt, 0).Add(layEvery).Sub(now))
}

type laid struct {
	Frog frogView `json:"frog"`
	Pond *view    `json:"pond"`
}

func (s *store) lay(id, frogID int64, dye string) (*laid, error) {
	out := &laid{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		frogs, err := loadFrogs(tx, p.ID)
		if err != nil {
			return err
		}
		if len(frogs) >= p.Pads {
			return errFullPads
		}
		var parent *frogRow
		for i := range frogs {
			if frogs[i].ID == frogID {
				parent = &frogs[i]
			}
		}
		if parent == nil {
			return errNotHeld
		}
		if layIn(*parent, now) != 0 {
			return errNotYet
		}
		if dye != "" {
			if _, ok := dyeMorph[dye]; !ok {
				return errNotHeld
			}
			res, err := tx.Exec(`UPDATE items SET count = count - 1 WHERE pond_id = ? AND kind = ? AND count > 0`, p.ID, dye)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return errNotHeld
			}
		}
		seed := randomSeed()
		m := breed(lookFor(parent.Seed, parent.Mods), seed, dye)
		raw, _ := json.Marshal(m)
		if _, err := tx.Exec(`UPDATE frogs SET laid_at = ? WHERE id = ?`, now.Unix(), parent.ID); err != nil {
			return err
		}
		g := genomeOf(seed, &m)
		if _, err := tx.Exec(`INSERT INTO frogs (pond_id, seed, genome_json, name, hatched_at, out_front)
			VALUES (?, ?, ?, ?, ?, 0)`, p.ID, seed, string(raw), g.Name, now.Unix()); err != nil {
			return err
		}
		out.Frog = frogViewOf(frogRow{Seed: seed, Mods: &m, HatchedAt: now.Unix()}, now)
		return nil
	})
	out.Pond = v
	return out, err
}

// front brings a frog out front, and the one that was there goes to the reeds
// with its hat on.
func (s *store) front(id, frogID int64) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM frogs WHERE pond_id = ? AND id = ?`, p.ID, frogID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return errNotHeld
		}
		if _, err := tx.Exec(`UPDATE frogs SET hat_shape = ?, hat_colour = ? WHERE id = ?`,
			p.Wearing.Shape, p.Wearing.Colour, p.FrogID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE frogs SET out_front = (id = ?) WHERE pond_id = ?`, frogID, p.ID); err != nil {
			return err
		}
		// savePond writes the hat back onto whichever frog is out front.
		return tx.QueryRow(`SELECT id, seed, genome_json, hatched_at, hat_shape, hat_colour FROM frogs WHERE id = ?`, frogID).
			Scan(&p.FrogID, &p.Seed, &p.ModsJSON, &p.HatchedAt, &p.Wearing.Shape, &p.Wearing.Colour)
	})
}

func (s *store) buyPad(id int64) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if p.Pads >= mostPads {
			return errNotYet
		}
		cost := padCost(p.Pads)
		if p.Pebbles < cost {
			return errPoor
		}
		p.Pebbles -= cost
		p.Pads++
		return nil
	})
}
