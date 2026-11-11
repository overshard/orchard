package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Burrows are the bank's fourth build. Each level digs one burrow and adds a
// pad, and a neighbour who's friendly enough can move into a burrow and pay
// rent into it.
var burrowCost = []int{300, 700, 1500, 3000, 6000}

const (
	// Pebbles an hour each lodger pays.
	rentRate = 4
	// How friendly a neighbour has to be before it'll move in.
	lodgeAt = 2
)

var (
	errNoBurrow = errors.New("every burrow has somebody in it, so dig another first")
	errNotLodge = errors.New("it doesn't know you well enough to move in yet")
)

func init() {
	buildMats["burrow"] = [buildLevels]map[string]int{nil, {"sticks": 30}, {"sticks": 80, "clay": 20}, {"sticks": 200, "clay": 60}, {"sticks": 400, "clay": 150}}
}

type burrows struct {
	level   int
	at      int64
	lodgers []int
	rentDay string
}

func loadBurrows(q querier, id int64) (burrows, error) {
	var b burrows
	var list string
	err := q.QueryRow(`SELECT burrow_level, burrow_at, lodgers, rent_day FROM ponds WHERE id = ?`, id).
		Scan(&b.level, &b.at, &list, &b.rentDay)
	for _, s := range strings.Split(list, ",") {
		if n, e := strconv.Atoi(s); e == nil && n >= 0 && n < len(residents) {
			b.lodgers = append(b.lodgers, n)
		}
	}
	return b, err
}

func (b burrows) save(tx *sql.Tx, id int64) error {
	ss := make([]string, len(b.lodgers))
	for i, n := range b.lodgers {
		ss[i] = strconv.Itoa(n)
	}
	_, err := tx.Exec(`UPDATE ponds SET burrow_level = ?, burrow_at = ?, lodgers = ?, rent_day = ? WHERE id = ?`,
		b.level, b.at, strings.Join(ss, ","), b.rentDay, id)
	return err
}

func (b burrows) lodging(who int) bool {
	for _, n := range b.lodgers {
		if n == who {
			return true
		}
	}
	return false
}

// held is the rent waiting and when the clock was last emptied, like the
// gravel bar's, at a rate set by how many are lodging.
func (b burrows) held(now time.Time) (int, int64) {
	rate := rentRate * len(b.lodgers)
	if rate == 0 {
		return 0, now.Unix()
	}
	per := int64(3600 / rate)
	n := int((now.Unix() - b.at) / per)
	limit := rate * int(holdFor/time.Hour)
	if n >= limit {
		return limit, now.Unix()
	}
	return max(n, 0), b.at + int64(n)*per
}

// payOut empties the rent before the lodgers change, so a move never loses any.
func (b *burrows) payOut(p *pond, now time.Time) {
	n, _ := b.held(now)
	p.Pebbles += n
	b.at = now.Unix()
}

func (s *store) buildBurrow(tx *sql.Tx, p *pond, now time.Time) error {
	b, err := loadBurrows(tx, p.ID)
	if err != nil {
		return err
	}
	if b.level >= buildLevels {
		return errMaxed
	}
	cost := burrowCost[b.level]
	if p.Pebbles < cost {
		return errPoor
	}
	if err := payMats(tx, p.ID, "burrow", b.level); err != nil {
		return err
	}
	p.Pebbles -= cost
	b.level++
	p.Pads++
	return b.save(tx, p.ID)
}

// lodge moves a neighbour into a free burrow, or out of its own.
func (s *store) lodge(id int64, who int, in bool) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if who < 0 || who >= len(residents) {
			return errNotHeld
		}
		b, err := loadBurrows(tx, p.ID)
		if err != nil {
			return err
		}
		if in == b.lodging(who) {
			return errNotYet
		}
		if in {
			if len(b.lodgers) >= b.level {
				return errNoBurrow
			}
			friends, err := loadFriends(tx, p.ID)
			if err != nil {
				return err
			}
			if friendLevel(friends[who].points) < lodgeAt {
				return errNotLodge
			}
		}
		b.payOut(p, now)
		if in {
			b.lodgers = append(b.lodgers, who)
		} else {
			kept := b.lodgers[:0]
			for _, n := range b.lodgers {
				if n != who {
					kept = append(kept, n)
				}
			}
			b.lodgers = kept
		}
		return b.save(tx, p.ID)
	})
}

