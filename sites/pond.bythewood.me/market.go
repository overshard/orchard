package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// The night market trades in pebbles and moves off the game itself. Goods are
// things from the bag priced off how much came in, what the residents buy and
// sell and the day's weather, the coin swings, and the moonmelon lasts a week.
type asset struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Moves is what comes in for a good.
	Moves string `json:"moves"`
	// A good is bought and sold as lots of its item.
	Item string `json:"item,omitempty"`
	flow string
	base float64
}

const (
	kindGood  = "good"
	kindCoin  = "coin"
	kindMelon = "melon"
	coinKey   = "bitcoin"
	melonKey  = "melon"
)

// Keys are kept for good, since the history is kept by them.
var assets = []asset{
	{"g_reeds", "Reeds", kindGood, "reeds", "reeds", "g_reeds", 45},
	{"g_clay", "Clay", kindGood, "clay", "clay", "g_clay", 45},
	{"g_fish", "Fish", kindGood, "fish", "smallfry", "g_fish", 80},
	{"g_sticks", "Sticks", kindGood, "sticks", "sticks", "g_sticks", 45},
	{coinKey, "Glow coin", kindCoin, "", "", "", 100},
	{melonKey, "Moonmelon", kindMelon, "", "", "", 30},
}

// What each job brings in counts toward its good's supply.
var jobGood = map[string]string{"bugs": "g_bugs", "garden": "g_crops", "sticks": "g_sticks", "reeds": "g_reeds", "mud": "g_clay"}

const (
	marketDays = 30
	// A good is priced and traded by the lot.
	lot = 10
	// Each way, so a trade straight back always loses.
	goodFee  = 0.05
	coinFee  = 0.03
	melonFee = 0.05
)

var (
	errCap   = errors.New("that's more than the bank lets you hold")
	errFirm  = errors.New("there's nothing by that name on the board")
	errCount = errors.New("pick how many")
	errNoLot = errors.New("a lot is ten, and you don't have that many")
)

func assetOf(key string) (asset, bool) {
	for _, a := range assets {
		if a.Key == key {
			return a, true
		}
	}
	return asset{}, false
}

func feeOf(kind string) float64 {
	switch kind {
	case kindGood:
		return goodFee
	case kindMelon:
		return melonFee
	}
	return coinFee
}

// marketCap is how many pebbles a pond can have in coin and melons at cost.
// It grows with the bank and with each rung of frog power.
func marketCap(p *pond, rank int) int {
	return 500 + 200*(p.GravelLevel+p.GardenLevel+p.DyeLevel) + 500*rank
}

func buyCost(price float64, n int, fee float64) int {
	return int(math.Ceil(price * float64(n) * (1 + fee)))
}

func sellPays(price float64, n int, fee float64) int {
	return int(math.Floor(price * float64(n) * (1 - fee)))
}

// closeBefore is an asset's last close before day, or base when it has none.
func closeBefore(q querier, key, day string, base float64) (float64, error) {
	var c float64
	err := q.QueryRow(`SELECT close FROM market_days WHERE key = ? AND day < ? ORDER BY day DESC LIMIT 1`, key, day).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return base, nil
	}
	return c, err
}

// priceNow is the latest kept price, today's once the market has run today.
func priceNow(q querier, key, day string, base float64) (float64, error) {
	var c float64
	err := q.QueryRow(`SELECT close FROM market_days WHERE key = ? AND day <= ? ORDER BY day DESC LIMIT 1`, key, day).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return base, nil
	}
	return c, err
}

// pushesOf is the push and its reason kept for each asset on a day.
func pushesOf(q querier, day string) (map[string]push, error) {
	rows, err := q.Query(`SELECT key, push, why FROM market_days WHERE day = ?`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]push{}
	for rows.Next() {
		var k string
		var p push
		if err := rows.Scan(&k, &p.by, &p.why); err != nil {
			return nil, err
		}
		out[k] = p
	}
	return out, rows.Err()
}

