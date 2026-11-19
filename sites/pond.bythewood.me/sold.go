package main

import (
	"database/sql"
	"errors"
	"time"
)

// What the shops and the stall buy off a pond in a New York day pays in full
// up to taperFull, by half up to taperHalf and one in taperTail after that,
// since jobs bring things in without end.
const (
	taperFull = 150
	taperHalf = 450
	taperTail = 20
)

const (
	buyerStall  = "stall"
	buyerMarket = "market"
	// The market buys goods a lot at a time and pays out less than a shop.
	marketRoom = 100
)

var errMarketFull = errors.New("the market has bought all the goods it will from you today, and it buys again tomorrow")

// taperPaid is what a day's selling at face pays. A sale pays the step it
// makes on this, so many small sales come to the same as one big one.
func taperPaid(face int) int {
	full := min(face, taperFull)
	half := min(max(face-taperFull, 0), taperHalf-taperFull)
	return full + half/2 + max(face-taperHalf, 0)/taperTail
}

type taperView struct {
	// Sold is today's selling at face and Paid what it came to.
	Sold int `json:"sold"`
	Paid int `json:"paid"`
	Full int `json:"full"`
	Half int `json:"half"`
	Tail int `json:"tail"`
}

func taperOf(q querier, id int64, day string) (taperView, error) {
	v := taperView{Full: taperFull, Half: taperHalf, Tail: taperTail}
	err := q.QueryRow(`SELECT COALESCE(SUM(face), 0), COALESCE(SUM(paid), 0) FROM sold
		WHERE pond_id = ? AND day = ? AND buyer != ?`, id, day, buyerMarket).Scan(&v.Sold, &v.Paid)
	return v, err
}

func marketLeft(q querier, id int64, day string) (int, error) {
	var n int
	err := q.QueryRow(`SELECT paid FROM sold WHERE pond_id = ? AND buyer = ? AND day = ?`, id, buyerMarket, day).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return marketRoom, nil
	}
	return max(0, marketRoom-n), err
}

// payOut pays for something sold at a price and says how much landed. The
// market pays what's left of its own room, and every other buyer pays down
// the day's taper.
func payOut(tx *sql.Tx, p *pond, buyer string, price int) (int, error) {
	if price <= 0 {
		return 0, nil
	}
	var n int
	if buyer == buyerMarket {
		left, err := marketLeft(tx, p.ID, p.today)
		if err != nil {
			return 0, err
		}
		if n = min(price, left); n <= 0 {
			return 0, nil
		}
		price = n
	} else {
		t, err := taperOf(tx, p.ID, p.today)
		if err != nil {
			return 0, err
		}
		n = taperPaid(t.Sold+price) - taperPaid(t.Sold)
	}
	if _, err := tx.Exec(`INSERT INTO sold (pond_id, buyer, day, paid, face) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (pond_id, buyer) DO UPDATE SET
		paid = CASE WHEN day = excluded.day THEN paid + excluded.paid ELSE excluded.paid END,
		face = CASE WHEN day = excluded.day THEN face + excluded.face ELSE excluded.face END,
		day = excluded.day`, p.ID, buyer, p.today, n, price); err != nil {
		return 0, err
	}
	p.Pebbles += n
	return n, addFlow(tx, p, "sold", n)
}

// payWaiting pays what's sold on stalls and not yet paid out in full, once,
// when the face column first goes in, since those takings were owed whole.
func payWaiting(db *sql.DB, now time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE sold SET face = paid`); err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE ponds SET pebbles = pebbles + (SELECT SUM(price) FROM stall s
			WHERE s.pond_id = ponds.id AND s.sells_at > 0 AND s.sells_at <= ?1)
			WHERE id IN (SELECT pond_id FROM stall WHERE sells_at > 0 AND sells_at <= ?1)`,
		`DELETE FROM stall WHERE sells_at > 0 AND sells_at <= ?1`,
	} {
		if _, err := tx.Exec(q, now.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
