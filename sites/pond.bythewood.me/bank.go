package main

import (
	"database/sql"
	"errors"
	"log/slog"
	"math"
	"sort"
	"time"
)

// Each build has five levels and each level costs about double the last.
var (
	gravelCost = []int{100, 250, 600, 1400, 3000}
	gravelRate = []int{6, 12, 18, 24, 30}
	gardenCost = []int{150, 350, 800, 1600, 3200}
	gardenPlot = []int{2, 3, 4, 5, 6}
)

const (
	buildLevels = 5
	// A collector holds this long of whatever it makes, at every level.
	holdFor = 8 * time.Hour
	// Seconds of growing at the plain rate before a plot is ready.
	growFor  = 3 * 3600.0
	waterAdd = 1.5 * 3600
	frostAt  = 33
)

var errMaxed = errors.New("that's built as far as it goes")

var errShort = errors.New("not enough sticks, reeds or clay for that")

// What each level past the first takes in materials as well as pebbles, so
// the first level of anything is open before a frog can work.
var buildMats = map[string][buildLevels]map[string]int{
	"gravel": {nil, {"clay": 10}, {"clay": 40}, {"clay": 100, "sticks": 40}, {"clay": 250, "sticks": 100}},
	"garden": {nil, {"sticks": 20}, {"sticks": 60}, {"sticks": 150, "reeds": 40}, {"sticks": 350, "reeds": 100}},
	"dye":    {nil, {"reeds": 20}, {"reeds": 60}, {"reeds": 150, "clay": 40}, {"reeds": 350, "clay": 100}},
}

func matsFor(what string, level int) map[string]int {
	if level >= buildLevels {
		return nil
	}
	return buildMats[what][level]
}

// payMats takes a level's materials, or none of them if any are short.
func payMats(tx *sql.Tx, id int64, what string, level int) error {
	need := matsFor(what, level)
	held, err := heldItems(tx, id)
	if err != nil {
		return err
	}
	for k, n := range need {
		if held[k] < n {
			return errShort
		}
	}
	for k, n := range need {
		if err := takeItem(tx, id, k, n); err != nil {
			return err
		}
	}
	return nil
}

// gravelHeld is what the bar has made since it was last emptied, and when it
// was last emptied once whole pebbles are taken out.
func gravelHeld(level int, at int64, now time.Time) (int, int64) {
	if level < 1 {
		return 0, at
	}
	rate := gravelRate[level-1]
	per := int64(3600 / rate)
	n := int((now.Unix() - at) / per)
	limit := rate * int(holdFor/time.Hour)
	if n >= limit {
		return limit, now.Unix()
	}
	return max(n, 0), at + int64(n)*per
}

// ---------------------------------------------------------------- growing

// Growing runs on one clock for every garden, wound by the real day: rain
// doubles it and frost stops it. A plot is sown at a reading of the clock and
// is ready once the clock has moved growFor past that.
func growRate(r *reading, now time.Time) (float64, string) {
	if r == nil {
		return 1, ""
	}
	v := dayOfReading(r, now)
	switch {
	case r.HasTemp && r.Temp < frostAt:
		return 0, "frost"
	case v.Rain:
		return 2, "rain"
	}
	return 1, ""
}

// growth reads the clock now without writing it.
func (s *store) growth(q querier, now time.Time) (float64, error) {
	var at int64
	var total float64
	err := q.QueryRow(`SELECT at, total FROM growth WHERE id = 1`).Scan(&at, &total)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	rate, _ := growRate(s.day.reading(now), now)
	return total + max(0, float64(now.Unix()-at))*rate, nil
}

