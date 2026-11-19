package main

import (
	"cmp"
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
		// A trinket box has the hat box's odds and a meter of its own.
		var tier int
		tier, p.TrinketPity = boxTier(p.TrinketPity)
		t := trinketOfTier(tier)
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

// sellTrinket is Moth buying a spare at a quarter of the going rate, or for
// what's left of the day's room when that's less.
func (s *store) sellTrinket(id int64, kind string) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		t, ok := trinketOf(kind)
		if !ok {
			return errNotHeld
		}
		if left, err := roomLeft(tx, p.ID, shopMoss, p.today); err != nil || left == 0 {
			return cmp.Or(err, errMothFull)
		}
		res, err := tx.Exec(`UPDATE items SET count = count - 1 WHERE pond_id = ? AND kind = ? AND count > 1`, p.ID, kind)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotHeld
		}
		_, err = payOut(tx, p, shopMoss, hatRate[t.tier]/4)
		return err
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
	Pity     pityView      `json:"pity"`
	SpotName []string      `json:"spot_names"`
}

func trinketsOf(q querier, p *pond, held map[string]int) (trinketsView, error) {
	tv := trinketsView{BoxCost: trinketBoxCost, Pity: pityOf(p.TrinketPity), SpotName: trinketSpots}
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

// everHad is every trinket, rock, keepsake and memento a pond has had, and
// every fish in its book under fishHad.
func everHad(q querier, id int64) (map[string]bool, error) {
	return kindsOf(q, `SELECT kind FROM trinkets_found WHERE pond_id = ?1
		UNION SELECT kind FROM rocks_found WHERE pond_id = ?1
		UNION SELECT kind FROM keepsakes WHERE pond_id = ?1
		UNION SELECT kind FROM mementos WHERE pond_id = ?1
		UNION SELECT '`+fishHad+`' || kind FROM fish WHERE pond_id = ?1`, id)
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

// trinketIcons are the share card's copy of the trinkets in icons.js, each
// colour already looked up, which a test holds them to.
var trinketIcons = map[string]struct {
	rows [12]string
	col  map[byte]string
}{
	"t_acorn":       {[12]string{"............", ".....d......", "...DDDDD....", "..DdDdDdD...", "..DDDDDDD...", "..BBBBBBB...", "..BHBBBBB...", "...BBBBB....", "....BBB.....", ".....B......", "............", "............"}, map[byte]string{'B': "#c48a4a", 'D': "#6a4428", 'H': "#e3c57a", 'd': "#4a2e1a"}},
	"t_pinecone":    {[12]string{"............", ".....G......", "....DbD.....", "...DbDbD....", "...bDbDb....", "..DbDbDbD...", "..bDbDbDb...", "...DbDbD....", "....bDb.....", ".....D......", "............", "............"}, map[byte]string{'D': "#6a4428", 'G': "#6fd08c", 'b': "#8a5a32"}},
	"t_marble":      {[12]string{"............", "............", "....uuuu....", "...uUUUUu...", "..uUWRUUUu..", "..uUURRUUu..", "..uUUURRUu..", "..uUUUUUUu..", "...uuuuuu...", "............", "............", "............"}, map[byte]string{'R': "#e2687a", 'U': "#6fc0e0", 'W': "#ffffff", 'u': "#2f7fa8"}},
	"t_button":      {[12]string{"............", "............", "....HHHH....", "...HHHHHH...", "..HHhHHhHH..", "..HHHHHHHH..", "..HHhHHhHH..", "...HHHHHH...", "....hhhh....", "............", "............", "............"}, map[byte]string{'H': "#efe0b8", 'h': "#b8a27a"}},
	"t_boat":        {[12]string{"............", ".....W......", ".....WW.....", ".....WWW....", ".....WWWW...", ".....b......", "..SSSSSSSS..", "...SSSSSS...", "TTTTTTTTTTTT", "............", "............", "............"}, map[byte]string{'S': "#b9c3cf", 'T': "#7fd6c2", 'W': "#ffffff", 'b': "#8a5a32"}},
	"t_duck":        {[12]string{"............", "....FFF.....", "...FFkFF....", "...FFFFFOO..", "....FFF.....", ".F..FFFF....", ".FFFFFFFFF..", ".FFFFFFFFF..", "..fffffff...", "............", "............", "............"}, map[byte]string{'F': "#ffd23a", 'O': "#ff9a4a", 'f': "#d8781a", 'k': "#111522"}},
	"t_cone":        {[12]string{"............", ".....O......", ".....O......", "....OOO.....", "....www.....", "...OOOOO....", "...OOOOO....", "..wwwwwww...", "..OOOOOOO...", ".eeeeeeeee..", "............", "............"}, map[byte]string{'O': "#ff9a4a", 'e': "#3a4250", 'w': "#fff6d0"}},
	"t_chanterelle": {[12]string{"............", "............", "..FFF..FFF..", ".FffFFFFffF.", "..FFffffFF..", "...FFFFFF...", "....FFFF....", "....FFFF....", ".....FF.....", "....FFFF....", "............", "............"}, map[byte]string{'F': "#ffd23a", 'f': "#d8781a"}},
	"t_clover":      {[12]string{"............", "...GG.GG....", "..GlGGGlG...", "..GGGgGGG...", "...GgggG....", "..GGGgGGG...", "..GlGGGlG...", "...GG.GG....", ".....g......", "......g.....", "............", "............"}, map[byte]string{'G': "#6fd08c", 'g': "#3f8a4a", 'l': "#a8e8a0"}},
	"t_earring":     {[12]string{"............", ".....YY.....", "....Y..Y....", ".....YY.....", ".....A......", "....AUA.....", "...AUUUA....", "...AUWUA....", "....AUA.....", ".....A......", "............", "............"}, map[byte]string{'A': "#f2c230", 'U': "#6fc0e0", 'W': "#ffffff", 'Y': "#ffcf6a"}},
	"t_globe":       {[12]string{"............", "...IIIIII...", "..IuuuWuuI..", "..IuWuuuuI..", "..IuuGGuuI..", "..IuGGGGuI..", "...IIIIII...", "..BBBBBBBB..", "..bbbbbbbb..", "............", "............", "............"}, map[byte]string{'B': "#c48a4a", 'G': "#6fd08c", 'I': "#d8f0fa", 'W': "#ffffff", 'b': "#8a5a32", 'u': "#2f7fa8"}},
	"t_gnome":       {[12]string{"............", ".....Q......", "....QQ......", "....QQQ.....", "...QQQQQ....", "...HHHHH....", "...HkHkH....", "...wwwww....", "..UwwwwwU...", "..UUUUUUU...", "...d...d....", "............"}, map[byte]string{'H': "#efe0b8", 'Q': "#e2412d", 'U': "#6fc0e0", 'd': "#4a2e1a", 'k': "#111522", 'w': "#fff6d0"}},
	"t_agaric":      {[12]string{"............", "....QQQQ....", "..QQwQQQQQ..", ".QQQQQQwQQQ.", ".QwQQQQQQwQ.", ".qqqqqqqqqq.", "....HHHH....", "....HHHH....", "....HHHH....", "...HHHHHH...", "............", "............"}, map[byte]string{'H': "#efe0b8", 'Q': "#e2412d", 'q': "#8a2a1a", 'w': "#fff6d0"}},
	"t_robot":       {[12]string{"............", ".....Q......", ".....S......", "...SSSSS....", "...SYSYS....", "...SSSSS....", "..eSSSSSe...", "..eSUUSSe...", "...SSSSS....", "...S...S....", "............", "............"}, map[byte]string{'Q': "#e2412d", 'S': "#b9c3cf", 'U': "#6fc0e0", 'Y': "#ffcf6a", 'e': "#3a4250"}},
	"t_disco":       {[12]string{"............", ".....e......", "....SWSS....", "...SsSsSs...", "..SsWsSsSs..", "..sSsSsWsS..", "..SsSsSsSs..", "...sSsSsS...", "....SsSs....", "............", "............", "............"}, map[byte]string{'S': "#b9c3cf", 'W': "#ffffff", 'e': "#3a4250", 's': "#7f8a99"}},
	"t_jar":         {[12]string{"............", "...BBBBBB...", "....SSSS....", "...S....S...", "..S..Y...S..", "..S...Y..S..", "..S.Y....S..", "..S....Y.S..", "..S..Y...S..", "...SSSSSS...", "............", "............"}, map[byte]string{'B': "#c48a4a", 'S': "#b9c3cf", 'Y': "#ffcf6a"}},
	"t_goldmorel":   {[12]string{"............", ".....AA.....", "....AaAA....", "...AaAaAA...", "...AAaAaA...", "...AaAAaA...", "...AAaAAA...", "....AAAA....", ".....HH.....", ".....HH.....", "....HHHH....", "............"}, map[byte]string{'A': "#f2c230", 'H': "#efe0b8", 'a': "#a87a12"}},
	"t_pearl":       {[12]string{"............", "............", "............", "....WWW.....", "...WWWWW....", "...WWWWh....", "....hhh.....", "..HHHHHHHH..", ".BHHHHHHHHB.", "..BBBBBBBB..", "............", "............"}, map[byte]string{'B': "#c48a4a", 'H': "#efe0b8", 'W': "#f6f0e6", 'h': "#cfc7b8"}},
	"t_raft":        {[12]string{"............", ".....W......", ".....WW.....", ".....WWW....", ".....b......", ".BBBBBBBBBB.", ".bBbBbBbBbB.", ".BBBBBBBBBB.", "TTTTTTTTTTTT", "............", "............", "............"}, map[byte]string{'B': "#c48a4a", 'T': "#7fd6c2", 'W': "#ffffff", 'b': "#8a5a32"}},
	"t_basket":      {[12]string{"............", "....bbbb....", "...b....b...", "..b......b..", ".BYBYBYBYBY.", ".YBYBYBYBYB.", "..BYBYBYBY..", "..YBYBYBYB..", "...bbbbbb...", "............", "............", "............"}, map[byte]string{'B': "#c48a4a", 'Y': "#ffcf6a", 'b': "#8a5a32"}},
	"t_pot":         {[12]string{"............", "............", "....oooo....", ".....OO.....", "....OOOO....", "...OOOOOO...", "..OOOOOOOO..", "..OwOOOOOO..", "...OOOOOO...", "....oooo....", "............", "............"}, map[byte]string{'O': "#ff9a4a", 'o': "#c0602a", 'w': "#fff6d0"}},
	"t_flute":       {[12]string{"............", ".........hH.", "........hHh.", ".......hkh..", "......hHh...", ".....hkh....", "....hHh.....", "...hkh......", "..hHh.......", ".hHh........", "............", "............"}, map[byte]string{'H': "#d6c88f", 'h': "#8a7a3a", 'k': "#111522"}},
	"t_statue":      {[12]string{"............", "...ww..ww...", "..wkkwwkkw..", "..OOOOOOOO..", ".OOOOOOOOOO.", ".OOoooooOOO.", ".OOOOOOOOOO.", "..OO....OO..", ".SSSSSSSSSS.", ".ssssssssss.", "............", "............"}, map[byte]string{'O': "#ff9a4a", 'S': "#b9c3cf", 'k': "#111522", 'o': "#c0602a", 's': "#7f8a99", 'w': "#fff6d0"}},
	"t_bee":         {[12]string{"............", "....II.II...", "....IIIII...", ".....III....", "...AAAAAA...", "..AkAkkAkA..", "..AAkAAkAA..", "...AkkAkA...", "....AAAA....", "............", "............", "............"}, map[byte]string{'A': "#f2c230", 'I': "#d8f0fa", 'k': "#111522"}},
	"t_ladybird":    {[12]string{"............", ".....kk.....", "....kkkk....", "...QQkkQQ...", "..QQkQQkQQ..", "..QQQkkQQQ..", "..QkQQQQkQ..", "..QQQkkQQQ..", "...QQQQQQ...", "....k..k....", "............", "............"}, map[byte]string{'Q': "#e2412d", 'k': "#111522"}},
	"t_hedgehog":    {[12]string{"............", "....D.D.D...", "...DDDDDDD..", "..DdDDdDDdD.", ".HDDDDDDDDD.", "HkHDDdDDDdD.", "HHHHDDDDDD..", ".HH.H...H...", "............", "............", "............", "............"}, map[byte]string{'D': "#6a4428", 'H': "#e3c57a", 'd': "#4a2e1a", 'k': "#111522"}},
	"t_dragonfly":   {[12]string{"............", ".....kk.....", "IIII.uu.IIII", ".III.uu.III.", "IIII.uu.IIII", ".....uu.....", ".....uu.....", ".....uu.....", ".....uu.....", "......u.....", "............", "............"}, map[byte]string{'I': "#d8f0fa", 'k': "#111522", 'u': "#2f7fa8"}},
	"t_newt":        {[12]string{"............", "............", ".OO.........", "OkOO........", "OOOOFOOO....", ".OOOOOOOOO..", ".O..O..O.OO.", "..........O.", "...........O", "............", "............", "............"}, map[byte]string{'F': "#ffd23a", 'O': "#ff9a4a", 'k': "#111522"}},
	"t_crayfish":    {[12]string{"............", ".QQ......QQ.", ".Q.Q....Q.Q.", "..QQ....QQ..", "....QkkQ....", "....QQQQ....", "....QqqQ....", "....QQQQ....", ".....QQ.....", "....QQQQ....", "............", "............"}, map[byte]string{'Q': "#e2412d", 'k': "#111522", 'q': "#8a2a1a"}},
	"t_mouse":       {[12]string{"............", "..hh........", ".hHHh.......", ".hHhh.......", "..hhhhhh....", ".khhhhhhh...", "..hhhhhhh...", "...h...h.p..", ".........p..", "........p...", "............", "............"}, map[byte]string{'H': "#e9dcc0", 'h': "#a8865a", 'k': "#111522", 'p': "#f6b8cc"}},
}
