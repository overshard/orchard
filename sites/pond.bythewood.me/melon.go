package main

import (
	"database/sql"
	"errors"
	"time"
)

// Moonmelons come in on Sunday and are only bought then. They sell on the
// board all week and spoil once Saturday is over, so a melon held too long
// is worth nothing, and a pond can only buy a few a week.
const melonMax = 20

var (
	errMelonShut = errors.New("moonmelons are only bought on Sunday, when the cart comes in")
	errMelonMax  = errors.New("that's every moonmelon the cart will sell you this week")
)

func melonDay(day string) bool { return weekday(day) == time.Sunday }

// melonWeek is the Sunday a melon bought on day came in, the week it lasts.
func melonWeek(day string) string {
	return dayShift(day, -int(weekday(day)))
}

type melons struct {
	Week  string
	Count int
	Cost  int
}

func loadMelons(q querier, id int64) (melons, error) {
	var m melons
	err := q.QueryRow(`SELECT week, count, cost FROM melons WHERE pond_id = ?`, id).Scan(&m.Week, &m.Count, &m.Cost)
	if errors.Is(err, sql.ErrNoRows) {
		return melons{}, nil
	}
	return m, err
}

// spoilMelons throws out melons from a week gone and leaves a line for the
// away list saying so.
func spoilMelons(tx *sql.Tx, p *pond) (int, error) {
	m, err := loadMelons(tx, p.ID)
	if err != nil || m.Count == 0 || m.Week == melonWeek(p.today) {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM melons WHERE pond_id = ?`, p.ID); err != nil {
		return 0, err
	}
	line := "Your " + plural(m.Count, "moonmelon") + " spoiled on Saturday night and went on the compost"
	if m.Count == 1 {
		line = "Your moonmelon spoiled on Saturday night and went on the compost"
	}
	return m.Count, noteAway(tx, p.ID, p.at, "Market", line)
}

// melonsLeft is how many more melons a pond can buy today.
func melonsLeft(p *pond, m melons) (int, error) {
	if !melonDay(p.today) {
		return 0, errMelonShut
	}
	if m.Week != p.today {
		return melonMax, nil
	}
	return melonMax - m.Count, nil
}

func buyMelons(tx *sql.Tx, p *pond, m melons, n, cost int) error {
	if m.Week != p.today {
		m = melons{}
	}
	_, err := tx.Exec(`INSERT INTO melons (pond_id, week, count, cost) VALUES (?, ?, ?, ?)
		ON CONFLICT (pond_id) DO UPDATE SET week = excluded.week, count = excluded.count, cost = excluded.cost`,
		p.ID, p.today, m.Count+n, m.Cost+cost)
	return err
}

func sellMelons(tx *sql.Tx, p *pond, price float64, n int) error {
	m, err := loadMelons(tx, p.ID)
	if err != nil {
		return err
	}
	if m.Week != melonWeek(p.today) || m.Count < n {
		return errNotHeld
	}
	left := m.Cost - m.Cost*n/m.Count
	if _, err := tx.Exec(`UPDATE melons SET count = count - ?, cost = ? WHERE pond_id = ?`, n, left, p.ID); err != nil {
		return err
	}
	p.Pebbles += sellPays(price, n, melonFee)
	return nil
}

type melonView struct {
	// Open is whether melons can be bought today, and Left how many more.
	Open bool `json:"open"`
	Left int  `json:"left"`
	Max  int  `json:"max"`
	Held int  `json:"held"`
	// Days is how many days are left to sell, today counted.
	Days int `json:"days"`
}

func melonViewOf(m melons, day string) melonView {
	v := melonView{Open: melonDay(day), Max: melonMax, Held: m.Count, Days: 7 - int(weekday(day))}
	if v.Open {
		v.Left = melonMax
		if m.Week == day {
			v.Left -= m.Count
		}
	}
	return v
}