// windGrowth writes the clock at the current rate, so a change of weather
// only counts from the next wind on.
func (s *store) windGrowth() error {
	now := s.now()
	return s.write(func(tx *sql.Tx) error {
		total, err := s.growth(tx, now)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO growth (id, at, total) VALUES (1, ?, ?)
			ON CONFLICT (id) DO UPDATE SET at = excluded.at, total = excluded.total`, now.Unix(), total)
		return err
	})
}

func windEvery(db *store) {
	for {
		if err := db.windGrowth(); err != nil {
			slog.Error("growth", slog.Any("err", err))
		}
		if err := db.lockMarket(); err != nil {
			slog.Error("market", slog.Any("err", err))
		}
		time.Sleep(dayEvery)
	}
}

type plot struct {
	Slot    int
	Sown    float64
	Watered bool
}

func loadPlots(q querier, id int64) ([]plot, error) {
	rows, err := q.Query(`SELECT slot, sown, watered FROM plots WHERE pond_id = ? ORDER BY slot`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []plot
	for rows.Next() {
		var p plot
		if err := rows.Scan(&p.Slot, &p.Sown, &p.Watered); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (p plot) grown(clock, speed float64) float64 {
	g := clock - p.Sown
	if p.Watered {
		g += waterAdd
	}
	return min(1, max(0, g*speed/growFor))
}

// gardenSpeed is how much faster the plots grow with a frog tending them, by
// the best gardener at work, up to twice as fast at 99.
func gardenSpeed(q querier, id int64, now time.Time) (float64, string, error) {
	frogs, err := loadFrogs(q, id)
	if err != nil {
		return 1, "", err
	}
	xp, err := loadXP(q, id)
	if err != nil {
		return 1, "", err
	}
	speed, who := 1.0, ""
	for _, f := range frogs {
		if f.Job != "garden" || !canWork(f, now) {
			continue
		}
		if sp := speedAt(levelOf(xp[f.ID]["garden"])); sp > speed || who == "" {
			speed, who = sp, titled(frogViewOf(f, now).Name)
		}
	}
	return speed, who, nil
}

// ---------------------------------------------------------------- moves

func (s *store) build(id int64, what string) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		switch what {
		case "gravel":
			if p.GravelLevel >= buildLevels {
				return errMaxed
			}
			cost := gravelCost[p.GravelLevel]
			if p.Pebbles < cost {
				return errPoor
			}
			if err := payMats(tx, p.ID, "gravel", p.GravelLevel); err != nil {
				return err
			}
			// What the bar holds is paid out first, so a new level never eats it.
			if p.GravelLevel > 0 {
				n, at := gravelHeld(p.GravelLevel, p.GravelAt, now)
				p.Pebbles += n
				p.GravelAt = at
			} else {
				p.GravelAt = now.Unix()
			}
			p.Pebbles -= cost
			p.GravelLevel++
		case "garden":
			if p.GardenLevel >= buildLevels {
				return errMaxed
			}
			cost := gardenCost[p.GardenLevel]
			if p.Pebbles < cost {
				return errPoor
			}
			if err := payMats(tx, p.ID, "garden", p.GardenLevel); err != nil {
				return err
			}
			clock, err := s.growth(tx, now)
			if err != nil {
				return err
			}
			p.Pebbles -= cost
			p.GardenLevel++
			for slot := gardenPlotsAt(p.GardenLevel - 1); slot < gardenPlotsAt(p.GardenLevel); slot++ {
				if _, err := tx.Exec(`INSERT INTO plots (pond_id, slot, sown) VALUES (?, ?, ?)`, p.ID, slot, clock); err != nil {
					return err
				}
			}
		case "dye":
			if p.DyeLevel >= buildLevels {
				return errMaxed
			}
			cost := dyeCost[p.DyeLevel]
			if p.Pebbles < cost {
				return errPoor
			}
			if err := payMats(tx, p.ID, "dye", p.DyeLevel); err != nil {
				return err
			}
			p.Pebbles -= cost
			p.DyeLevel++
		case "burrow":
			return s.buildBurrow(tx, p, now)
		default:
			return errNotHeld
		}
		return nil
	})
}

func gardenPlotsAt(level int) int {
	if level < 1 {
		return 0
	}
	return gardenPlot[level-1]
}

type collected struct {
	Pebbles int   `json:"pebbles"`
	Pond    *view `json:"pond"`
}

func (s *store) collect(id int64) (*collected, error) {
	out := &collected{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if p.GravelLevel < 1 {
			return errNotHeld
		}
		n, at := gravelHeld(p.GravelLevel, p.GravelAt, now)
		if n < 1 {
			return errNotYet
		}
		p.Pebbles += n
		p.GravelAt = at
		out.Pebbles = n
		return nil
	})
	out.Pond = v
	return out, err
}

type picked struct {
	Got  []string `json:"got"`
	Pond *view    `json:"pond"`
}

// pick takes whatever is ready and sows the plot again. What came up is
// decided now, by the season it was picked in.
func (s *store) pick(id int64) (*picked, error) {
	out := &picked{Got: []string{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		clock, err := s.growth(tx, now)
		if err != nil {
			return err
		}
		plots, err := loadPlots(tx, p.ID)
		if err != nil {
			return err
		}
		speed, _, err := gardenSpeed(tx, p.ID, now)
		if err != nil {
			return err
		}
		rain := dayOfReading(s.day.reading(now), now).Rain
		for _, pl := range plots {
			if pl.grown(clock, speed) < 1 {
				continue
			}
			g := ingredients[rollCrop(now, rain)]
			if err := addItem(tx, p.ID, g.kind, 1); err != nil {
				return err
			}
			out.Got = append(out.Got, g.kind)
			if _, err := tx.Exec(`UPDATE plots SET sown = ?, watered = 0 WHERE pond_id = ? AND slot = ?`,
				clock, p.ID, pl.Slot); err != nil {
				return err
			}
		}
		if len(out.Got) == 0 {
			return errNotYet
		}
		return nil
	})
	out.Pond = v
	return out, err
}

// water gives every growing plot a head start once a sowing. Rain does it for
// you, so there's nothing to water then.
func (s *store) water(id int64) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if rate, _ := growRate(s.day.reading(now), now); rate != 1 {
			return errNotYet
		}
		clock, err := s.growth(tx, now)
		if err != nil {
			return err
		}
		speed, _, err := gardenSpeed(tx, p.ID, now)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE plots SET watered = 1 WHERE pond_id = ? AND watered = 0 AND ? - sown < ?`,
			p.ID, clock, growFor/speed)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotYet
		}
		return nil
	})
}

