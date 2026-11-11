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
	{"t_raft", "Stick raft", 0},
	{"t_basket", "Reed basket", 0},
	{"t_pot", "Clay pot", 1},
	{"t_flute", "Reed flute", 2},
	{"t_statue", "Clay frog statue", 3},
	{"t_bee", "Bumblebee", 4},
	{"t_ladybird", "Ladybird", 4},
	{"t_hedgehog", "Hedgehog", 4},
	{"t_dragonfly", "Dragonfly", 4},
	{"t_newt", "Newt", 4},
	{"t_crayfish", "Crayfish", 4},
	{"t_mouse", "Workshop mouse", 4},
}

// Pets only turn up at work, by job, so no box, present or wizard hands one out.
var pets = map[string]string{
	"t_bee":       "garden",
	"t_ladybird":  "bugs",
	"t_hedgehog":  "sticks",
	"t_dragonfly": "reeds",
	"t_newt":      "mud",
	"t_crayfish":  "pebbles",
	"t_mouse":     "craft",
}

// petEvery is the work hours between pets at level 1, half that at 99.
const petEvery = 3000

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
		if x.tier == t && pets[x.kind] == "" {
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

// sellTrinket is Moth buying a spare at a quarter of the going rate.
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
	// The job a pet turns up at, by name.
	Pet string `json:"pet,omitempty"`
}

type trinketsView struct {
	All      []trinketView `json:"all"`
	Spots    []string      `json:"spots"`
	BoxCost  int           `json:"box_cost"`
	SpotName []string      `json:"spot_names"`
}

