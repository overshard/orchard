package main

import "database/sql"

// A memento is the one thing a neighbour gives a best friend. They're kept
// in their own table with the day and never as items, so nothing can sell
// one, list it or take it to the wizard.
type memento struct {
	kind string
	// thing is its name without whose it was, for the diary.
	thing string
	// line is what the neighbour says handing it over.
	line string
}

// In the same order as residents.
var mementos = []memento{
	{"m_float", "lucky float", "Take it. It never caught me a thing, so it might as well sit with you."},
	{"m_notebook", "notebook", "Everything I know about everybody is in here. Don't read the page about you."},
	{"m_gizmo", "first invention", "It's my first invention. I don't know what it does yet, so you can find out."},
	{"m_bag", "spare bag", "It's my spare bag, packed and ready, for when we go. Any day now."},
	{"m_trowel", "old trowel", "It's dug every bed on this bank. Mind you keep it clean."},
	{"m_mud", "jar of good mud", "This is the good mud, from my best spot. It's very nice to sit near."},
	{"m_spoon", "tasting spoon", "My tasting spoon. It's had a bit of everything, so it's nearly a meal."},
	{"m_leaf", "weather leaf", "The first forecast I ever wrote down. It says rain, and it did."},
	{"m_medal", "second best medal", "My second best medal. I'm keeping the best one, and this one's still very good."},
	{"m_poem", "poem about a puddle", "I wrote it for you. It's about a puddle and it made the crickets cry."},
	{"m_lantern", "spare lantern", "A spare lantern, trimmed and filled. Hang it somewhere sensible."},
	{"m_shiny", "shiny thing", "This is the shiny thing I found. I wasn't going to show anybody."},
}

// A memento is drawn and counted as an epic whoever it's from.
const mementoTier = 3

func (m memento) name(who int) string { return residents[who].name + "'s " + m.thing }

// handMemento gives a neighbour's memento over, and nothing the second time.
func handMemento(tx *sql.Tx, p *pond, who int) (*prize, error) {
	m := mementos[who]
	res, err := tx.Exec(`INSERT INTO mementos (pond_id, kind, day) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, p.ID, m.kind, p.today)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil
	}
	return &prize{Item: m.kind, Text: m.line, HatTier: mementoTier}, nil
}

// owedMementos hands over one from every best friend that hasn't given its
// own yet, for a pond that got there before there were any.
func owedMementos(tx *sql.Tx, p *pond) ([]prize, []int, error) {
	friends, err := loadFriends(tx, p.ID)
	if err != nil {
		return nil, nil, err
	}
	prizes, from := []prize{}, []int{}
	for who := range residents {
		if friendLevel(friends[who].points) < bestFriend {
			continue
		}
		z, err := handMemento(tx, p, who)
		if err != nil {
			return nil, nil, err
		}
		if z != nil {
			prizes, from = append(prizes, *z), append(from, who)
		}
	}
	return prizes, from, nil
}

type mementoView struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Tier int    `json:"tier"`
	// Who is the resident it comes from, by place and by name.
	Who  int    `json:"who"`
	From string `json:"from"`
	// Day is when it was handed over, "" until then.
	Day string `json:"day"`
}

func mementosOf(q querier, id int64) ([]mementoView, error) {
	days := map[string]string{}
	rows, err := q.Query(`SELECT kind, day FROM mementos WHERE pond_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, d string
		if err := rows.Scan(&k, &d); err != nil {
			return nil, err
		}
		days[k] = d
	}
	out := make([]mementoView, 0, len(mementos))
	for who, m := range mementos {
		out = append(out, mementoView{m.kind, m.name(who), mementoTier, who, residents[who].name, days[m.kind]})
	}
	return out, rows.Err()
}
