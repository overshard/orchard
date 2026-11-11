package main

import (
	"database/sql"
	"errors"
	"time"
)

// A tool is bought once for the whole pond and kept for good. It makes every
// action at its job quicker for a frog good enough to use it, and a frog
// picks a new one up at the start of its next basket.
type tool struct {
	Job   string `json:"job"`
	Tier  int    `json:"tier"`
	Name  string `json:"name"`
	Level int    `json:"level"`
	Price int    `json:"price"`
	Shop  string `json:"shop"`
	// What it leaves of an action's time.
	Cut float64 `json:"cut"`
}

const toolTiers = 3

// What each tier leaves of an action's time, the level a frog needs to use
// it and the price. Tier 0 is working with nothing.
var (
	toolCut    = [toolTiers + 1]float64{1, 0.9, 0.8, 0.75}
	toolLevel  = [toolTiers + 1]int{0, 10, 35, 62}
	toolPrices = [toolTiers + 1]int{0, 900, 2500, 6000}
)

// Moth has the odd glass and gauze, Big Bean the garden things and Tansy
// everything with an edge or a handle.
var toolSets = []struct {
	job, shop string
	names     [toolTiers]string
}{
	{"garden", shopBrindle, [toolTiers]string{"Dibber", "Watering can", "Brass watering can"}},
	{"bugs", shopMoss, [toolTiers]string{"Twig net", "Reed net", "Fine net"}},
	{"sticks", shopFenn, [toolTiers]string{"Bramble hook", "Hand saw", "Axe"}},
	{"reeds", shopFenn, [toolTiers]string{"Reed knife", "Sickle", "Scythe"}},
	{"mud", shopFenn, [toolTiers]string{"Trowel", "Spade", "Long spade"}},
	{"pebbles", shopMoss, [toolTiers]string{"Goggles", "Flippers", "Diving helmet"}},
	{jobCraft, shopFenn, [toolTiers]string{"Awl", "Mallet", "Carving set"}},
}

func toolOf(job string, tier int) (tool, bool) {
	if tier < 1 || tier > toolTiers {
		return tool{}, false
	}
	for _, s := range toolSets {
		if s.job == job {
			return tool{job, tier, s.names[tier-1], toolLevel[tier], toolPrices[tier], s.shop, toolCut[tier]}, true
		}
	}
	return tool{}, false
}

func toolsFor(job string) []tool {
	out := []tool{}
	for tier := 1; tier <= toolTiers; tier++ {
		if t, ok := toolOf(job, tier); ok {
			out = append(out, t)
		}
	}
	return out
}

// toolFor is the best tier a frog at this level can use of what the pond owns.
func toolFor(owned, level int) int {
	t := owned
	for t > 0 && level < toolLevel[t] {
		t--
	}
	return t
}

func loadTools(q querier, pondID int64) (map[string]int, error) {
	rows, err := q.Query(`SELECT job, tier FROM tools WHERE pond_id = ?`, pondID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var j string
		var t int
		if err := rows.Scan(&j, &t); err != nil {
			return nil, err
		}
		out[j] = t
	}
	return out, rows.Err()
}

var errToolLevel = errors.New("no frog here is good enough at it to use that yet")

type toolBought struct {
	Tool tool  `json:"tool"`
	Pond *view `json:"pond"`
}

// buyTool sells the pond the next tier of a job's tool. It needs a frog at
// that tier's level and never touches a basket already on the go.
func (s *store) buyTool(id int64, job string) (*toolBought, error) {
	out := &toolBought{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		owned, err := loadTools(tx, p.ID)
		if err != nil {
			return err
		}
		t, ok := toolOf(job, owned[job]+1)
		if !ok {
			if _, known := toolOf(job, 1); known {
				return errSoldOut
			}
			return errNotHeld
		}
		xp, err := loadXP(tx, p.ID)
		if err != nil {
			return err
		}
		best := 1
		for _, m := range xp {
			best = max(best, levelOf(m[job]))
		}
		if best < t.Level {
			return errToolLevel
		}
		if p.Pebbles < t.Price {
			return errPoor
		}
		p.Pebbles -= t.Price
		out.Tool = t
		_, err = tx.Exec(`INSERT INTO tools (pond_id, job, tier) VALUES (?, ?, ?)
			ON CONFLICT (pond_id, job) DO UPDATE SET tier = excluded.tier`, p.ID, job, t.Tier)
		return err
	})
	out.Pond = v
	return out, err
}
