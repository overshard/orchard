package main

import (
	"database/sql"
	"errors"
)

// Each buyer pays a pond at most sellRoom pebbles a New York day, about what
// a fair garden cooks into on an ordinary day. Jobs bring things in without
// end, so with no limit a frog at work would turn straight into pebbles.
const sellRoom = 150

const buyerStall = "stall"

var buyers = []string{shopBrindle, shopMoss, buyerStall}

var (
	errBeanFull  = errors.New("Big Bean has bought all the dishes he can today, and he buys again tomorrow")
	errMothFull  = errors.New("Moth has bought all the trinkets there's room for today, so try again tomorrow")
	errStallPaid = errors.New("the neighbours have paid all they will today, and the rest comes tomorrow")
)

// roomsOf is what's left of each buyer's day for a pond.
func roomsOf(q querier, id int64, day string) (map[string]int, error) {
	out := map[string]int{}
	for _, b := range buyers {
		out[b] = sellRoom
	}
	rows, err := q.Query(`SELECT buyer, paid FROM sold WHERE pond_id = ? AND day = ?`, id, day)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var n int
		if err := rows.Scan(&b, &n); err != nil {
			return out, err
		}
		out[b] = max(0, sellRoom-n)
	}
	return out, rows.Err()
}

func roomLeft(q querier, id int64, buyer, day string) (int, error) {
	var n int
	err := q.QueryRow(`SELECT paid FROM sold WHERE pond_id = ? AND buyer = ? AND day = ?`, id, buyer, day).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return sellRoom, nil
	}
	return max(0, sellRoom-n), err
}

// payOut pays as much of a price as the buyer has left today and says how
// much that was.
func payOut(tx *sql.Tx, p *pond, buyer string, price int) (int, error) {
	left, err := roomLeft(tx, p.ID, buyer, p.today)
	if err != nil {
		return 0, err
	}
	n := min(price, left)
	if n <= 0 {
		return 0, nil
	}
	if _, err := tx.Exec(`INSERT INTO sold (pond_id, buyer, day, paid) VALUES (?, ?, ?, ?)
		ON CONFLICT (pond_id, buyer) DO UPDATE SET
		paid = CASE WHEN day = excluded.day THEN paid + excluded.paid ELSE excluded.paid END,
		day = excluded.day`, p.ID, buyer, p.today, n); err != nil {
		return 0, err
	}
	p.Pebbles += n
	return n, nil
}
