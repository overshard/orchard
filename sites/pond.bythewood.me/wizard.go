package main

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The frog wizard takes three of the same thing and turns them into one thing
// of any sort, mostly a tier down, sometimes the same tier and now and then a
// tier up. Three in and one out keeps it from ever making more than it took.
const wizardTakes = 3

var wizardOdds = struct{ up, same int }{15, 35}

// What a wizard's stew is made of when it goes down from common.
const wizardDust = 2

// Bench things and flotsam have no tier of their own anywhere else.
var benchTier = map[string]int{
	"snail": 0, "cap": 0, "feather": 0,
	"geode": 1, "dye_olive": 1, "dye_brown": 1, "dye_grey": 1,
	"dye_berry": 2, "dye_gold": 2, "stamp_spots": 1, "stamp_bands": 1,
	"stamp_speckled": 2, "stamp_mask": 2, "stamp_stripe": 2,
	"dye_blue": 3, "drops_red": 3, "drops_pale": 3,
}

// wizardTier is the tier of anything the wizard will take, as an item kind or
// hat_<shape>_<colour>.
func wizardTier(kind string) (int, bool) {
	if h, ok := hatOfKind(kind); ok {
		return h.tier(), true
	}
	if t, ok := benchTier[kind]; ok {
		return t, true
	}
	for _, r := range rocks {
		if r.kind == kind {
			return r.tier, true
		}
	}
	if t, ok := trinketOf(kind); ok {
		return t.tier, true
	}
	if i, ok := ingredientOf(kind); ok {
		return ingredients[i].tier, true
	}
	if d, ok := dishOfKind(kind); ok {
		return d.tier, true
	}
	return 0, false
}

func hatKind(h hat) string { return fmt.Sprintf("hat_%d_%d", h.Shape, h.Colour) }

func hatOfKind(kind string) (hat, bool) {
	rest, ok := strings.CutPrefix(kind, "hat_")
	if !ok {
		return hat{}, false
	}
	a, b, ok := strings.Cut(rest, "_")
	s, err1 := strconv.Atoi(a)
	c, err2 := strconv.Atoi(b)
	h := hat{Shape: s, Colour: c}
	if !ok || err1 != nil || err2 != nil || !h.valid() || h.Shape == 0 {
		return hat{}, false
	}
	return h, true
}

// wizardPool is what can come out at each tier, one list per sort, so a tier
// with sixty hats in it doesn't turn out a hat every time.
var wizardPool = func() [tierCount][][]string {
	var pool [tierCount][][]string
	add := func(sort map[int][]string) {
		for t, kinds := range sort {
			pool[t] = append(pool[t], kinds)
		}
	}
	rk, tk, ik, bk, hk := map[int][]string{}, map[int][]string{}, map[int][]string{}, map[int][]string{}, map[int][]string{}
	for _, r := range rocks {
		rk[r.tier] = append(rk[r.tier], r.kind)
	}
	for _, t := range trinkets {
		tk[t.tier] = append(tk[t.tier], t.kind)
	}
	for _, in := range ingredients {
		if !in.trinket {
			ik[in.tier] = append(ik[in.tier], in.kind)
		}
	}
	for _, k := range sortedTierKeys(benchTier) {
		bk[benchTier[k]] = append(bk[benchTier[k]], k)
	}
	for t, hs := range hatsByTier {
		for _, h := range hs {
			hk[t] = append(hk[t], hatKind(h))
		}
	}
	for _, m := range []map[int][]string{rk, tk, ik, bk, hk} {
		add(m)
	}
	return pool
}()

func sortedTierKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func rollWizard(tier int) (int, string) {
	r := rand.IntN(100)
	switch {
	case r < wizardOdds.up:
		tier = min(tier+1, tierCount-1)
	case r < wizardOdds.up+wizardOdds.same:
	default:
		tier--
	}
	if tier < 0 {
		return tier, ""
	}
	sorts := wizardPool[tier]
	kinds := sorts[rand.IntN(len(sorts))]
	return tier, kinds[rand.IntN(len(kinds))]
}

type wizarded struct {
	Pond *view `json:"pond"`
	// up, same, down or dust.
	Went  string `json:"went"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Tier  int    `json:"tier"`
	Hat   *hat   `json:"hat,omitempty"`
	Spare bool   `json:"spare"`
	Dust  int    `json:"dust,omitempty"`
}

// keepsOne is a sort where the last of a thing is part of a collection, so
// the wizard only takes spares of it.
func keepsOne(kind string) bool {
	_, isHat := hatOfKind(kind)
	return isHat || strings.HasPrefix(kind, "t_")
}

func (s *store) wizard(id int64, kind string) (*wizarded, error) {
	from, ok := wizardTier(kind)
	if !ok {
		return nil, errNotHeld
	}
	out := &wizarded{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		need := wizardTakes
		if keepsOne(kind) {
			need++
		}
		if h, ok := hatOfKind(kind); ok {
			res, err := tx.Exec(`UPDATE hats SET count = count - ? WHERE pond_id = ? AND shape = ? AND colour = ? AND count >= ?`,
				wizardTakes, p.ID, h.Shape, h.Colour, need)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return errNotHeld
			}
		} else {
			res, err := tx.Exec(`UPDATE items SET count = count - ? WHERE pond_id = ? AND kind = ? AND count >= ?`,
				wizardTakes, p.ID, kind, need)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return errNotHeld
			}
		}
		tier, got := rollWizard(from)
		switch {
		case got == "":
			out.Went, out.Dust = "dust", wizardDust
			p.Pebbles += wizardDust
			return nil
		case tier > from:
			out.Went = "up"
		case tier == from:
			out.Went = "same"
		default:
			out.Went = "down"
		}
		out.Kind, out.Tier = got, tier
		z := prize{Item: got}
		if h, ok := hatOfKind(got); ok {
			z = prize{Hat: &h}
			out.Hat = &h
		}
		if err := award(tx, p, &z); err != nil {
			return err
		}
		out.Spare = z.Spare
		out.Name = thingName(got)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Pond = v
	return out, nil
}

// thingName names any item kind or hat kind.
func thingName(kind string) string {
	if h, ok := hatOfKind(kind); ok {
		return h.name()
	}
	if t, ok := trinketOf(kind); ok {
		return t.name
	}
	if d, ok := dishOfKind(kind); ok {
		return d.name
	}
	if n, ok := itemNames[kind]; ok {
		return n
	}
	if i, ok := ingredientOf(kind); ok {
		return ingredients[i].name
	}
	return kind
}
