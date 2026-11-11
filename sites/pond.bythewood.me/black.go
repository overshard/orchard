package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"
)

// A frog in a black hood sells a few things at the market's back door for
// Glowworm Coin. What a deal really is comes off an HMAC of the pepper, so
// the source can't read it ahead and the page learns it only once bought.
const blackDeals = 3

// Opening hours on the pond's clock, in minutes from midnight. The night one
// runs on past midnight and goes by the day it opened.
var blackHours = []struct {
	tag      string
	from, to int
}{
	{"dawn", 5 * 60, 6*60 + 30},
	{"night", 23 * 60, 26 * 60},
}

// What a deal turns out to be.
const (
	dealGreat = "great"
	dealFair  = "fair"
	dealScam  = "scam"
	dealFake  = "fake"
)

// Out of 100, and what each asks as a share of the thing's worth. A cheap
// one is as likely a fake as a steal.
var dealOdds = []struct {
	outcome string
	w       int
	lo, hi  float64
	// Misspelt is how often its name is spelt wrong on the sign, out of 100.
	misspelt int
	// The odds of each sort of patter, in the order of patter.
	talk [4]int
}{
	{dealGreat, 10, 0.4, 0.75, 15, [4]int{25, 15, 20, 40}},
	{dealFair, 40, 0.7, 1.4, 15, [4]int{20, 20, 20, 40}},
	{dealScam, 25, 4, 10, 25, [4]int{15, 20, 40, 25}},
	{dealFake, 25, 0.2, 0.75, 25, [4]int{20, 30, 20, 30}},
}

// What the hooded frog says over a deal. Shifty leans to a steal, pushy to a
// fake and smooth to a scam, and bored says nothing either way.
var patter = [4][]string{
	{"Fell off the back of a heron. Don't ask where the heron is.",
		"I need it gone before sunup, so it's cheap.",
		"Somebody's uncle left it behind and he isn't coming back for it.",
		"Keep your voice down, the owner's still out looking for it."},
	{"One hundred percent real. Ask anyone. Actually don't ask anyone.",
		"It says genuine right on it, look. I wrote that myself.",
		"Best price in the whole pond, and I'd know, I set it.",
		"Don't hold it up to the light, it's very real and the paint's still wet."},
	{"A collector's piece. Very rare, very special, very dear.",
		"There's only one of these in the whole world and you're looking at it.",
		"Honestly at this price you're robbing me.",
		"Royalty wore this once. Frog royalty, but still."},
	{"It's a thing. You want it or not?",
		"Same as you'd get in town, more or less.",
		"Take it or leave it, I'm not fussed.",
		"Coin only. No pebbles and no questions."},
}

var (
	errBlackShut = errors.New("the back door's shut, so come back when the lantern's lit")
	errDealt     = errors.New("you've had that deal already")
	errNoCoin    = errors.New("you don't have that much Glowworm Coin")
)

// blackWindow is the window open at now and when it shuts, or with open
// false the next one and when it opens.
func blackWindow(now time.Time) (key string, open bool, edge time.Time) {
	local := now.In(pondZone)
	var next time.Time
	nextKey := ""
	for d := -1; d <= 1; d++ {
		day := time.Date(local.Year(), local.Month(), local.Day()+d, 0, 0, 0, 0, pondZone)
		for _, h := range blackHours {
			from := time.Date(day.Year(), day.Month(), day.Day(), 0, h.from, 0, 0, pondZone)
			to := time.Date(day.Year(), day.Month(), day.Day(), 0, h.to, 0, 0, pondZone)
			k := day.Format("2006-01-02") + " " + h.tag
			if !now.Before(from) && now.Before(to) {
				return k, true, to
			}
			if from.After(now) && (next.IsZero() || from.Before(next)) {
				next, nextKey = from, k
			}
		}
	}
	return nextKey, false, next
}

// offer is one thing on the hooded frog's crate. Everything below price is the
// truth of it and never goes to the page before it's bought.
type offer struct {
	Slot int
	Kind string
	N    int
	// Name is as written on the sign, misspelt or not.
	Name   string
	Tier   int
	Coins  int
	Patter string
	worth  int
	// What it really is, and with a steal sometimes something better.
	outcome  string
	gotKind  string
	gotN     int
	misspelt bool
	talk     int
}

func dealRand(pepper []byte, window string, slot int) *rand.Rand {
	mac := hmac.New(sha256.New, pepper)
	fmt.Fprintf(mac, "black|%s|%d", window, slot)
	b := mac.Sum(nil)
	return rand.New(rand.NewPCG(binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:16])))
}