// paidOut is pebbles collected, with any presents from neighbours it brought.
type paidOut struct {
	Pebbles int     `json:"pebbles"`
	Line    string  `json:"line"`
	Prizes  []prize `json:"prizes"`
	// Who each present is from, in the same order.
	From []int `json:"from"`
	Pond *view `json:"pond"`
}

// collectRent takes the rent, and the first time each day every lodger gets a
// little friendlier for being there.
func (s *store) collectRent(id int64) (*paidOut, error) {
	out := &paidOut{Prizes: []prize{}, From: []int{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		b, err := loadBurrows(tx, p.ID)
		if err != nil {
			return err
		}
		if len(b.lodgers) == 0 {
			return errNotHeld
		}
		n, at := b.held(now)
		if n < 1 {
			return errNotYet
		}
		p.Pebbles += n
		b.at = at
		out.Pebbles = n
		out.Line = fmt.Sprintf("%d pebbles of rent.", n)
		if today := dayOf(now); b.rentDay != today {
			b.rentDay = today
			for _, who := range b.lodgers {
				zs, err := befriend(tx, p, who, 1, "")
				if err != nil {
					return err
				}
				for _, z := range zs {
					out.Prizes = append(out.Prizes, z)
					out.From = append(out.From, who)
				}
			}
			out.Line = fmt.Sprintf("%d pebbles of rent, and a chat with everyone in the burrows.", n)
		}
		return b.save(tx, p.ID)
	})
	out.Pond = v
	return out, err
}

type burrowView struct {
	Level   int            `json:"level"`
	Cost    int            `json:"cost"`
	Mats    map[string]int `json:"mats"`
	Lodgers []int          `json:"lodgers"`
	Rate    int            `json:"rate"`
	Held    int            `json:"held"`
	Holds   int            `json:"holds"`
	// Seconds until the next pebble, 0 while it's full or empty.
	NextIn  int64 `json:"next_in"`
	Chatted bool  `json:"chatted"`
	LodgeAt int   `json:"lodge_at"`
	// Paint is the pieces of the paintings in, paintPieces to a painting.
	Paint int `json:"paint"`
}

// Each visitor and each stamp on the frog's page leaves a piece of a pond
// painting, and a finished one hangs in the burrows.
const (
	paintPieces = 12
	paintings   = 4
)

// addPaint puts n pieces in and says which painting that finished, or -1.
func addPaint(tx *sql.Tx, id int64, n int) (int, error) {
	var was int
	if err := tx.QueryRow(`SELECT paint FROM ponds WHERE id = ?`, id).Scan(&was); err != nil {
		return -1, err
	}
	now := min(was+n, paintPieces*paintings)
	if now == was {
		return -1, nil
	}
	if _, err := tx.Exec(`UPDATE ponds SET paint = ? WHERE id = ?`, now, id); err != nil {
		return -1, err
	}
	if now/paintPieces > was/paintPieces {
		return now/paintPieces - 1, nil
	}
	return -1, nil
}

func burrowViewOf(q querier, id int64, now time.Time) (burrowView, error) {
	b, err := loadBurrows(q, id)
	v := burrowView{Level: b.level, Lodgers: b.lodgers, Mats: matsFor("burrow", b.level), LodgeAt: lodgeAt}
	if v.Lodgers == nil {
		v.Lodgers = []int{}
	}
	if b.level < buildLevels {
		v.Cost = burrowCost[b.level]
	}
	v.Rate = rentRate * len(b.lodgers)
	v.Holds = v.Rate * int(holdFor/time.Hour)
	var at int64
	v.Held, at = b.held(now)
	if v.Rate > 0 && v.Held < v.Holds {
		v.NextIn = at + int64(3600/v.Rate) - now.Unix()
	}
	v.Chatted = b.rentDay == dayOf(now)
	if err != nil {
		return v, err
	}
	return v, q.QueryRow(`SELECT paint FROM ponds WHERE id = ?`, id).Scan(&v.Paint)
}
