package main

import (
	"database/sql"
	"fmt"
	"strings"
)

// A set is four things from across the collections, and finishing one puts
// something on the water for good. Members are kinds the way found things
// are kept together, a hat as hat_<shape>_<colour> and a fish as fish_<kind>.
type set struct {
	kind, name string
	// gives is what it puts on the water and scene the page's key for it.
	gives, scene string
	members      [4]string
}

// Stored by kind, so a new set only ever goes on the end.
var sets = []set{
	{"mushrooms", "Mushrooms", "A mushroom ring", "s_ring", [4]string{"t_button", "f_toadstool", "hat_9_0", "dish_8_9"}},
	{"night", "The night", "A second lantern", "s_lantern", [4]string{"fish_eel", "k_moon", "f_lantern", "t_jar"}},
	{"garden", "The kitchen garden", "A scarecrow", "s_scarecrow", [4]string{"dish_0_0", "hat_15_0", "t_gnome", "m_trowel"}},
	{"shiny", "Shiny things", "A wind chime", "s_chime", [4]string{"t_marble", "pyrite", "t_earring", "fish_koi"}},
	{"workshop", "The workshop", "A little bridge", "s_bridge", [4]string{"t_raft", "t_pot", "hat_12_0", "t_flute"}},
	{"weather", "The weather", "A weathervane", "s_vane", [4]string{"fish_trout", "k_web", "k_frost", "t_globe"}},
}

type memberView struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Tier int    `json:"tier"`
	// Sort says how the page draws it, and a hat or a dish carries its parts.
	Sort string `json:"sort"`
	Hat  *hat   `json:"hat,omitempty"`
	A    string `json:"a,omitempty"`
	B    string `json:"b,omitempty"`
	// Where it comes from, for one not found yet.
	Where string `json:"where"`
	Found bool   `json:"found"`
}

// memberOf is a set's member as the shelf shows it, and false for a kind
// that isn't in any collection.
func memberOf(kind string) (memberView, bool) {
	m := memberView{Kind: kind, Name: thingName(kind)}
	if h, ok := hatOfKind(kind); ok {
		m.Sort, m.Tier, m.Hat, m.Where = "hat", h.tier(), &h, "A hat. Boxes at Little Tully's, the wheel and presents."
		return m, true
	}
	if d, ok := dishOfKind(kind); ok {
		m.Sort, m.Tier, m.Where = "dish", d.tier, "A dish with a name of its own, cooked in the kitchen."
		m.A, m.B = dishParts(kind)
		return m, true
	}
	if f, ok := fishOf(strings.TrimPrefix(kind, fishHad)); ok && strings.HasPrefix(kind, fishHad) {
		m.Name, m.Sort, m.Tier, m.Where = f.name, "fish", f.tier, "A fish. "+f.hint
		return m, true
	}
	if t, ok := trinketOf(kind); ok {
		m.Sort, m.Tier, m.Where = "trinket", t.tier, "A trinket. Boxes at Moth's and presents."
		if _, made := recipeOf(kind); made {
			m.Where = "A trinket a frog makes at the craft bench."
		}
		return m, true
	}
	for _, k := range keepsakes {
		if k.kind == kind {
			m.Name, m.Sort, m.Tier, m.Where = k.name, "keepsake", k.tier, "A keepsake. Comes with "+k.line+"."
			return m, true
		}
	}
	for who, x := range mementos {
		if x.kind == kind {
			m.Sort, m.Tier, m.Where = "memento", mementoTier, "A memento. "+residents[who].name+" gives it to a best friend."
			return m, true
		}
	}
	for _, r := range rocks {
		if r.kind == kind {
			m.Sort, m.Tier, m.Where = "rock", r.tier, "A rock. The wheel, the reeds and Moth."
			return m, true
		}
	}
	for _, f := range furniture {
		if f.kind == kind {
			m.Sort, m.Where = "furniture", "Furniture from Tansy's Pad and Reed, in town."
			return m, true
		}
	}
	return m, false
}

type setView struct {
	Kind    string       `json:"kind"`
	Name    string       `json:"name"`
	Gives   string       `json:"gives"`
	Scene   string       `json:"scene"`
	Members []memberView `json:"members"`
	Have    int          `json:"have"`
	// Done stays true once the set has been finished, and Day is when.
	Done bool   `json:"done"`
	Day  string `json:"day,omitempty"`
}

// everFound adds the hats, dishes and furniture to what a pond has had, so a
// set can ask after any of them by kind.
func everFound(v *view, had map[string]bool) map[string]bool {
	for _, h := range v.found {
		had[hatKind(h)] = true
	}
	for _, d := range v.Kitchen.Cookbook {
		had[d.Kind] = true
	}
	for _, k := range v.Furniture {
		had[k] = true
	}
	return had
}

// setsOf is every set off what the pond has ever found, so a thing cooked or
// sold since still counts. days is the day each finished one was kept.
func setsOf(had map[string]bool, days map[string]string) []setView {
	out := make([]setView, 0, len(sets))
	for _, s := range sets {
		sv := setView{Kind: s.kind, Name: s.name, Gives: s.gives, Scene: s.scene, Day: days[s.kind]}
		for _, k := range s.members {
			m, _ := memberOf(k)
			m.Found = had[k]
			if m.Found {
				sv.Have++
			}
			sv.Members = append(sv.Members, m)
		}
		_, kept := days[s.kind]
		sv.Done = kept || sv.Have == len(s.members)
		out = append(out, sv)
	}
	return out
}

// setMarks is the two halves of a dish a set is waiting on, once the pond
// holds both. Nothing else a set wants is ever in the bag.
func setMarks(v *view) []mark {
	if !v.Open.has("garden") {
		return nil
	}
	var out []mark
	for _, s := range v.Sets {
		for _, m := range s.Members {
			d, ok := dishOfKind(m.Kind)
			if s.Done || m.Found || !ok || !d.has(v.held) {
				continue
			}
			line := fmt.Sprintf("Goes in a dish the %s set is waiting on.", setWord(s.Name))
			out = append(out, mark{ingredients[d.a].kind, line})
			if d.b != d.a {
				out = append(out, mark{ingredients[d.b].kind, line})
			}
		}
	}
	return out
}

// setWord is a set's name inside a sentence, "the night" as "night".
func setWord(name string) string {
	return strings.TrimPrefix(strings.ToLower(name), "the ")
}

// kept reads the day every finished set was kept.
func kept(q querier, id int64) (map[string]string, error) {
	done := map[string]string{}
	rows, err := q.Query(`SELECT kind, day FROM sets WHERE pond_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, day string
		if err := rows.Scan(&kind, &day); err != nil {
			return nil, err
		}
		done[kind] = day
	}
	return done, rows.Err()
}

// landedView is what a move only just earned, for the page to pop up.
type landedView struct {
	Sets []string `json:"sets"`
}

// land keeps every set the view says is finished and has no day yet. It
// writes nothing on a move that finished nothing.
func land(tx *sql.Tx, v *view, p *pond) error {
	l := landedView{Sets: []string{}}
	for i := range v.Sets {
		if s := &v.Sets[i]; s.Done && s.Day == "" {
			s.Day = p.today
			l.Sets = append(l.Sets, s.Kind)
			if _, err := tx.Exec(`INSERT INTO sets (pond_id, kind, day) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, p.ID, s.Kind, p.today); err != nil {
				return err
			}
		}
	}
	if len(l.Sets) > 0 {
		v.Landed = &l
	}
	return nil
}