func pickW(r *rand.Rand, w []int) int {
	sum := 0
	for _, x := range w {
		sum += x
	}
	n := r.IntN(sum)
	for i, x := range w {
		if n < x {
			return i
		}
		n -= x
	}
	return 0
}

// The fish dishes the hooded frog gets hold of, by their two ingredients.
var blackDishes = [][2]int{{11, 11}, {1, 12}, {12, 12}, {11, 12}, {7, 12}}

// blackThing deals what's on the sign, its worth in pebbles and what a steal
// of it gives instead.
func blackThing(r *rand.Rand) (kind string, n, tier, worth int, better string, betterN int) {
	switch pickW(r, []int{25, 20, 15, 15, 10, 15}) {
	case 0:
		t := 1 + pickW(r, []int{45, 40, 15})
		pool := hatsByTier[t]
		h, up := pool[r.IntN(len(pool))], hatsByTier[t+1]
		return hatKind(h), 1, t, wrenSells[t], hatKind(up[r.IntN(len(up))]), 1
	case 1:
		t := pickW(r, []int{35, 35, 22, 8})
		pool, up := trinketsOfTier(t), trinketsOfTier(t+1)
		x := pool[r.IntN(len(pool))]
		return x.kind, 1, t, wrenSells[t], up[r.IntN(len(up))].kind, 1
	case 2:
		w := make([]int, len(mossRocks))
		for i, x := range mossRocks {
			w[i] = x.w
		}
		x := mossRocks[pickW(r, w)]
		t, _ := wizardTier(x.kind)
		return x.kind, 1, t, x.price, x.kind, 2
	case 3:
		var k string
		var price int
		switch r.IntN(3) {
		case 0:
			k, price = wheelDyes[r.IntN(len(wheelDyes))], dyePrice
		case 1:
			keys := sortedKeys(stamps)
			k, price = keys[r.IntN(len(keys))], stampPrice
		default:
			keys := sortedKeys(drops)
			k, price = keys[r.IntN(len(keys))], dropsPrice
		}
		return k, 1, benchTier[k], price, k, 2
	case 4:
		p := blackDishes[r.IntN(len(blackDishes))]
		d := dishOf(p[0], p[1])
		return d.kind(), 1, d.tier, 3 * dishWorth[d.tier], d.kind(), 2
	}
	var goods []asset
	for _, a := range assets {
		if a.Kind == kindGood {
			goods = append(goods, a)
		}
	}
	g := goods[r.IntN(len(goods))]
	lots := 2 + r.IntN(5)
	return g.Item, lots * lot, 0, buyCost(g.base, lots, goodFee), g.Item, 2 * lots * lot
}

func trinketsOfTier(t int) []trinket {
	var out []trinket
	for _, x := range trinkets {
		if x.tier == t && pets[x.kind] == "" {
			out = append(out, x)
		}
	}
	return out
}

// fakeOf is what a fake turns out to be: a leaf for a hat, a painted agate
// for a rock or a bottle, and one pebble wrapped up for the rest.
func fakeOf(kind string) string {
	switch {
	case strings.HasPrefix(kind, "hat_"):
		return hatKind(hat{Shape: 4})
	case isRock(kind), strings.HasPrefix(kind, "dye_"), strings.HasPrefix(kind, "stamp_"), strings.HasPrefix(kind, "drops_"):
		return "agate"
	}
	return "peb"
}

// misspell swaps two letters in the middle of the name's longest word.
func misspell(name string, r *rand.Rand) string {
	words := strings.Fields(name)
	at := 0
	for i, w := range words {
		if len(w) > len(words[at]) {
			at = i
		}
	}
	w := []byte(words[at])
	if len(w) < 4 {
		return name + "e"
	}
	i := 1 + r.IntN(len(w)-2)
	if i == len(w)-1 || w[i] == w[i+1] {
		i = 1
	}
	w[i], w[i+1] = w[i+1], w[i]
	words[at] = string(w)
	return strings.Join(words, " ")
}

