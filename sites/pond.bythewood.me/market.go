package main

import (
	"database/sql"
	"errors"
	"math"
	"time"
)

// The pond market has a business for each of dash's market series. A share
// moves three times the real day's percent, priced in pebbles, and a day's
// close is kept here so the market carries on while dash is away.
type business struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Follows string `json:"follows"`
}

var businesses = []business{
	{"sp500", "Lilypad & Co", "the S&P 500"},
	{"dow", "Old Bullfrog Holdings", "the Dow"},
	{"nasdaq", "Ribbit Labs", "the Nasdaq"},
	{"russell", "Tadpole Ventures", "the Russell 2000"},
	{"gold", "Golden Fly Mines", "gold"},
	{"oil", "Bog Oil & Peat", "oil"},
	{"bitcoin", "Glowworm Coin", "bitcoin"},
	{"vix", "Storm Shelter Supply", "the VIX"},
}

const (
	marketStart = 100.0
	marketLever = 3.0
	// Two percent each way, so buying and selling straight back always loses.
	marketFee  = 0.02
	marketDays = 30
	// Glowworm Coin trades every day, the rest only in dash's regular session.
	alwaysOpen = "bitcoin"
)

var (
	errCap   = errors.New("that's more than the bank lets you hold")
	errFirm  = errors.New("there's no business by that name")
	errCount = errors.New("pick how many shares")
)

func businessOf(key string) (business, bool) {
	for _, b := range businesses {
		if b.Key == key {
			return b, true
		}
	}
	return business{}, false
}

// marketCap is how many pebbles a pond can have in shares at cost, which
// rises with everything built in the bank.
func marketCap(p *pond) int {
	return 300 + 150*(p.GravelLevel+p.GardenLevel+p.DyeLevel)
}

func buyCost(price float64, n int) int  { return int(math.Ceil(price * float64(n) * (1 + marketFee))) }
func sellPays(price float64, n int) int { return int(math.Floor(price * float64(n) * (1 - marketFee))) }

// closeBefore is a business's last kept close before day, or the launch
// price when it has none.
func closeBefore(q querier, key, day string) (float64, error) {
	var c float64
	err := q.QueryRow(`SELECT close FROM market_days WHERE key = ? AND day < ? ORDER BY day DESC LIMIT 1`, key, day).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return marketStart, nil
	}
	return c, err
}

// priceNow is the latest kept price, today's when today has traded.
func priceNow(q querier, key, day string) (float64, error) {
	var c float64
	err := q.QueryRow(`SELECT close FROM market_days WHERE key = ? AND day <= ? ORDER BY day DESC LIMIT 1`, key, day).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return marketStart, nil
	}
	return c, err
}

