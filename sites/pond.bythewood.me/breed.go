package main

import (
	"cmp"
	"database/sql"
	"encoding/json"
	"math/rand/v2"
	"slices"
	"time"
)

// A frog off the egg bench is its parent with the numbers nudged, which its
// seed can't rebuild, so what changed is kept beside the seed as mods. The
// seed still gives it a name, a pupil, a nature and a call of its own.
type mods struct {
	Morph   string  `json:"morph"`
	Pattern string  `json:"pattern"`
	Hue     float64 `json:"hue"`
	Sat     float64 `json:"sat"`
	Lit     float64 `json:"lit"`
	BW      float64 `json:"bw"`
	BH      float64 `json:"bh"`
	Eye     float64 `json:"eye"`
	// Iris is an eye colour from drops, "" for the morph's own.
	Iris string   `json:"iris,omitempty"`
	Took []string `json:"took,omitempty"`
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
		g.drops = m.Iris != ""
	}
	return g
}

func lookFor(seed uint32, m *mods) look {
	l := lookOf(seed)
	if m != nil {
		l.morph, l.pattern = *morphNamed(m.Morph), m.Pattern
		l.hue, l.sat, l.lit, l.bw, l.bh, l.eye = m.Hue, m.Sat, m.Lit, m.BW, m.BH, m.Eye
		l.iris = m.Iris
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

// boughtPads leaves out the pad each burrow came with, so digging never
// makes the next pad dearer.
func boughtPads(q querier, p *pond) (int, error) {
	var dug int
	err := q.QueryRow(`SELECT burrow_level FROM ponds WHERE id = ?`, p.ID).Scan(&dug)
	return p.Pads - dug, err
}

func clamp(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }

// A fed frog lays a better egg. The meal's tier sets how often a dye, pattern
// or drops take, and a rare meal or better can give the egg odd eyes alone.
var (
	mealTakes = [tierCount]float64{0.74, 0.80, 0.86, 0.92, 1}
	mealEyes  = [tierCount]float64{0, 0, 0.05, 0.12, 0.25}
)

// breed lays an egg from a parent with a new seed. dye, stamp and drop are ""
// or one of dyeMorph's, stamps' or drops' keys.
func breed(parent look, seed uint32, dye, stamp, drop string) mods {
	return breedFed(parent, seed, dye, stamp, drop, -1)
}

// breedFed is breed from a parent that ate a meal of the given tier, -1 for
// none.
func breedFed(parent look, seed uint32, dye, stamp, drop string, meal int) mods {
	takes := dyeTakes
	if meal >= 0 {
		takes = mealTakes[meal]
	}
	child := lookOf(seed)
	nudge := func(by float64) float64 { return (rand.Float64()*2 - 1) * by }
	m := mods{
		Morph: parent.morph.Name, Pattern: parent.pattern, Sat: parent.sat,
		Hue: parent.hue + nudge(7), Lit: parent.lit + nudge(3),
		BW: clamp(parent.bw+nudge(0.8), 10, 15.5), BH: clamp(parent.bh+nudge(0.6), 6.6, 10.4),
		Eye: clamp(parent.eye+nudge(0.3), 2.3, 4.4), Iris: parent.iris,
	}
	if rand.Float64() < 0.2 {
		m.Pattern = child.pattern
	}
	if rand.Float64() < throwback {
		m.Morph, m.Hue, m.Sat, m.Lit = child.morph.Name, child.hue, child.sat, child.lit
		m.Iris = ""
		m.Took = append(m.Took, "throwback")
	}
	if name, ok := dyeMorph[dye]; ok && rand.Float64() < takes {
		d := morphNamed(name)
		m.Morph, m.Hue, m.Sat, m.Lit = d.Name, (d.H[0]+d.H[1])/2, (d.S[0]+d.S[1])/2, (d.L[0]+d.L[1])/2
		m.Iris = ""
		m.Took = append(m.Took, "dye")
	}
	if pat, ok := stamps[stamp]; ok && rand.Float64() < takes {
		m.Pattern = pat
		m.Took = append(m.Took, "stamp")
	}
	if iris, ok := drops[drop]; ok && rand.Float64() < takes {
		m.Iris = iris
		m.Took = append(m.Took, "drops")
	} else if meal >= 0 && m.Iris == "" && rand.Float64() < mealEyes[meal] {
		m.Iris = randomDrops()
		m.Took = append(m.Took, "meal")
	}
	// Drops the colour the morph already has are no drops at all.
	if m.Iris == morphEye[m.Morph] {
		m.Iris = ""
	}
	// Colour drifts over generations but stays near what its morph looks like.
	d := morphNamed(m.Morph)
	m.Hue = clamp(m.Hue, d.H[0]-15, d.H[1]+15)
	m.Lit = clamp(m.Lit, d.L[0]-6, d.L[1]+6)
	return m
}

// ---------------------------------------------------------------- the odds

type outcomeView struct {
	Morph   string  `json:"morph"`
	Pattern string  `json:"pattern"`
	Chance  float64 `json:"chance"`
	// New is a colour or a back the pond's register doesn't have yet.
	New bool `json:"new"`
}

type oddsView struct {
	Takes float64 `json:"takes"`
	// Meal is the dish the frog is full of, "" when it isn't.
	Meal     string        `json:"meal"`
	Outcomes []outcomeView `json:"outcomes"`
	// Eyes is the chance of odd eyes, and Drops what it would be with drops in.
	Eyes  float64 `json:"eyes"`
	Drops float64 `json:"drops"`
}

// layOdds works out what breedFed can hatch and how often, by walking every
// roll it makes. The nudges to hue and size never change a morph or a back,
// so they're left out. seen is the register's colours and backs.
func layOdds(parent look, dye, stamp, drop string, meal int, seen map[string]bool) oddsView {
	takes := dyeTakes
	if meal >= 0 {
		takes = mealTakes[meal]
	}
	eyeChance := 0.0
	if meal >= 0 {
		eyeChance = mealEyes[meal]
	}
	dyed, hasDye := dyeMorph[dye]
	pat, hasStamp := stamps[stamp]
	iris, hasDrops := drops[drop]
	chance := map[[2]string]float64{}
	var eyes, dropEyes float64
	// odd is the chance an egg with this morph and this iris so far ends up
	// with odd eyes, with drops put in or not.
	odd := func(withDrops bool, ir, mo string) float64 {
		is := func(c string) float64 {
			if c != "" && c != morphEye[mo] {
				return 1
			}
			return 0
		}
		rest := is(ir)
		if meal >= 0 && ir == "" {
			rest = eyeChance * (is(drops["drops_red"]) + is(drops["drops_pale"])) / 2
		}
		if !withDrops {
			return rest
		}
		col := iris
		if !hasDrops {
			col = drops["drops_red"]
		}
		return takes*is(col) + (1-takes)*rest
	}
	for _, c := range morphs {
		pc := float64(c.W) / 1000
		backs := map[string]float64{}
		for _, b := range patterns {
			backs[b] += 1 / float64(len(patterns))
		}
		if c.Mask {
			for b := range backs {
				backs[b] *= 0.4
			}
			backs["MASK"] += 0.6
		}
		for _, thrown := range []bool{false, true} {
			pt := throwback
			if !thrown {
				pt = 1 - throwback
			}
			for _, took := range []bool{false, true} {
				pd := 0.0
				if hasDye {
					pd = takes
				}
				if !took {
					pd = 1 - pd
				}
				if pd == 0 {
					continue
				}
				mo := parent.morph.Name
				if thrown {
					mo = c.Name
				}
				if took {
					mo = dyed
				}
				p := pc * pt * pd
				ir := parent.iris
				if thrown || took {
					ir = ""
				}
				eyes += p * odd(hasDrops, ir, mo)
				dropEyes += p * odd(true, ir, mo)
				for back, pb := range map[string]float64{"": 0.8, "child": 0.2} {
					from := map[string]float64{parent.pattern: 1}
					if back == "child" {
						from = backs
					}
					for b, q := range from {
						ps := 0.0
						if hasStamp {
							ps = takes
						}
						chance[[2]string{mo, b}] += p * pb * q * (1 - ps)
						if hasStamp {
							chance[[2]string{mo, pat}] += p * pb * q * ps
						}
					}
				}
			}
		}
	}
	out := oddsView{Takes: takes, Eyes: eyes, Drops: dropEyes}
	for k, p := range chance {
		out.Outcomes = append(out.Outcomes, outcomeView{Morph: k[0], Pattern: k[1], Chance: p, New: !seen[k[0]] || !seen[k[1]]})
	}
	slices.SortFunc(out.Outcomes, func(a, b outcomeView) int {
		if a.Chance != b.Chance {
			return cmp.Compare(b.Chance, a.Chance)
		}
		return cmp.Compare(a.Morph+a.Pattern, b.Morph+b.Pattern)
	})
	return out
}

// layOddsOf is layOdds for one of a pond's frogs with what it has eaten.
func (s *store) layOddsOf(id, frogID int64, dye, stamp, drop string) (*oddsView, error) {
	if _, ok := dyeMorph[dye]; dye != "" && !ok {
		return nil, errNotHeld
	}
	if _, ok := stamps[stamp]; stamp != "" && !ok {
		return nil, errNotHeld
	}
	if _, ok := drops[drop]; drop != "" && !ok {
		return nil, errNotHeld
	}
	frogs, err := loadFrogs(s.r, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	seen := map[string]bool{}
	var parent *frogRow
	for i, f := range frogs {
		if f.ID == frogID {
			parent = &frogs[i]
		}
		if stage, _ := stageOf(now.Sub(time.Unix(f.HatchedAt, 0))); stage != "egg" {
			g := genomeOf(f.Seed, f.Mods)
			seen[g.Morph], seen[g.Pattern] = true, true
		}
	}
	if parent == nil {
		return nil, errNotHeld
	}
	meal, name := -1, ""
	if d, ok := dishOfKind(parent.Fed); ok && fullUntil(*parent) > now.Unix() {
		meal, name = d.tier, d.name
	}
	o := layOdds(lookFor(parent.Seed, parent.Mods), dye, stamp, drop, meal, seen)
	o.Meal = name
	return &o, nil
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
	Name      string
	// Fed is the kind of the dish it last ate and FedAt when, and it stays
	// full for a while after.
	Fed    string
	FedAt  int64
	Job    string
	Target string
	// When its unpaid actions began, and how many it had been paid for before.
	WorkAt int64
	WorkN  int
	// The tool tier its basket is worked with, set when the basket starts.
	WorkTool int
	// Its favourite dish and hat by kind, empty until it's a frog.
	FavDish, FavHat string
}

func loadFrogs(q querier, pondID int64) ([]frogRow, error) {
	rows, err := q.Query(`SELECT id, seed, genome_json, hatched_at, laid_at, out_front, hat_shape, hat_colour, name, fed, fed_at,
		job, target, work_at, work_n, work_tool, fav_dish, fav_hat FROM frogs WHERE pond_id = ? ORDER BY id`, pondID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []frogRow
	for rows.Next() {
		var f frogRow
		var raw string
		if err := rows.Scan(&f.ID, &f.Seed, &raw, &f.HatchedAt, &f.LaidAt, &f.Front, &f.Hat.Shape, &f.Hat.Colour, &f.Name, &f.Fed, &f.FedAt,
			&f.Job, &f.Target, &f.WorkAt, &f.WorkN, &f.WorkTool, &f.FavDish, &f.FavHat); err != nil {
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

func (s *store) lay(id, frogID int64, dye, stamp, drop string) (*laid, error) {
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
		if _, ok := dyeMorph[dye]; dye != "" && !ok {
			return errNotHeld
		}
		if _, ok := stamps[stamp]; stamp != "" && !ok {
			return errNotHeld
		}
		if _, ok := drops[drop]; drop != "" && !ok {
			return errNotHeld
		}
		for _, k := range []string{dye, stamp, drop} {
			if k == "" {
				continue
			}
			if err := takeItem(tx, p.ID, k, 1); err != nil {
				return err
			}
		}
		seed := randomSeed()
		meal := -1
		if d, ok := dishOfKind(parent.Fed); ok && fullUntil(*parent) > now.Unix() {
			meal = d.tier
		}
		m := breedFed(lookFor(parent.Seed, parent.Mods), seed, dye, stamp, drop, meal)
		raw, _ := json.Marshal(m)
		if _, err := tx.Exec(`UPDATE frogs SET laid_at = ?, fed = '' WHERE id = ?`, now.Unix(), parent.ID); err != nil {
			return err
		}
		g := genomeOf(seed, &m)
		if _, err := tx.Exec(`INSERT INTO frogs (pond_id, seed, genome_json, name, hatched_at, out_front)
			VALUES (?, ?, ?, ?, ?, 0)`, p.ID, seed, string(raw), g.Name, now.Unix()); err != nil {
			return err
		}
		out.Frog = frogViewOf(frogRow{Seed: seed, Mods: &m, HatchedAt: now.Unix()}, now)
		return addFlow(tx, p, "fed", 1)
	})
	out.Pond = v
	return out, err
}

// feed gives a frog a dish from the bag. A frog that's still full won't take
// another, and laying uses the meal up.
func (s *store) feed(id, frogID int64, kind string) (*view, error) {
	if _, ok := dishOfKind(kind); !ok {
		return nil, errNotHeld
	}
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		var f frogRow
		err := tx.QueryRow(`SELECT fed, fed_at, hatched_at, fav_dish FROM frogs WHERE pond_id = ? AND id = ?`, p.ID, frogID).
			Scan(&f.Fed, &f.FedAt, &f.HatchedAt, &f.FavDish)
		if err != nil {
			return errNotHeld
		}
		if !canWork(f, now) {
			return errNotYet
		}
		if fullUntil(f) > now.Unix() {
			return errFed
		}
		if err := takeItem(tx, p.ID, kind, 1); err != nil {
			return err
		}
		// What it did before this meal is paid as it stands.
		if _, err := s.settle(tx, p, frogID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE frogs SET fed = ?, fed_at = ? WHERE id = ?`, kind, now.Unix(), frogID); err != nil {
			return err
		}
		if err := addFlow(tx, p, "fed", 1); err != nil {
			return err
		}
		return noteTaste(tx, frogID, kind)
	})
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
		return tx.QueryRow(`SELECT id, seed, genome_json, hatched_at, hat_shape, hat_colour, name FROM frogs WHERE id = ?`, frogID).
			Scan(&p.FrogID, &p.Seed, &p.ModsJSON, &p.HatchedAt, &p.Wearing.Shape, &p.Wearing.Colour, &p.FrogName)
	})
}

func (s *store) buyPad(id int64) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		bought, err := boughtPads(tx, p)
		if err != nil {
			return err
		}
		if bought >= mostPads {
			return errNotYet
		}
		cost := padCost(bought)
		if p.Pebbles < cost {
			return errPoor
		}
		p.Pebbles -= cost
		p.Pads++
		return nil
	})
}