// lockMarket closes the last day each asset traded and writes today's price
// so far. A day's push is kept once it has a reason, so the weather turning
// later in the day doesn't move the morning.
func (s *store) lockMarket() error {
	now := s.now()
	day := dayOf(now)
	return s.write(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM market_ticks WHERE day < ?`, day); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM market_flow WHERE day < ?`, dayShift(day, -flowKeep)); err != nil {
			return err
		}
		book, err := loadFlows(tx, day)
		if err != nil {
			return err
		}
		fresh := pushesOn(s.day.reading(now), day, book)
		today, err := pushesOf(tx, day)
		if err != nil {
			return err
		}
		for k, p := range fresh {
			if today[k].why == "" {
				today[k] = p
			}
		}
		trades := folkTradesOn(s.pepper, day, today)
		for _, a := range assets {
			var last string
			var open float64
			err := tx.QueryRow(`SELECT day, open FROM market_days WHERE key = ? AND day < ? ORDER BY day DESC LIMIT 1`, a.Key, day).Scan(&last, &open)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && open > 0 && last >= dayShift(day, -flowWeek) {
				then, err := pushesOf(tx, last)
				if err != nil {
					return err
				}
				m := dayMove{push: then[a.Key].by, folk: folkImpact(folkTradesOn(s.pepper, last, then), a.Key, 24*60)}
				c := priceAt(a, hmacDraw(s.pepper, last, a.Key), book, last, open, now, true, m)
				if _, err := tx.Exec(`UPDATE market_days SET close = ? WHERE key = ? AND day = ?`, c, a.Key, last); err != nil {
					return err
				}
			}
			var close float64
			err = tx.QueryRow(`SELECT open, close FROM market_days WHERE key = ? AND day = ?`, a.Key, day).Scan(&open, &close)
			p := today[a.Key]
			switch {
			case errors.Is(err, sql.ErrNoRows):
				if open, err = closeBefore(tx, a.Key, day, a.base); err != nil {
					return err
				}
				if a.Kind == kindMelon && melonDay(day) {
					open = a.base
				}
			case err != nil:
				return err
			case open == 0:
				// Opened where today's walk so far lands on the real price.
				q := priceAt(a, hmacDraw(s.pepper, day, a.Key), book, day, close, now, false, dayMove{})
				open = close * close / q
			}
			m := dayMove{push: p.by, folk: folkImpact(trades, a.Key, minuteOf(now))}
			price := priceAt(a, hmacDraw(s.pepper, day, a.Key), book, day, open, now, false, m)
			if _, err := tx.Exec(`INSERT INTO market_days (day, key, open, close, push, why) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (key, day) DO UPDATE SET open = excluded.open, close = excluded.close,
				push = CASE WHEN why = '' THEN excluded.push ELSE push END, why = CASE WHEN why = '' THEN excluded.why ELSE why END`,
				day, a.Key, open, price, p.by, p.why); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO market_ticks (key, at, day, price) VALUES (?, ?, ?, ?)`,
				a.Key, now.Unix(), day, price); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------------------------------------------------------------- holdings

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

// takeShares sells n off a holding and takes its cost down in step, so what's
// left keeps its average price. The back door spends coin through it.
func takeShares(tx *sql.Tx, id int64, key string, n int) error {
	var h holding
	err := tx.QueryRow(`SELECT count, cost FROM shares WHERE pond_id = ? AND key = ?`, id, key).Scan(&h.count, &h.cost)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (h.count < n || n < 1) {
		return errNotHeld
	}
	if err != nil {
		return err
	}
	left := h.cost - h.cost*n/h.count
	_, err = tx.Exec(`UPDATE shares SET count = count - ?, cost = ? WHERE pond_id = ? AND key = ?`, n, left, id, key)
	return err
}

// ---------------------------------------------------------------- trading

func (s *store) trade(id int64, key string, n int, sell bool) (*view, error) {
	a, ok := assetOf(key)
	if !ok {
		return nil, errFirm
	}
	if n < 1 || n > 100 {
		return nil, errCount
	}
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if _, err := spoilMelons(tx, p); err != nil {
			return err
		}
		price, err := priceNow(tx, key, p.today, a.base)
		if err != nil {
			return err
		}
		fee := feeOf(a.Kind)
		switch {
		case a.Kind == kindGood && sell:
			return sellGood(tx, p, a, price, n)
		case a.Kind == kindGood:
			cost := buyCost(price, n, fee)
			if p.Pebbles < cost {
				return errPoor
			}
			p.Pebbles -= cost
			if err := addItem(tx, p.ID, a.Item, n*lot); err != nil {
				return err
			}
			return markTried(tx, p.ID, triedShare)
		case a.Kind == kindMelon && sell:
			return sellMelons(tx, p, price, n)
		case sell:
			if err := takeShares(tx, p.ID, key, n); err != nil {
				return err
			}
			p.Pebbles += sellPays(price, n, fee)
			return nil
		}
		cost := buyCost(price, n, fee)
		if p.Pebbles < cost {
			return errPoor
		}
		sh, err := loadShares(tx, p.ID)
		if err != nil {
			return err
		}
		mel, err := loadMelons(tx, p.ID)
		if err != nil {
			return err
		}
		if a.Kind == kindMelon {
			left, err := melonsLeft(p, mel)
			if err != nil {
				return err
			}
			if n > left {
				return errMelonMax
			}
		}
		rank, err := rankTop(tx, p.ID)
		if err != nil {
			return err
		}
		if heldCost(sh)+mel.Cost+cost > marketCap(p, rank) {
			return errCap
		}
		if a.Kind == kindMelon {
			if err := buyMelons(tx, p, mel, n, cost); err != nil {
				return err
			}
		} else if _, err = tx.Exec(`INSERT INTO shares (pond_id, key, count, cost) VALUES (?, ?, ?, ?)
			ON CONFLICT (pond_id, key) DO UPDATE SET count = count + excluded.count, cost = cost + excluded.cost`,
			p.ID, key, n, cost); err != nil {
			return err
		}
		p.Pebbles -= cost
		return markTried(tx, p.ID, triedShare)
	})
}

// sellGood takes lots out of the bag. Jobs bring goods in without end, so
// the market pays through its own room a day like any other buyer.
func sellGood(tx *sql.Tx, p *pond, a asset, price float64, n int) error {
	var have int
	err := tx.QueryRow(`SELECT count FROM items WHERE pond_id = ? AND kind = ?`, p.ID, a.Item).Scan(&have)
	if errors.Is(err, sql.ErrNoRows) || err == nil && have < n*lot {
		return errNoLot
	}
	if err != nil {
		return err
	}
	pays := sellPays(price, n, goodFee)
	left, err := marketLeft(tx, p.ID, p.today)
	if err != nil {
		return err
	}
	if pays > left {
		return errMarketFull
	}
	if err := takeItem(tx, p.ID, a.Item, n*lot); err != nil {
		return err
	}
	_, err = payOut(tx, p, buyerMarket, pays)
	return err
}

// ---------------------------------------------------------------- the view

type firmView struct {
	asset
	Price int `json:"price"`
	Prev  int `json:"prev"`
	Buy   int `json:"buy"`
	Sell  int `json:"sell"`
	// Held is coins or melons, or for a good how many of its item are in the bag.
	Held  int `json:"held"`
	Cost  int `json:"cost"`
	Worth int `json:"worth"`
	// Why is the one line under its name on the board.
	Why string `json:"why"`
	// Days is each close before today, oldest first and opening on the launch
	// price while there are few, and Ticks is today's prices as they came.
	Days        []int `json:"days"`
	Ticks       []int `json:"ticks"`
	price, prev float64
}

type marketView struct {
	Cap  int `json:"cap"`
	In   int `json:"in"`
	Lot  int `json:"lot"`
	Fees struct {
		Good  int `json:"good"`
		Coin  int `json:"coin"`
		Melon int `json:"melon"`
	} `json:"fees"`
	// Room is what the market will still pay today for goods.
	Room  int        `json:"room"`
	Firms []firmView `json:"firms"`
	// Worth of the coin and melons held at today's price and at the last close.
	Worth     int `json:"worth"`
	WorthPrev int `json:"worth_prev"`
	// News is the story in today's paper that moves a good, if there is one.
	News  *news     `json:"news,omitempty"`
	Melon melonView `json:"melon"`
	Tape  []tapeRow `json:"tape"`
}

func marketOf(q querier, p *pond, now time.Time, pepper []byte) (marketView, error) {
	day := dayOf(now)
	rank, err := rankTop(q, p.ID)
	if err != nil {
		return marketView{}, err
	}
	v := marketView{Cap: marketCap(p, rank), Lot: lot, Firms: []firmView{}}
	if v.News, err = newsFor(q, day); err != nil {
		return v, err
	}
	v.Fees.Good, v.Fees.Coin, v.Fees.Melon = int(goodFee*100), int(coinFee*100), int(melonFee*100)
	if v.Room, err = marketLeft(q, p.ID, day); err != nil {
		return v, err
	}
	sh, err := loadShares(q, p.ID)
	if err != nil {
		return v, err
	}
	mel, err := loadMelons(q, p.ID)
	if err != nil {
		return v, err
	}
	if mel.Week != melonWeek(day) {
		mel = melons{}
	}
	v.In = heldCost(sh) + mel.Cost
	held, err := heldItems(q, p.ID)
	if err != nil {
		return v, err
	}
	pushes, err := pushesOf(q, day)
	if err != nil {
		return v, err
	}
	for _, a := range assets {
		price, err := priceNow(q, a.Key, day, a.base)
		if err != nil {
			return v, err
		}
		prev, err := closeBefore(q, a.Key, day, a.base)
		if err != nil {
			return v, err
		}
		if a.Kind == kindMelon && melonDay(day) {
			prev = a.base
		}
		fee := feeOf(a.Kind)
		f := firmView{asset: a, Price: int(math.Round(price)), Prev: int(math.Round(prev)), price: price, prev: prev,
			Buy: buyCost(price, 1, fee), Sell: sellPays(price, 1, fee), Why: whyOf(a, pushes[a.Key], day), Days: []int{}, Ticks: []int{}}
		h := sh[a.Key]
		switch a.Kind {
		case kindGood:
			f.Held = held[a.Item]
		case kindMelon:
			h = holding{mel.Count, mel.Cost}
		}
		if a.Kind != kindGood && h.count > 0 {
			f.Held, f.Cost, f.Worth = h.count, h.cost, sellPays(price, h.count, fee)
			v.Worth += f.Worth
			v.WorthPrev += sellPays(prev, h.count, fee)
		}
		if f.Days, err = closes(q, a.Key, day); err != nil {
			return v, err
		}
		if len(f.Days) < marketDays {
			f.Days = append([]int{int(math.Round(a.base))}, f.Days...)
		}
		if f.Ticks, err = ticks(q, a.Key, day); err != nil {
			return v, err
		}
		v.Firms = append(v.Firms, f)
	}
	v.Melon = melonViewOf(mel, day)
	v.Tape = tapeOf(pepper, day, pushes, now)
	return v, nil
}

func closes(q querier, key, day string) ([]int, error) {
	return roundedRows(q, `SELECT close FROM (SELECT day, close FROM market_days WHERE key = ? AND day < ?
		ORDER BY day DESC LIMIT ?) ORDER BY day`, key, day, marketDays)
}

func ticks(q querier, key, day string) ([]int, error) {
	return roundedRows(q, `SELECT price FROM market_ticks WHERE key = ? AND day = ? ORDER BY at`, key, day)
}

func roundedRows(q querier, query string, args ...any) ([]int, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var c float64
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, int(math.Round(c)))
	}
	return out, rows.Err()
}

// marketMood is the frog's word on the purse when what it holds had a big
// day, a tenth or more either way.
func marketMood(m marketView) string {
	if m.WorthPrev < 50 {
		return ""
	}
	switch d := float64(m.Worth-m.WorthPrev) / float64(m.WorthPrev); {
	case d >= 0.1:
		return "What you hold on the board had a very good day and it keeps checking to be sure."
	case d <= -0.1:
		return "What you hold on the board had a bad day. It says it'll come back, and pats your hand."
	}
	return ""
}

// firmTalk is what a resident says in town about the thing it trades when
// that's moved a long way either way.
func firmTalk(f firmView, seed uint32) string {
	if f.Prev == 0 {
		return ""
	}
	what := storyName(f.asset)
	brag := []string{"Says " + what + " is the thing to hold this week, and won't stop saying it.",
		"Bought " + what + " this morning and is very pleased with itself."}
	sulk := []string{"Is sulking about the price of " + what + ".",
		"Won't talk about " + what + " today."}
	switch d := float64(f.Price-f.Prev) / float64(f.Prev); {
	case d >= 0.03:
		return brag[seed%2]
	case d <= -0.03:
		return sulk[seed%2]
	}
	return ""
}

// storyName is an asset's name in the middle of a sentence.
func storyName(a asset) string {
	if a.Kind == kindMelon {
		return "moonmelons"
	}
	return strings.ToLower(a.Name)
}

// ---------------------------------------------------------------- shares go

// retiredShares are the businesses the board carried before it sold goods,
// with the price each launched at.
var retiredShares = map[string]float64{
	"sp500": 100, "dow": 100, "nasdaq": 100, "russell": 100, "gold": 100, "oil": 100, "vix": 100,
	"guild": 40, "reedcut": 40, "claypit": 40, "gatherers": 40, "divers": 40, "fishery": 40, "gardens": 40,
	"tackle": 40, "frames": 40,
}

// divBack is how many days of dividends a pond away the longest was owed.
const divBack = 30

// payShares buys back every share at its last price with no fee and pays
// the dividends owed on them, once, in one transaction.
func payShares(db *sql.DB, now time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM migrations WHERE name = 'shares paid'`).Scan(&done); err != nil || done > 0 {
		return err
	}
	today := dayOf(now)
	type owed struct {
		shares int
		worth  float64
	}
	by := map[int64]*owed{}
	var order []int64
	rows, err := tx.Query(`SELECT pond_id, key, count FROM shares WHERE count > 0 ORDER BY pond_id`)
	if err != nil {
		return err
	}
	type row struct {
		id  int64
		key string
		n   int
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.key, &r.n); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, r := range all {
		base, ok := retiredShares[r.key]
		if !ok {
			continue
		}
		price, err := priceNow(tx, r.key, today, base)
		if err != nil {
			return err
		}
		if by[r.id] == nil {
			by[r.id] = &owed{}
			order = append(order, r.id)
		}
		by[r.id].shares += r.n
		by[r.id].worth += price * float64(r.n)
	}
	for _, id := range order {
		o := by[id]
		var since string
		var carry float64
		if err := tx.QueryRow(`SELECT div_day, div_carry FROM ponds WHERE id = ?`, id).Scan(&since, &carry); err != nil {
			return err
		}
		due := carry
		if since != "" && since < today {
			var d float64
			if err := tx.QueryRow(`SELECT COALESCE(SUM(s.count * d.div), 0) FROM shares s JOIN market_days d ON d.key = s.key
				WHERE s.pond_id = ? AND s.count > 0 AND d.day >= ? AND d.day < ?`, id, max(since, dayShift(today, -divBack)), today).Scan(&d); err != nil {
				return err
			}
			due += d
		}
		worth, div := int(math.Ceil(o.worth-1e-9)), int(math.Ceil(due-1e-9))
		if _, err := tx.Exec(`UPDATE ponds SET pebbles = pebbles + ?, div_day = ?, div_carry = 0, div_total = div_total + ? WHERE id = ?`,
			worth+div, today, div, id); err != nil {
			return err
		}
		line := fmt.Sprintf("Shares came off the board, so your %s came to %s, paid in full with no fee", plural(o.shares, "share"), plural(worth, "pebble"))
		if div > 0 {
			line += fmt.Sprintf(", and %s more for the dividends they were owed", plural(div, "pebble"))
		}
		if err := noteAway(tx, id, now, "Market", line); err != nil {
			return err
		}
	}
	for key := range retiredShares {
		if _, err := tx.Exec(`DELETE FROM shares WHERE key = ?`, key); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO migrations (name, at) VALUES ('shares paid', ?)`, now.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
