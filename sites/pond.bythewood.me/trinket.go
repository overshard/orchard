package main

import (
	"database/sql"
	"math/rand/v2"
	"strings"
	"time"
)

// Trinkets are the second collection, silly things to set round the pad.
// They're held as items with a t_ prefix, and trinkets_found remembers every
// one a pond has had, so cooking the last of one never loses it from the set.
type trinket struct {
	kind, name string
	tier       int
}

var trinkets = []trinket{
	{"t_acorn", "Acorn", 0},
	{"t_pinecone", "Pine cone", 0},
	{"t_marble", "Marble", 0},
	{"t_button", "Button mushroom", 0},
	{"t_boat", "Paper boat", 0},
	{"t_duck", "Rubber duck", 1},
	{"t_cone", "Traffic cone", 1},
	{"t_chanterelle", "Chanterelle", 1},
	{"t_clover", "Four leaf clover", 1},
	{"t_earring", "Lost earring", 1},
	{"t_globe", "Snow globe", 2},
	{"t_gnome", "Garden gnome", 2},
	{"t_agaric", "Fly agaric", 2},
	{"t_robot", "Tin robot", 2},
	{"t_disco", "Disco ball", 3},
	{"t_jar", "Jar of fireflies", 3},
	{"t_goldmorel", "Golden morel", 4},
	{"t_pearl", "Pearl", 4},
}

const trinketBoxCost = 60

// The spots round the pad a trinket can sit in, in the order the page and
// the trinkets_out column keep them.
var trinketSpots = []string{"left", "right", "water"}

func init() {
	for _, t := range trinkets {
		itemNames[t.kind] = t.name
	}
}

func trinketOf(kind string) (trinket, bool) {
	for _, t := range trinkets {
		if t.kind == kind {
			return t, true
		}
	}
	return trinket{}, false
}

func trinketOfTier(t int) trinket {
	var pool []trinket
	for _, x := range trinkets {
		if x.tier == t {
			pool = append(pool, x)
		}
	}
	return pool[rand.IntN(len(pool))]
}

// A trinket box has the hat box's odds.
func rollTrinket() trinket {
	r, acc := rand.IntN(100), 0
	for t, w := range boxOdds {
		acc += w
		if r < acc {
			return trinketOfTier(t)
		}
	}
	return trinketOfTier(0)
}

func addTrinket(tx *sql.Tx, id int64, kind string) (int, error) {
	if _, err := tx.Exec(`INSERT INTO trinkets_found (pond_id, kind) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, kind); err != nil {
		return 0, err
	}
	var n int
	err := tx.QueryRow(`INSERT INTO items (pond_id, kind, count) VALUES (?, ?, 1)
		ON CONFLICT (pond_id, kind) DO UPDATE SET count = count + 1 RETURNING count`, id, kind).Scan(&n)
	return n, err
}

func (s *store) trinketBox(id int64) (*opened, error) {
	out := &opened{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if p.Pebbles < trinketBoxCost {
			return errPoor
		}
		p.Pebbles -= trinketBoxCost
		t := rollTrinket()
		out.Prize = prize{Item: t.kind, Text: t.name}
		return award(tx, p, &out.Prize)
	})
	out.Pond = v
	return out, err
}

func parseSpots(raw string) []string {
	out := strings.Split(raw, ",")
	for len(out) < len(trinketSpots) {
		out = append(out, "")
	}
	return out[:len(trinketSpots)]
}

// place sets a trinket in a spot, or clears it with "". A trinket sits in one
// spot at a time, so placing it again moves it.
func (s *store) place(id int64, spot int, kind string) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if spot < 0 || spot >= len(trinketSpots) {
			return errNotHeld
		}
		spots := parseSpots(p.TrinketsOut)
		if kind != "" {
			if _, ok := trinketOf(kind); !ok {
				return errNotHeld
			}
			var n int
			if err := tx.QueryRow(`SELECT COALESCE(SUM(count), 0) FROM items WHERE pond_id = ? AND kind = ?`, p.ID, kind).Scan(&n); err != nil {
				return err
			}
			if n < 1 {
				return errNotHeld
			}
			for i := range spots {
				if spots[i] == kind {
					spots[i] = ""
				}
			}
		}
		spots[spot] = kind
		p.TrinketsOut = strings.Join(spots, ",")
		return nil
	})
}

// sellTrinket is Old Moss buying a spare at a quarter of the going rate.
func (s *store) sellTrinket(id int64, kind string) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		t, ok := trinketOf(kind)
		if !ok {
			return errNotHeld
		}
		res, err := tx.Exec(`UPDATE items SET count = count - 1 WHERE pond_id = ? AND kind = ? AND count > 1`, p.ID, kind)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotHeld
		}
		p.Pebbles += hatRate[t.tier] / 4
		return nil
	})
}

type trinketView struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Tier  int    `json:"tier"`
	Count int    `json:"count"`
	Found bool   `json:"found"`
	Cook  bool   `json:"cook"`
	Moss  int    `json:"moss"`
}

type trinketsView struct {
	All      []trinketView `json:"all"`
	Spots    []string      `json:"spots"`
	BoxCost  int           `json:"box_cost"`
	SpotName []string      `json:"spot_names"`
}

func trinketsOf(q querier, p *pond, held map[string]int) (trinketsView, error) {
	tv := trinketsView{BoxCost: trinketBoxCost, SpotName: trinketSpots}
	found := map[string]bool{}
	rows, err := q.Query(`SELECT kind FROM trinkets_found WHERE pond_id = ?`, p.ID)
	if err != nil {
		return tv, err
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return tv, err
		}
		found[k] = true
	}
	if err := rows.Close(); err != nil {
		return tv, err
	}
	for _, t := range trinkets {
		_, cook := ingredientOf(t.kind)
		tv.All = append(tv.All, trinketView{t.kind, t.name, t.tier, held[t.kind], found[t.kind], cook, hatRate[t.tier] / 4})
	}
	// A trinket cooked or sold down to none leaves its spot empty.
	tv.Spots = parseSpots(p.TrinketsOut)
	for i, k := range tv.Spots {
		if held[k] < 1 {
			tv.Spots[i] = ""
		}
	}
	return tv, nil
}