// dealsOf is a window's deals, the same for every pond. coin is what a coin
// closed at before the window's day, so the price in coin holds all window.
func dealsOf(pepper []byte, window string, coin float64) []offer {
	out := make([]offer, 0, blackDeals)
	for slot := range blackDeals {
		r := dealRand(pepper, window, slot)
		w := make([]int, len(dealOdds))
		for i, o := range dealOdds {
			w[i] = o.w
		}
		o := dealOdds[pickW(r, w)]
		kind, n, tier, worth, better, betterN := blackThing(r)
		d := offer{Slot: slot, Kind: kind, N: n, Tier: tier, worth: worth, outcome: o.outcome, gotKind: kind, gotN: n}
		d.Name = thingName(kind)
		if n > 1 {
			d.Name = fmt.Sprintf("%d %s", n, strings.ToLower(d.Name))
		}
		if r.IntN(100) < o.misspelt {
			d.Name, d.misspelt = misspell(d.Name, r), true
		}
		d.talk = pickW(r, o.talk[:])
		d.Patter = patter[d.talk][r.IntN(len(patter[d.talk]))]
		ask := float64(worth) * (o.lo + (o.hi-o.lo)*r.Float64())
		d.Coins = max(1, int(math.Round(ask/max(1, coin))))
		switch o.outcome {
		case dealGreat:
			if r.IntN(5) == 0 {
				d.gotKind, d.gotN = better, betterN
			}
		case dealFake:
			d.gotKind, d.gotN = fakeOf(kind), 1
		}
		out = append(out, d)
	}
	return out
}

// worthIn is what a kind is worth to the hooded frog's way of thinking,
// which is what the shops ask for it.
func worthIn(kind string, n int) int {
	switch {
	case kind == "peb":
		return n
	case strings.HasPrefix(kind, "hat_"):
		h, _ := hatOfKind(kind)
		return n * wrenSells[h.tier()]
	}
	if t, ok := trinketOf(kind); ok {
		return n * wrenSells[t.tier]
	}
	if d, ok := dishOfKind(kind); ok {
		return n * 3 * dishWorth[d.tier]
	}
	for _, a := range assets {
		if a.Kind == kindGood && a.Item == kind {
			return buyCost(a.base, n/lot, goodFee)
		}
	}
	if p, ok := shopFloor(kind); ok {
		if strings.HasPrefix(kind, "dye_") {
			p = dyePrice
		}
		return n * p
	}
	return 0
}

func (s *store) windowDeals(q querier, window string) ([]offer, error) {
	day, _, _ := strings.Cut(window, " ")
	coin, _ := assetOf(coinKey)
	ref, err := closeBefore(q, coinKey, day, coin.base)
	if err != nil {
		return nil, err
	}
	return dealsOf(s.pepper, window, ref), nil
}

// ---------------------------------------------------------------- buying

type blackGot struct {
	Outcome string `json:"outcome"`
	Kind    string `json:"kind"`
	N       int    `json:"n"`
	Name    string `json:"name"`
	Hat     *hat   `json:"hat,omitempty"`
	Tier    int    `json:"tier"`
	// Worth is what the shops would ask for what came, in pebbles.
	Worth int `json:"worth"`
}

type blackBought struct {
	Deal offerView `json:"deal"`
	Pond *view     `json:"pond"`
}

func gotOf(outcome, kind string, n int) *blackGot {
	g := &blackGot{Outcome: outcome, Kind: kind, N: n, Name: thingName(kind), Worth: worthIn(kind, n)}
	if kind == "peb" {
		g.Name = "One pebble"
	}
	if h, ok := hatOfKind(kind); ok {
		g.Hat, g.Tier = &h, h.tier()
	} else {
		g.Tier, _ = wizardTier(kind)
	}
	return g
}