type cooked struct {
	Dish itemView `json:"dish"`
	Tier int      `json:"tier"`
	New  bool     `json:"new"`
	Pond *view    `json:"pond"`
}

func takeItem(tx *sql.Tx, id int64, kind string, n int) error {
	res, err := tx.Exec(`UPDATE items SET count = count - ? WHERE pond_id = ? AND kind = ? AND count >= ?`,
		n, id, kind, n)
	if err != nil {
		return err
	}
	if got, _ := res.RowsAffected(); got == 0 {
		return errNotHeld
	}
	return nil
}

func (s *store) cook(id int64, a, b string) (*cooked, error) {
	out := &cooked{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		ia, ok1 := ingredientOf(a)
		ib, ok2 := ingredientOf(b)
		if !ok1 || !ok2 {
			return errNotHeld
		}
		if ia == ib {
			if err := takeItem(tx, p.ID, a, 2); err != nil {
				return err
			}
		} else {
			if err := takeItem(tx, p.ID, a, 1); err != nil {
				return err
			}
			if err := takeItem(tx, p.ID, b, 1); err != nil {
				return err
			}
		}
		d := dishOf(ia, ib)
		if err := addItem(tx, p.ID, d.kind(), 1); err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO cookbook (pond_id, dish, first_at) VALUES (?, ?, ?)
			ON CONFLICT DO NOTHING`, p.ID, d.kind(), now.Unix())
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		out.New = n > 0
		out.Dish = itemView{Kind: d.kind(), Name: d.name, Count: 1}
		out.Tier = d.tier
		return nil
	})
	out.Pond = v
	return out, err
}

// sellDish is Big Bean buying a dish at its worth.
func (s *store) sellDish(id int64, kind string) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		d, ok := dishOfKind(kind)
		if !ok {
			return errNotHeld
		}
		if err := takeItem(tx, p.ID, kind, 1); err != nil {
			return err
		}
		p.Pebbles += dishWorth[d.tier]
		return nil
	})
}

// ---------------------------------------------------------------- the view

type bankView struct {
	Gravel struct {
		Level int `json:"level"`
		Rate  int `json:"rate"`
		Held  int `json:"held"`
		Holds int `json:"holds"`
		// Seconds until the next pebble, 0 while it's full.
		NextIn int64          `json:"next_in"`
		Cost   int            `json:"cost"`
		Mats   map[string]int `json:"mats,omitempty"`
	} `json:"gravel"`
	Garden struct {
		Level int            `json:"level"`
		Cost  int            `json:"cost"`
		Mats  map[string]int `json:"mats,omitempty"`
		Rate  float64        `json:"rate"`
		Why   string         `json:"why"`
		Plots []plotView     `json:"plots"`
		// How much faster a gardening frog makes them grow, and its name.
		Speed    float64 `json:"speed"`
		Gardener string  `json:"gardener,omitempty"`
	} `json:"garden"`
	Dye     dyeView    `json:"dye"`
	Burrows burrowView `json:"burrows"`
}

type plotView struct {
	Grown   float64 `json:"grown"`
	Watered bool    `json:"watered"`
	// Seconds until it's ready at today's rate, -1 while nothing grows.
	ReadyIn int64 `json:"ready_in"`
}

type kitchenView struct {
	Ingredients []ingredientView `json:"ingredients"`
	Dishes      []dishView       `json:"dishes"`
	Cookbook    []dishView       `json:"cookbook"`
	Of          int              `json:"of"`
	// The tier of every dish there is, so the collection can show what's
	// still to find without naming it.
	Tiers []int `json:"tiers"`
}

var dishTiers = func() []int {
	var out []int
	for a := range ingredients {
		for b := a; b < len(ingredients); b++ {
			out = append(out, dishOf(a, b).tier)
		}
	}
	sort.Ints(out)
	return out
}()

type ingredientView struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Tier  int    `json:"tier"`
	Count int    `json:"count"`
	// In season means it can come up in a plot this month.
	Season  bool `json:"season"`
	Trinket bool `json:"trinket,omitempty"`
}

type dishView struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Tier  int    `json:"tier"`
	Count int    `json:"count,omitempty"`
	Worth int    `json:"worth"`
	A     string `json:"a"`
	B     string `json:"b"`
}

func dishViewOf(d dish, count int) dishView {
	return dishView{d.kind(), d.name, d.tier, count, dishWorth[d.tier], ingredients[d.a].kind, ingredients[d.b].kind}
}

func (s *store) bankOf(q querier, p *pond, now time.Time, held map[string]int) (bankView, kitchenView, error) {
	var b bankView
	var k kitchenView
	g := &b.Gravel
	g.Level = p.GravelLevel
	if g.Level > 0 {
		g.Rate = gravelRate[g.Level-1]
		g.Holds = g.Rate * int(holdFor/time.Hour)
		var at int64
		g.Held, at = gravelHeld(g.Level, p.GravelAt, now)
		if g.Held < g.Holds {
			g.NextIn = at + int64(3600/g.Rate) - now.Unix()
		}
	}
	if g.Level < buildLevels {
		g.Cost = gravelCost[g.Level]
	}
	g.Mats = matsFor("gravel", g.Level)

	gd := &b.Garden
	gd.Level = p.GardenLevel
	if gd.Level < buildLevels {
		gd.Cost = gardenCost[gd.Level]
	}
	gd.Mats = matsFor("garden", gd.Level)
	gd.Rate, gd.Why = growRate(s.day.reading(now), now)
	gd.Plots = []plotView{}
	if gd.Level > 0 {
		clock, err := s.growth(q, now)
		if err != nil {
			return b, k, err
		}
		plots, err := loadPlots(q, p.ID)
		if err != nil {
			return b, k, err
		}
		if gd.Speed, gd.Gardener, err = gardenSpeed(q, p.ID, now); err != nil {
			return b, k, err
		}
		for _, pl := range plots {
			v := plotView{Grown: pl.grown(clock, gd.Speed), Watered: pl.Watered, ReadyIn: -1}
			if v.Grown >= 1 {
				v.ReadyIn = 0
			} else if gd.Rate > 0 {
				v.ReadyIn = int64(math.Ceil((1 - v.Grown) * growFor / gd.Speed / gd.Rate))
			}
			gd.Plots = append(gd.Plots, v)
		}
	}

	b.Dye = dyeViewOf(p, now, held)
	var err error
	if b.Burrows, err = burrowViewOf(q, p.ID, now); err != nil {
		return b, k, err
	}

	rain := dayOfReading(s.day.reading(now), now).Rain
	w := seasonWeights(now, rain)
	for i, in := range ingredients {
		k.Ingredients = append(k.Ingredients, ingredientView{in.kind, in.name, in.tier, held[in.kind], w[i] > 0, in.trinket})
	}
	k.Dishes = []dishView{}
	for a := range ingredients {
		for b := a; b < len(ingredients); b++ {
			d := dishOf(a, b)
			if n := held[d.kind()]; n > 0 {
				k.Dishes = append(k.Dishes, dishViewOf(d, n))
			}
		}
	}
	k.Cookbook = []dishView{}
	k.Of = dishCount
	k.Tiers = dishTiers
	rows, err := q.Query(`SELECT dish FROM cookbook WHERE pond_id = ? ORDER BY first_at`, p.ID)
	if err != nil {
		return b, k, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return b, k, err
		}
		if d, ok := dishOfKind(kind); ok {
			k.Cookbook = append(k.Cookbook, dishViewOf(d, 0))
		}
	}
	return b, k, rows.Err()
}

func (s *store) readyPlots(q querier, id int64, now time.Time) (int, error) {
	clock, err := s.growth(q, now)
	if err != nil {
		return 0, err
	}
	plots, err := loadPlots(q, id)
	if err != nil {
		return 0, err
	}
	speed, _, err := gardenSpeed(q, id, now)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, pl := range plots {
		if pl.grown(clock, speed) >= 1 {
			n++
		}
	}
	return n, nil
}
