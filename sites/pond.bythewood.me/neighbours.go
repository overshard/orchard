package main

import (
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"time"
)

// Neighbours are resident frogs the pond runs, the same in every pond. Each
// has a favourite dish and gets friendlier with every dish it's given, and
// one asks a small favour each day. Ignoring them costs nothing.
type resident struct {
	name string
	seed uint32
	hat  int
	// The two ingredients of its favourite dish.
	fav [2]int
}

var residents = []resident{
	{"Sedge", 40213, 1, [2]int{0, 1}},
	{"Dot", 91877, 7, [2]int{1, 2}},
	{"Little Tully", 5521, 5, [2]int{0, 0}},
	{"Pip", 772301, 11, [2]int{3, 4}},
	{"Bramble", 31337, 12, [2]int{2, 2}},
	{"Puddle", 120587, 6, [2]int{4, 5}},
	{"Big Bean", 66049, 15, [2]int{1, 3}},
	{"Old Clover", 8675309, 4, [2]int{5, 7}},
	{"Rush", 424242, 8, [2]int{4, 6}},
	{"Marsh", 2718281, 9, [2]int{2, 7}},
	{"Tansy", 1618033, 10, [2]int{6, 7}},
	{"Moth", 9001, 2, [2]int{6, 6}},
}

func (r resident) favDish() dish { return dishOf(r.fav[0], r.fav[1]) }

// residentNamed is -1 for a name that isn't a resident's, like the rolled
// names stalls sold to before the residents did the buying.
func residentNamed(name string) int {
	for i, r := range residents {
		if r.name == name {
			return i
		}
	}
	return -1
}

var (
	// Points a dish is worth by tier, three times over for a favourite.
	giftPoints = [tierCount]int{1, 2, 4, 8, 16}
	// Where each friendship level starts.
	friendAt    = []int{0, 3, 10, 25, 50, 100}
	friendNames = []string{"Stranger", "Waves hello", "Friendly", "Good friend", "Close friend", "Best friend"}
	// At level 2 a neighbour tells you how to cook its favourite.
	recipeAt = 2
)

const (
	favourPoints  = 5
	favourPebbles = 20
)

// What a neighbour can ask for, n of a kind.
var asks = []struct {
	kind string
	n    int
}{
	{"sticks", 10}, {"reeds", 10}, {"clay", 8}, {"cricket", 2},
	{"duckweed", 3}, {"watercress", 3}, {"agate", 1}, {"quartz", 1},
}

var errGiven = errors.New("they've had something from you today already")

func friendLevel(points int) int {
	l := 0
	for i, at := range friendAt {
		if points >= at {
			l = i
		}
	}
	return l
}

// friendPresent is what a neighbour gives on reaching a level.
func friendPresent(level int) prize {
	switch level {
	case 1:
		return prize{Pebbles: 30, Text: "30 pebbles, to say hello."}
	case 2:
		return prize{Pebbles: 60, Text: "60 pebbles, and the recipe for its favourite."}
	case 3:
		h := hatOfTier(1)
		return prize{Hat: &h}
	case 4:
		t := trinketOfTier(2)
		return prize{Item: t.kind, Text: t.name + ", for round the pad."}
	}
	h := hatOfTier(3)
	return prize{Hat: &h}
}

func dayHash(day, salt string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(day + salt))
	return h.Sum32()
}

// favourOf is who asks today and what for, the same in every pond.
func favourOf(day string) (int, string, int) {
	a := asks[dayHash(day, "ask")%uint32(len(asks))]
	return int(dayHash(day, "who") % uint32(len(residents))), a.kind, a.n
}

type friendship struct {
	points  int
	giftDay string
}

