package main

import (
	"database/sql"
	"errors"
	"math"
	"time"
)

// The pond market trades in pebbles and moves off the game itself. Shares in
// the town's businesses follow what every pond did in that line of work,
// goods are things from the bag priced off how much came in, and the coin swings.
type asset struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Moves finishes "moves with" for a share, and for a good it's what comes in.
	Moves string `json:"moves"`
	// A good is bought and sold as lots of its item.
	Item string `json:"item,omitempty"`
	flow string
	base float64
}

const (
	kindShare = "share"
	kindGood  = "good"
	kindCoin  = "coin"
	coinKey   = "bitcoin"
)

// Keys are kept for good, since holdings and the history are kept by them.
// The first eight followed dash's markets once and carry on from there.
var assets = []asset{
	{"sp500", "Lilypad & Co", kindShare, "every line of work in the pond together", "", "", 100},
	{"dow", "Old Bullfrog Holdings", kindShare, "everything ponds sell", "", "sold", 100},
	{"nasdaq", "Ribbit Labs", kindShare, "games played", "", "played", 100},
	{"russell", "Tadpole Ventures", kindShare, "frogs fed and eggs laid", "", "fed", 100},
	{"gold", "Golden Fly Mines", kindShare, "finds turned up at work", "", "finds", 100},
	{"oil", "Bog Oil & Peat", kindShare, "dishes cooked", "", "cooked", 100},
	{"vix", "Storm Shelter Supply", kindShare, "things made at the craft bench", "", "job:craft", 100},
	{"guild", "Bug Catchers' Guild", kindShare, "bugs caught", "", "job:bugs", 40},
	{"reedcut", "The Reed Cutters", kindShare, "reeds cut", "", "job:reeds", 40},
	{"claypit", "The Clay Pits", kindShare, "mud dug", "", "job:mud", 40},
	{"gatherers", "Stick Gatherers", kindShare, "sticks gathered", "", "job:sticks", 40},
	{"divers", "Pebble Divers", kindShare, "dives for pebbles", "", "job:pebbles", 40},
	{"fishery", "The Fishery", kindShare, "fish landed", "", "g_fish", 40},
	{"gardens", "The Gardeners", kindShare, "work in the beds", "", "job:garden", 40},
	{"tackle", "Sedge's Tackle", kindShare, "bait sold at Sedge's", "", "shop:sedge", 40},
	{"frames", "Marsh's Frames", kindShare, "frames sold at Marsh's", "", "shop:marsh", 40},
	{"g_bugs", "Bugs", kindGood, "bugs", "cricket", "g_bugs", 75},
	{"g_fish", "Fish", kindGood, "fish", "smallfry", "g_fish", 80},
	{"g_crops", "Crops", kindGood, "crops", "duckweed", "g_crops", 75},
	{"g_sticks", "Sticks", kindGood, "sticks", "sticks", "g_sticks", 45},
	{"g_reeds", "Reeds", kindGood, "reeds", "reeds", "g_reeds", 45},
	{"g_clay", "Clay", kindGood, "clay", "clay", "g_clay", 45},
	{coinKey, "Glowworm Coin", kindCoin, "nothing but itself", "", "", 100},
}

// What each job brings in counts toward its good's supply.
var jobGood = map[string]string{"bugs": "g_bugs", "garden": "g_crops", "sticks": "g_sticks", "reeds": "g_reeds", "mud": "g_clay"}

const (
	marketDays = 30
	// A good is priced and traded by the lot.
	lot = 10
	// Each way, so a trade straight back always loses.
	shareFee = 0.02
	goodFee  = 0.05
	coinFee  = 0.03
	// What a share pays a day, as a share of its price, on a usual day.
	divYield = 0.003
	// Dividends wait this long at most for a pond that's away.
	divBack = 30
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
	case kindCoin:
		return coinFee
	}
	return shareFee
}

// marketCap is how many pebbles a pond can have in shares and coin at cost.
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