func (s *store) blackBuy(id int64, slot int) (*blackBought, error) {
	out := &blackBought{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		window, open, _ := blackWindow(now)
		if !open {
			return errBlackShut
		}
		deals, err := s.windowDeals(tx, window)
		if err != nil {
			return err
		}
		if slot < 0 || slot >= len(deals) {
			return errNotHeld
		}
		d := deals[slot]
		var had int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM black_buys WHERE pond_id = ? AND window = ? AND slot = ?`, p.ID, window, slot).Scan(&had); err != nil {
			return err
		}
		if had > 0 {
			return errDealt
		}
		if err := takeShares(tx, p.ID, coinKey, d.Coins); errors.Is(err, errNotHeld) {
			return errNoCoin
		} else if err != nil {
			return err
		}
		if err := giveDeal(tx, p, d.gotKind, d.gotN); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO black_buys (pond_id, window, slot, outcome, kind, n, coins, day) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, window, slot, d.outcome, d.gotKind, d.gotN, d.Coins, p.today); err != nil {
			return err
		}
		out.Deal = viewOffer(d, 0)
		out.Deal.Got = gotOf(d.outcome, d.gotKind, d.gotN)
		return nil
	})
	out.Pond = v
	return out, err
}

func giveDeal(tx *sql.Tx, p *pond, kind string, n int) error {
	switch {
	case kind == "peb":
		p.Pebbles += n
		return nil
	case strings.HasPrefix(kind, "hat_"):
		h, _ := hatOfKind(kind)
		for range n {
			if err := award(tx, p, &prize{Hat: &h}); err != nil {
				return err
			}
		}
		return nil
	case strings.HasPrefix(kind, "t_"):
		for range n {
			if err := award(tx, p, &prize{Item: kind}); err != nil {
				return err
			}
		}
		return nil
	}
	return addItem(tx, p.ID, kind, n)
}

// ---------------------------------------------------------------- the view

type offerView struct {
	Slot   int    `json:"slot"`
	Kind   string `json:"kind"`
	N      int    `json:"n"`
	Name   string `json:"name"`
	Hat    *hat   `json:"hat,omitempty"`
	Tier   int    `json:"tier"`
	Coins  int    `json:"coins"`
	Patter string `json:"patter"`
	// About is the price in pebbles at the coin's price now.
	About int       `json:"about"`
	Got   *blackGot `json:"got,omitempty"`
}

func viewOffer(d offer, coin float64) offerView {
	v := offerView{Slot: d.Slot, Kind: d.Kind, N: d.N, Name: d.Name, Tier: d.Tier, Coins: d.Coins, Patter: d.Patter,
		About: int(math.Round(float64(d.Coins) * coin))}
	if h, ok := hatOfKind(d.Kind); ok {
		v.Hat = &h
	}
	return v
}

type blackView struct {
	Open bool `json:"open"`
	// Seconds until the door shuts, or while it's shut until it opens.
	Edge  int64       `json:"edge"`
	Back  string      `json:"back"`
	Deals []offerView `json:"deals"`
	Coins int         `json:"coins"`
	// Hours is each window as the pond's clock has it.
	Hours [][2]string `json:"hours"`
	// What this pond has had at the back door, ever.
	Bought int `json:"bought"`
	Steals int `json:"steals"`
	Stung  int `json:"stung"`
}

func clockOf(min int) string {
	h, m := (min/60)%24, min%60
	s := "am"
	if h >= 12 {
		s = "pm"
	}
	h = (h+11)%12 + 1
	if m == 0 {
		return fmt.Sprintf("%d%s", h, s)
	}
	return fmt.Sprintf("%d:%02d%s", h, m, s)
}

func (s *store) blackOf(q querier, id int64, now time.Time) (blackView, error) {
	window, open, edge := blackWindow(now)
	v := blackView{Open: open, Edge: int64(edge.Sub(now) / time.Second), Deals: []offerView{}}
	for _, h := range blackHours {
		v.Hours = append(v.Hours, [2]string{clockOf(h.from), clockOf(h.to)})
	}
	if !open {
		v.Back = clockOf(edge.In(pondZone).Hour()*60 + edge.In(pondZone).Minute())
	}
	err := q.QueryRow(`SELECT COUNT(*), COALESCE(SUM(outcome = 'great'), 0), COALESCE(SUM(outcome IN ('scam', 'fake')), 0)
		FROM black_buys WHERE pond_id = ?`, id).Scan(&v.Bought, &v.Steals, &v.Stung)
	if err != nil {
		return v, err
	}
	err = q.QueryRow(`SELECT count FROM shares WHERE pond_id = ? AND key = ?`, id, coinKey).Scan(&v.Coins)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	if !open {
		return v, nil
	}
	deals, err := s.windowDeals(q, window)
	if err != nil {
		return v, err
	}
	coin, _ := assetOf(coinKey)
	price, err := priceNow(q, coinKey, dayOf(now), coin.base)
	if err != nil {
		return v, err
	}
	got := map[int]*blackGot{}
	rows, err := q.Query(`SELECT slot, outcome, kind, n FROM black_buys WHERE pond_id = ? AND window = ?`, id, window)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var slot, n int
		var outcome, kind string
		if err := rows.Scan(&slot, &outcome, &kind, &n); err != nil {
			return v, err
		}
		got[slot] = gotOf(outcome, kind, n)
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	for _, d := range deals {
		dv := viewOffer(d, price)
		dv.Got = got[d.Slot]
		v.Deals = append(v.Deals, dv)
	}
	return v, nil
}