func trinketsOf(q querier, p *pond, held map[string]int) (trinketsView, error) {
	tv := trinketsView{BoxCost: trinketBoxCost, SpotName: trinketSpots}
	found, err := foundTrinkets(q, p.ID)
	if err != nil {
		return tv, err
	}
	for _, t := range trinkets {
		_, cook := ingredientOf(t.kind)
		var pet string
		if j, ok := jobOf(pets[t.kind]); ok {
			pet = j.name
		}
		tv.All = append(tv.All, trinketView{t.kind, t.name, t.tier, held[t.kind], found[t.kind], cook, hatRate[t.tier] / 4, pet})
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

// foundTrinkets is every trinket a pond has ever had, which is what the set
// and frog power count.
func foundTrinkets(q querier, id int64) (map[string]bool, error) {
	return kindsOf(q, `SELECT kind FROM trinkets_found WHERE pond_id = ?1`, id)
}

// everHad is every trinket, rock and keepsake a pond has had.
func everHad(q querier, id int64) (map[string]bool, error) {
	return kindsOf(q, `SELECT kind FROM trinkets_found WHERE pond_id = ?1
		UNION SELECT kind FROM rocks_found WHERE pond_id = ?1
		UNION SELECT kind FROM keepsakes WHERE pond_id = ?1`, id)
}

func kindsOf(q querier, query string, id int64) (map[string]bool, error) {
	found := map[string]bool{}
	rows, err := q.Query(query, id)
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
	return found, rows.Err()
}

// spotsOut is what's sitting round the pad for the share page, with a spot
// whose trinket ran out shown empty.
func spotsOut(q querier, p *pond) ([]string, error) {
	spots := parseSpots(p.TrinketsOut)
	for i, k := range spots {
		if k == "" {
			continue
		}
		var n int
		if err := q.QueryRow(`SELECT COALESCE(SUM(count), 0) FROM items WHERE pond_id = ? AND kind = ?`, p.ID, k).Scan(&n); err != nil {
			return nil, err
		}
		if n < 1 {
			spots[i] = ""
		}
	}
	return spots, nil
}

// trinketIcons are the share card's copy of the trinkets in icons.js, which
// a test holds them to.
var trinketIcons = map[string]struct {
	rows [8]string
	col  map[byte]string
}{
	"t_acorn":       {[8]string{"...a....", ".bbbbb..", "bcbcbcb.", "bbbbbbb.", ".ddddd..", ".ddedd..", "..ddd...", "...d...."}, map[byte]string{'a': "#6b4a2a", 'b': "#8a5a2b", 'c': "#5a3f24", 'd': "#c9974f", 'e': "#e3c57a"}},
	"t_pinecone":    {[8]string{"...a....", "..bcb...", ".bcbcb..", ".cbcbc..", ".bcbcb..", ".cbcbc..", "..bcb...", "...b...."}, map[byte]string{'a': "#3f9a4d", 'b': "#8a5a2b", 'c': "#5a3f24"}},
	"t_marble":      {[8]string{"........", "..aaa...", ".abcaa..", ".acbba..", ".aabba..", "..aaa...", "........", "........"}, map[byte]string{'a': "#3d8fd6", 'b': "#b5dcff", 'c': "#e2567a"}},
	"t_button":      {[8]string{"........", "..aaaa..", ".aaaaaa.", ".abaaba.", "..cccc..", "...cc...", "...cc...", "..cccc.."}, map[byte]string{'a': "#e9dcc0", 'b': "#c9b896", 'c': "#fff6dc"}},
	"t_boat":        {[8]string{"........", "...a....", "...ab...", "...abb..", "aaaaaaaa", ".aaaaaa.", "..aaaa..", "cccccccc"}, map[byte]string{'a': "#e9e3d6", 'b': "#c9c3b6", 'c': "#7fd6c2"}},
	"t_duck":        {[8]string{"..aa....", ".aaba...", ".aaaacc.", "..aa....", "aaaaaaa.", "aaaaaaa.", ".aaaaa..", "........"}, map[byte]string{'a': "#f2c230", 'b': "#1a1a1a", 'c': "#e08a1a"}},
	"t_cone":        {[8]string{"...a....", "...a....", "..bbb...", "..aaa...", ".aaaaa..", ".bbbbb..", "aaaaaaa.", "cccccccc"}, map[byte]string{'a': "#e86a1a", 'b': "#fff6dc", 'c': "#3a3a3a"}},
	"t_chanterelle": {[8]string{"........", ".aaaaaa.", "aabbbbaa", "..aaaa..", "...aa...", "...aa...", "..aaaa..", "........"}, map[byte]string{'a': "#f2a23a", 'b': "#c9741a"}},
	"t_clover":      {[8]string{"..a.a...", ".aaaaa..", "..aba...", ".aaaaa..", "..a.a...", "...c....", "....c...", "........"}, map[byte]string{'a': "#3f9a4d", 'b': "#9fe08f", 'c': "#2f7a3f"}},
	"t_earring":     {[8]string{"...a....", "..a.a...", "...a....", "...b....", "..bcb...", ".bcccb..", "..bcb...", "...b...."}, map[byte]string{'a': "#ffe27a", 'b': "#c9b25a", 'c': "#3d8fd6"}},
	"t_globe":       {[8]string{".aaaaaa.", "abbcbbba", "abbbbcba", "abcdddba", "abbdddba", ".aaaaaa.", "eeeeeeee", ".eeeeee."}, map[byte]string{'a': "#cfe3ea", 'b': "#3b5a6a", 'c': "#ffffff", 'd': "#3f9a4d", 'e': "#8a5a2b"}},
	"t_gnome":       {[8]string{"...a....", "..aaa...", ".aaaaa..", "..bbb...", ".bdddb..", ".eddde..", ".eeeee..", "..f.f..."}, map[byte]string{'a': "#e2412d", 'b': "#f6dcb0", 'd': "#fff6dc", 'e': "#3d8fd6", 'f': "#5a3f24"}},
	"t_agaric":      {[8]string{"..aaaa..", ".abaaba.", "aaaabaaa", "abaaaaba", "..cccc..", "...cc...", "...cc...", "..cccc.."}, map[byte]string{'a': "#e2412d", 'b': "#fff6dc", 'c': "#e9dcc0"}},
	"t_robot":       {[8]string{"...a....", "..bbbb..", "..bcbc..", "..bbbb..", ".dbbbbd.", ".dbeebd.", "..bbbb..", "..b..b.."}, map[byte]string{'a': "#e2412d", 'b': "#9aa0a6", 'c': "#ffe27a", 'd': "#6d747a", 'e': "#3d8fd6"}},
	"t_disco":       {[8]string{"...a....", "..bcbc..", ".bcdcbc.", ".cbcbcb.", ".bcbcdc.", "..cbcb..", "........", "........"}, map[byte]string{'a': "#6d747a", 'b': "#e9e3d6", 'c': "#9aa0a6", 'd': "#ffffff"}},
	"t_jar":         {[8]string{"..aaaa..", ".b....b.", ".bcbbcb.", ".bbcbbb.", ".bbbbcb.", ".bcbbbb.", ".bbbbbb.", "..bbbb.."}, map[byte]string{'a': "#6d747a", 'b': "#2a4a5a", 'c': "#ffe27a"}},
	"t_goldmorel":   {[8]string{"...aa...", "..abba..", ".ababba.", ".abbaba.", "..abba..", "...cc...", "...cc...", "..cccc.."}, map[byte]string{'a': "#ffe27a", 'b': "#a87a12", 'c': "#fff6dc"}},
	"t_pearl":       {[8]string{"........", "........", "..aaa...", ".abaaa..", ".aaaac..", "..acc...", "dddddddd", ".dddddd."}, map[byte]string{'a': "#f6f0e6", 'b': "#ffffff", 'c': "#cfc7b8", 'd': "#e3c57a"}},
	"t_raft":        {[8]string{"........", "...c....", "...cd...", "...cdd..", "aaaaaaaa", "babababa", "aaaaaaaa", "eeeeeeee"}, map[byte]string{'a': "#8a5a2b", 'b': "#6b4a2a", 'c': "#e9e3d6", 'd': "#c9c3b6", 'e': "#7fd6c2"}},
	"t_basket":      {[8]string{"........", "..aaaa..", ".a....a.", "bbbbbbbb", "bcbcbcbc", "cbcbcbcb", ".bcbcbc.", "..bbbb.."}, map[byte]string{'a': "#b08a5a", 'b': "#c9974f", 'c': "#8a6230"}},
	"t_pot":         {[8]string{"........", "..aaaa..", "...bb...", "..bbbb..", ".bbcbbb.", ".bbbbbb.", "..bbbb..", "...bb..."}, map[byte]string{'a': "#a8642e", 'b': "#c9743a", 'c': "#e8a05a"}},
	"t_flute":       {[8]string{"......aa", ".....aba", "....aba.", "...aba..", "..aba...", ".aba....", "aba.....", "aa......"}, map[byte]string{'a': "#8a7a3a", 'b': "#d6c88f"}},
	"t_statue":      {[8]string{"........", ".a....a.", "abaaaaba", "aaaaaaaa", "aacaacaa", ".aaaaaa.", "dddddddd", ".dddddd."}, map[byte]string{'a': "#b8642e", 'b': "#1a1a1a", 'c': "#e8a05a", 'd': "#8a939e"}},
	"t_bee":         {[8]string{"..c.c...", ".ccccc..", "..ccc...", ".babab..", "bababab.", ".babab..", "..a.a...", "........"}, map[byte]string{'a': "#1a1a1a", 'b': "#f2c230", 'c': "#cfe3ea"}},
	"t_ladybird":    {[8]string{"...bb...", "..bbbb..", ".aabbaa.", "abaaaaba", "aaaaaaaa", "aabaabaa", ".aaaaaa.", "..b..b.."}, map[byte]string{'a': "#e2412d", 'b': "#1a1a1a"}},
	"t_hedgehog":    {[8]string{"........", "...a.a..", "..aaaaa.", ".aaaaaaa", "cdaaaaaa", "bccaaaaa", ".cc.c.c.", "........"}, map[byte]string{'a': "#6b4a2a", 'b': "#1a1a1a", 'c': "#e3c57a", 'd': "#1a1a1a"}},
	"t_dragonfly":   {[8]string{"...cc...", "bbbaabbb", ".bbaabb.", "bbbaabbb", "...aa...", "...aa...", "...aa...", "....a..."}, map[byte]string{'a': "#3d8fd6", 'b': "#cfe3ea", 'c': "#1a1a1a"}},
	"t_newt":        {[8]string{"........", ".aa.....", "abaa....", "aaaacaa.", ".aaaaaaa", ".c..c..a", "........", "........"}, map[byte]string{'a': "#e8742a", 'b': "#1a1a1a", 'c': "#f2c230"}},
	"t_crayfish":    {[8]string{"a......a", "aa....aa", ".a.cc.a.", "..aaaa..", "..abba..", "..aaaa..", "...ab...", "..aaaa.."}, map[byte]string{'a': "#c9432d", 'b': "#8a2a1a", 'c': "#1a1a1a"}},
	"t_mouse":       {[8]string{"........", ".aa.....", "abaa....", "acaaaa..", "aaaaaaa.", ".aaaaaad", "..b..b.d", "......d."}, map[byte]string{'a': "#a8865a", 'b': "#e9dcc0", 'c': "#1a1a1a", 'd': "#e3a0a0"}},
}