func loadFriends(q querier, id int64) (map[int]friendship, error) {
	rows, err := q.Query(`SELECT who, points, gift_day FROM neighbours WHERE pond_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]friendship{}
	for rows.Next() {
		var who int
		var f friendship
		if err := rows.Scan(&who, &f.points, &f.giftDay); err != nil {
			return nil, err
		}
		out[who] = f
	}
	return out, rows.Err()
}

// befriend adds points and hands over a present for every level crossed.
func befriend(tx *sql.Tx, p *pond, who, points int, giftDay string) ([]prize, error) {
	var before int
	err := tx.QueryRow(`SELECT points FROM neighbours WHERE pond_id = ? AND who = ?`, p.ID, who).Scan(&before)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO neighbours (pond_id, who, points, gift_day) VALUES (?, ?, ?, ?)
		ON CONFLICT (pond_id, who) DO UPDATE SET points = points + excluded.points,
		gift_day = CASE WHEN excluded.gift_day = '' THEN gift_day ELSE excluded.gift_day END`,
		p.ID, who, points, giftDay); err != nil {
		return nil, err
	}
	var out []prize
	for l := friendLevel(before) + 1; l <= friendLevel(before+points); l++ {
		z := friendPresent(l)
		if err := award(tx, p, &z); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, nil
}

type befriended struct {
	Line    string  `json:"line"`
	Level   int     `json:"level"`
	Up      bool    `json:"up"`
	Prizes  []prize `json:"prizes"`
	Pebbles int     `json:"pebbles,omitempty"`
	Pond    *view   `json:"pond"`
}

// gift hands a neighbour a dish, once a day each.
func (s *store) gift(id int64, who int, kind string) (*befriended, error) {
	out := &befriended{Prizes: []prize{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if who < 0 || who >= len(residents) {
			return errNotHeld
		}
		d, ok := dishOfKind(kind)
		if !ok {
			return errNotHeld
		}
		r := residents[who]
		friends, err := loadFriends(tx, p.ID)
		if err != nil {
			return err
		}
		today := dayOf(now)
		if friends[who].giftDay == today {
			return errGiven
		}
		if err := takeItem(tx, p.ID, kind, 1); err != nil {
			return err
		}
		pts := giftPoints[d.tier]
		if d == r.favDish() {
			pts *= 3
			out.Line = r.name + " loves it. That's its favourite."
		} else {
			out.Line = r.name + " says thank you, and eats it on the spot."
		}
		out.Prizes, err = befriend(tx, p, who, pts, today)
		out.Level = friendLevel(friends[who].points + pts)
		out.Up = len(out.Prizes) > 0
		return err
	})
	out.Pond = v
	return out, err
}

// favour gives today's asker what it asked for.
func (s *store) favour(id int64) (*befriended, error) {
	out := &befriended{Prizes: []prize{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		today := dayOf(now)
		var done string
		if err := tx.QueryRow(`SELECT favour_day FROM ponds WHERE id = ?`, p.ID).Scan(&done); err != nil {
			return err
		}
		if done == today {
			return errNotYet
		}
		who, kind, n := favourOf(today)
		if err := takeItem(tx, p.ID, kind, n); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE ponds SET favour_day = ? WHERE id = ?`, today, p.ID); err != nil {
			return err
		}
		friends, err := loadFriends(tx, p.ID)
		if err != nil {
			return err
		}
		p.Pebbles += favourPebbles
		out.Pebbles = favourPebbles
		out.Line = fmt.Sprintf("%s is very grateful, and gives you %d pebbles.", residents[who].name, favourPebbles)
		out.Prizes, err = befriend(tx, p, who, favourPoints, "")
		out.Level = friendLevel(friends[who].points + favourPoints)
		out.Up = len(out.Prizes) > 0
		return err
	})
	out.Pond = v
	return out, err
}

// ---------------------------------------------------------------- the view

type neighbourView struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Seed   uint32 `json:"seed"`
	Hat    hat    `json:"hat"`
	Level  int    `json:"level"`
	Title  string `json:"title"`
	Points int    `json:"points"`
	From   int    `json:"from"`
	To     int    `json:"to"`
	Fav    string `json:"fav"`
	// Its favourite's two ingredients, once it's friendly enough to say.
	FavA   string  `json:"fav_a,omitempty"`
	FavB   string  `json:"fav_b,omitempty"`
	Gave   bool    `json:"gave"`
	Lodger bool    `json:"lodger"`
	Firm   string  `json:"firm"`
	Talk   string  `json:"talk"`
	Gossip string  `json:"gossip"`
	Bio    string  `json:"bio"`
	Now    folkDay `json:"now"`
}

type favourView struct {
	Who  int    `json:"who"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	What string `json:"what"`
	N    int    `json:"n"`
	Done bool   `json:"done"`
	Pay  int    `json:"pay"`
}

type neighboursView struct {
	All    []neighbourView `json:"all"`
	Favour favourView      `json:"favour"`
}

func neighboursOf(q querier, id int64, now time.Time) (neighboursView, error) {
	var v neighboursView
	friends, err := loadFriends(q, id)
	if err != nil {
		return v, err
	}
	b, err := loadBurrows(q, id)
	if err != nil {
		return v, err
	}
	today := dayOf(now)
	for i, r := range residents {
		f := friends[i]
		l := friendLevel(f.points)
		d := r.favDish()
		nv := neighbourView{ID: i, Name: r.name, Seed: r.seed, Hat: hat{Shape: r.hat}, Level: l, Title: friendNames[l],
			Points: f.points, From: friendAt[l], To: friendAt[min(l+1, len(friendAt)-1)], Fav: d.name, Gave: f.giftDay == today,
			Lodger: b.lodging(i)}
		if l >= recipeAt {
			nv.FavA, nv.FavB = ingredients[d.a].kind, ingredients[d.b].kind
		}
		v.All = append(v.All, nv)
	}
	var done string
	if err := q.QueryRow(`SELECT favour_day FROM ponds WHERE id = ?`, id).Scan(&done); err != nil {
		return v, err
	}
	who, kind, n := favourOf(today)
	v.Favour = favourView{who, residents[who].name, kind, thingName(kind), n, done == today, favourPebbles}
	return v, nil
}
