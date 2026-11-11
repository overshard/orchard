package main

import (
	"database/sql"
	"errors"
	"hash/fnv"
	"time"
)

// Bond is how well a frog knows its visitor, in points with a heart every
// bondHeart of them. A rub adds a little, only so much a day, and nothing
// ever takes it away.
const (
	bondHeart  = 100
	bondHearts = 5
	bondADay   = 20
	petPoints  = 2
)

var petSpots = []string{"head", "chin", "back"}

// favSpot is where a frog likes a rub best, which counts double.
func favSpot(seed uint32) string {
	h := fnv.New32a()
	h.Write([]byte{'s', 'p', 'o', 't', byte(seed), byte(seed >> 8), byte(seed >> 16), byte(seed >> 24)})
	return petSpots[h.Sum32()%uint32(len(petSpots))]
}

type bondView struct {
	Hearts int `json:"hearts"`
	// How far along the next heart is, in a hundred.
	Fill int `json:"fill"`
	// Points it can still gain today.
	Left int `json:"left"`
}

func bondOf(points int, day string, today int, now time.Time) bondView {
	if day != dayOf(now) {
		today = 0
	}
	v := bondView{Hearts: points / bondHeart, Fill: points % bondHeart * 100 / bondHeart, Left: bondADay - today}
	if v.Hearts >= bondHearts {
		v.Hearts, v.Fill, v.Left = bondHearts, 0, 0
	}
	return v
}

type bondRow struct {
	id     int64
	seed   uint32
	points int
	day    string
	today  int
}

func loadBond(q querier, pondID int64) (bondRow, error) {
	var b bondRow
	err := q.QueryRow(`SELECT id, seed, bond, bond_day, bond_today FROM frogs WHERE pond_id = ? AND out_front = 1`, pondID).
		Scan(&b.id, &b.seed, &b.points, &b.day, &b.today)
	if errors.Is(err, sql.ErrNoRows) {
		return b, errNoPond
	}
	return b, err
}

type petted struct {
	Bond bondView `json:"bond"`
	// Fav is a rub on its favourite spot, and Heart a new heart from this one.
	Fav   bool `json:"fav"`
	Heart bool `json:"heart"`
}

// pet is one rub of the frog out front. It's called as fast as a finger can
// rub, so a frog that's had its day's worth is answered off the reading pool.
func (s *store) pet(id int64, spot string) (*petted, error) {
	now := s.now()
	b, err := loadBond(s.r, id)
	if err != nil {
		return nil, err
	}
	out := &petted{Bond: bondOf(b.points, b.day, b.today, now), Fav: spot == favSpot(b.seed)}
	if out.Bond.Left <= 0 {
		return out, nil
	}
	err = s.write(func(tx *sql.Tx) error {
		b, err := loadBond(tx, id)
		if err != nil {
			return err
		}
		today := dayOf(now)
		if b.day != today {
			b.day, b.today = today, 0
		}
		add := petPoints
		if out.Fav {
			add *= 2
		}
		add = max(0, min(add, bondADay-b.today, bondHeart*bondHearts-b.points))
		out.Heart = (b.points+add)/bondHeart > b.points/bondHeart
		b.points += add
		b.today += add
		out.Bond = bondOf(b.points, b.day, b.today, now)
		heart := ""
		if out.Heart {
			heart = today
		}
		_, err = tx.Exec(`UPDATE frogs SET bond = ?, bond_day = ?, bond_today = ?,
			heart_day = CASE WHEN ? != '' THEN ? ELSE heart_day END WHERE id = ?`,
			b.points, b.day, b.today, heart, heart, b.id)
		return err
	})
	return out, err
}