// lockMarket closes the last day each asset traded and writes today's price
// so far. A row with no open came from the real markets, so its price is
// where the day carries on from and it's never worked out again.
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
		for _, a := range assets {
			var last string
			var open float64
			err := tx.QueryRow(`SELECT day, open FROM market_days WHERE key = ? AND day < ? ORDER BY day DESC LIMIT 1`, a.Key, day).Scan(&last, &open)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && open > 0 && last >= dayShift(day, -flowWeek) {
				c, div := priceAt(a, hmacDraw(s.pepper, last, a.Key), book, last, open, now, true)
				if _, err := tx.Exec(`UPDATE market_days SET close = ?, div = ? WHERE key = ? AND day = ?`, c, div, a.Key, last); err != nil {
					return err
				}
			}
			var close float64
			err = tx.QueryRow(`SELECT open, close FROM market_days WHERE key = ? AND day = ?`, a.Key, day).Scan(&open, &close)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				if open, err = closeBefore(tx, a.Key, day, a.base); err != nil {
					return err
				}
			case err != nil:
				return err
			case open == 0:
				// Opened where today's walk so far lands on the real price.
				p, _ := priceAt(a, hmacDraw(s.pepper, day, a.Key), book, day, close, now, false)
				open = close * close / p
			}
			price, div := priceAt(a, hmacDraw(s.pepper, day, a.Key), book, day, open, now, false)
			if _, err := tx.Exec(`INSERT INTO market_days (day, key, open, close, div) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (key, day) DO UPDATE SET open = excluded.open, close = excluded.close, div = excluded.div`,
				day, a.Key, open, price, div); err != nil {
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
// left keeps its average price. The black market spends coin through it.
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

// ---------------------------------------------------------------- dividends

// dividendsDue is what the pond's shares have earned at every close since it
// was last paid, plus the part of a pebble left over then.
func dividendsDue(q querier, id int64, today string) (float64, string, error) {
	var since string
	var carry float64
	if err := q.QueryRow(`SELECT div_day, div_carry FROM ponds WHERE id = ?`, id).Scan(&since, &carry); err != nil {
		return 0, "", err
	}
	if since == "" || since >= today {
		return carry, since, nil
	}
	since = max(since, dayShift(today, -divBack))
	var due float64
	err := q.QueryRow(`SELECT COALESCE(SUM(s.count * d.div), 0) FROM shares s JOIN market_days d ON d.key = s.key
		WHERE s.pond_id = ? AND s.count > 0 AND d.day >= ? AND d.day < ?`, id, since, today).Scan(&due)
	return carry + due, since, err
}

// payDividends pays whole pebbles and keeps the rest for next time. Every
// trade pays them first, so shares bought today never earn for days gone.
func payDividends(tx *sql.Tx, p *pond) (int, error) {
	due, since, err := dividendsDue(tx, p.ID, p.today)
	if err != nil {
		return 0, err
	}
	n := 0
	if since != "" {
		n = int(due)
		p.Pebbles += n
	}
	_, err = tx.Exec(`UPDATE ponds SET div_day = ?, div_carry = ? WHERE id = ?`, p.today, due-float64(n), p.ID)
	return n, err
}

// collectDividends is the collect key on the board and in the Next strip.
func (s *store) collectDividends(id int64) (*paidOut, error) {
	out := &paidOut{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		n, err := payDividends(tx, p)
		out.Pebbles = n
		return err
	})
	out.Pond = v
	return out, err
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
		if _, err := payDividends(tx, p); err != nil {
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
			return addItem(tx, p.ID, a.Item, n*lot)
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
		rank, err := rankTop(tx, p.ID)
		if err != nil {
			return err
		}
		if heldCost(sh)+cost > marketCap(p, rank) {
			return errCap
		}
		p.Pebbles -= cost
		if _, err = tx.Exec(`INSERT INTO shares (pond_id, key, count, cost) VALUES (?, ?, ?, ?)
			ON CONFLICT (pond_id, key) DO UPDATE SET count = count + excluded.count, cost = cost + excluded.cost`,
			p.ID, key, n, cost); err != nil {
			return err
		}
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
	left, err := roomLeft(tx, p.ID, buyerMarket, p.today)
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
	// Held is shares or coins, or for a good how many of its item are in the bag.
	Held  int `json:"held"`
	Cost  int `json:"cost"`
	Worth int `json:"worth"`
	// Div is what one share paid at the last close.
	Div float64 `json:"div"`
	// Days is each close before today, oldest first and opening on the launch
	// price while there are few, and Ticks is today's prices as they came.
	Days  []int `json:"days"`
	Ticks []int `json:"ticks"`
}

type marketView struct {
	Cap  int `json:"cap"`
	In   int `json:"in"`
	Lot  int `json:"lot"`
	Fees struct {
		Share int `json:"share"`
		Good  int `json:"good"`
		Coin  int `json:"coin"`
	} `json:"fees"`
	// Room is what the market will still pay today for goods.
	Room int `json:"room"`
	// Due is whole pebbles of dividends waiting, and Daily what the shares
	// held now paid at the last close.
	Due   int        `json:"due"`
	Daily float64    `json:"daily"`
	Firms []firmView `json:"firms"`
	// Worth of the shares and coin held at today's price and at the last close.
	Worth     int `json:"worth"`
	WorthPrev int `json:"worth_prev"`
}

func marketOf(q querier, p *pond, now time.Time) (marketView, error) {
	day := dayOf(now)
	rank, err := rankTop(q, p.ID)
	if err != nil {
		return marketView{}, err
	}
	v := marketView{Cap: marketCap(p, rank), Lot: lot, Firms: []firmView{}}
	v.Fees.Share, v.Fees.Good, v.Fees.Coin = int(shareFee*100), int(goodFee*100), int(coinFee*100)
	if v.Room, err = roomLeft(q, p.ID, buyerMarket, day); err != nil {
		return v, err
	}
	due, since, err := dividendsDue(q, p.ID, day)
	if err != nil {
		return v, err
	}
	if since != "" {
		v.Due = int(due)
	}
	sh, err := loadShares(q, p.ID)
	if err != nil {
		return v, err
	}
	v.In = heldCost(sh)
	held, err := heldItems(q, p.ID)
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
		fee := feeOf(a.Kind)
		f := firmView{asset: a, Price: int(math.Round(price)), Prev: int(math.Round(prev)),
			Buy: buyCost(price, 1, fee), Sell: sellPays(price, 1, fee), Days: []int{}, Ticks: []int{}}
		if a.Kind == kindShare {
			err := q.QueryRow(`SELECT div FROM market_days WHERE key = ? AND day < ? ORDER BY day DESC LIMIT 1`, a.Key, day).Scan(&f.Div)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return v, err
			}
			f.Div = math.Round(f.Div*100) / 100
		}
		if a.Kind == kindGood {
			f.Held = held[a.Item]
		} else if h, ok := sh[a.Key]; ok {
			f.Held, f.Cost, f.Worth = h.count, h.cost, sellPays(price, h.count, fee)
			v.Worth += f.Worth
			v.WorthPrev += sellPays(prev, h.count, fee)
			v.Daily += f.Div * float64(h.count)
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
	v.Daily = math.Round(v.Daily*10) / 10
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
func residentFirm(i int) asset {
	var shares []asset
	for _, a := range assets {
		if a.Kind == kindShare {
			shares = append(shares, a)
		}
	}
	return shares[i%len(shares)]
}

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