// lockMarket writes today's price for each business off the latest reading.
// The last write of the day is its close. Outside the regular session dash
// shows futures, which aren't the day's move, so those readings are skipped.
func (s *store) lockMarket() error {
	now := s.now()
	r := s.day.reading(now)
	if r == nil || len(r.Markets) == 0 {
		return nil
	}
	day := dayOf(now)
	return s.write(func(tx *sql.Tx) error {
		// Today's prices as they came, for the chart. Only today's are kept.
		if _, err := tx.Exec(`DELETE FROM market_ticks WHERE day < ?`, day); err != nil {
			return err
		}
		for _, b := range businesses {
			pct, ok := r.Markets[b.Key]
			if !ok || (r.Session != "regular" && b.Key != alwaysOpen) {
				continue
			}
			prev, err := closeBefore(tx, b.Key, day)
			if err != nil {
				return err
			}
			price := max(1, prev*(1+marketLever*pct/100))
			if _, err := tx.Exec(`INSERT INTO market_days (day, key, close) VALUES (?, ?, ?)
				ON CONFLICT (day, key) DO UPDATE SET close = excluded.close`, day, b.Key, price); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO market_ticks (key, at, day, price) VALUES (?, ?, ?, ?)`,
				b.Key, now.Unix(), day, price); err != nil {
				return err
			}
		}
		return nil
	})
}

type holding struct {
	count, cost int
}

func loadShares(q querier, id int64) (map[string]holding, error) {
	rows, err := q.Query(`SELECT key, count, cost FROM shares WHERE pond_id = ? AND count > 0`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]holding{}
	for rows.Next() {
		var k string
		var h holding
		if err := rows.Scan(&k, &h.count, &h.cost); err != nil {
			return nil, err
		}
		out[k] = h
	}
	return out, rows.Err()
}

func heldCost(sh map[string]holding) int {
	n := 0
	for _, h := range sh {
		n += h.cost
	}
	return n
}

func (s *store) buyShares(id int64, key string, n int) (*view, error) {
	if _, ok := businessOf(key); !ok {
		return nil, errFirm
	}
	if n < 1 || n > 100 {
		return nil, errCount
	}
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		price, err := priceNow(tx, key, dayOf(now))
		if err != nil {
			return err
		}
		cost := buyCost(price, n)
		if p.Pebbles < cost {
			return errPoor
		}
		sh, err := loadShares(tx, p.ID)
		if err != nil {
			return err
		}
		if heldCost(sh)+cost > marketCap(p) {
			return errCap
		}
		p.Pebbles -= cost
		_, err = tx.Exec(`INSERT INTO shares (pond_id, key, count, cost) VALUES (?, ?, ?, ?)
			ON CONFLICT (pond_id, key) DO UPDATE SET count = count + excluded.count, cost = cost + excluded.cost`,
			p.ID, key, n, cost)
		if err != nil {
			return err
		}
		return markTried(tx, p.ID, triedShare)
	})
}

// sellShares takes the cost basis down in proportion, so what's left keeps
// its average price.
func (s *store) sellShares(id int64, key string, n int) (*view, error) {
	if _, ok := businessOf(key); !ok {
		return nil, errFirm
	}
	if n < 1 {
		return nil, errCount
	}
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		var h holding
		err := tx.QueryRow(`SELECT count, cost FROM shares WHERE pond_id = ? AND key = ?`, p.ID, key).Scan(&h.count, &h.cost)
		if errors.Is(err, sql.ErrNoRows) || err == nil && h.count < n {
			return errNotHeld
		}
		if err != nil {
			return err
		}
		price, err := priceNow(tx, key, dayOf(now))
		if err != nil {
			return err
		}
		p.Pebbles += sellPays(price, n)
		left := h.cost - h.cost*n/h.count
		_, err = tx.Exec(`UPDATE shares SET count = count - ?, cost = ? WHERE pond_id = ? AND key = ?`, n, left, p.ID, key)
		return err
	})
}

// ---------------------------------------------------------------- the view

type firmView struct {
	business
	Price int `json:"price"`
	Prev  int `json:"prev"`
	Buy   int `json:"buy"`
	Sell  int `json:"sell"`
	Held  int `json:"held"`
	Cost  int `json:"cost"`
	Worth int `json:"worth"`
	// Days is each close before today, oldest first and opening on the launch
	// price while there are few, and Ticks is today's prices as they came.
	Days  []int `json:"days"`
	Ticks []int `json:"ticks"`
}

type marketView struct {
	Open  bool       `json:"open"`
	Cap   int        `json:"cap"`
	In    int        `json:"in"`
	Fee   int        `json:"fee"`
	Firms []firmView `json:"firms"`
	// Worth of everything held at today's price and at the last close.
	Worth     int `json:"worth"`
	WorthPrev int `json:"worth_prev"`
}

func marketOf(q querier, p *pond, r *reading, now time.Time) (marketView, error) {
	v := marketView{Cap: marketCap(p), Fee: int(marketFee * 100), Firms: []firmView{}}
	v.Open = r != nil && r.Session == "regular"
	day := dayOf(now)
	sh, err := loadShares(q, p.ID)
	if err != nil {
		return v, err
	}
	v.In = heldCost(sh)
	for _, b := range businesses {
		price, err := priceNow(q, b.Key, day)
		if err != nil {
			return v, err
		}
		prev, err := closeBefore(q, b.Key, day)
		if err != nil {
			return v, err
		}
		f := firmView{business: b, Price: int(math.Round(price)), Prev: int(math.Round(prev)),
			Buy: buyCost(price, 1), Sell: sellPays(price, 1), Days: []int{}, Ticks: []int{}}
		if h, ok := sh[b.Key]; ok {
			f.Held, f.Cost, f.Worth = h.count, h.cost, sellPays(price, h.count)
			v.Worth += f.Worth
			v.WorthPrev += sellPays(prev, h.count)
		}
		rows, err := q.Query(`SELECT close FROM (SELECT day, close FROM market_days WHERE key = ? AND day < ?
			ORDER BY day DESC LIMIT ?) ORDER BY day`, b.Key, day, marketDays)
		if err != nil {
			return v, err
		}
		for rows.Next() {
			var c float64
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return v, err
			}
			f.Days = append(f.Days, int(math.Round(c)))
		}
		rows.Close()
		if len(f.Days) < marketDays {
			f.Days = append([]int{int(marketStart)}, f.Days...)
		}
		rows, err = q.Query(`SELECT price FROM market_ticks WHERE key = ? AND day = ? ORDER BY at`, b.Key, day)
		if err != nil {
			return v, err
		}
		for rows.Next() {
			var c float64
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return v, err
			}
			f.Ticks = append(f.Ticks, int(math.Round(c)))
		}
		rows.Close()
		v.Firms = append(v.Firms, f)
	}
	return v, nil
}

// marketMood is the frog's word on the purse when holdings had a big day,
// a tenth or more either way.
func marketMood(m marketView) string {
	if m.WorthPrev < 50 {
		return ""
	}
	switch d := float64(m.Worth-m.WorthPrev) / float64(m.WorthPrev); {
	case d >= 0.1:
		return "Your shares had a very good day and it keeps checking the board to be sure."
	case d <= -0.1:
		return "Your shares had a bad day. It says they'll come back, and pats your hand."
	}
	return ""
}

// Each resident has a favourite business and says so in town when it moves
// a long way either way.
func residentFirm(i int) business { return businesses[i%len(businesses)] }

func firmTalk(f firmView, seed uint32) string {
	if f.Prev == 0 {
		return ""
	}
	brag := []string{"Says " + f.Name + " is having a wonderful day, and won't stop saying it.",
		"Bought more " + f.Name + " this morning and is very pleased with itself."}
	sulk := []string{"Is sulking about " + f.Name + ".",
		"Won't talk about " + f.Name + " today."}
	switch d := float64(f.Price-f.Prev) / float64(f.Prev); {
	case d >= 0.03:
		return brag[seed%2]
	case d <= -0.03:
		return sulk[seed%2]
	}
	return ""
}
