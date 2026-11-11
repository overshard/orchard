package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// A price walks a step every slot of the day off an HMAC of the pepper, so
// nothing in this repository says where it goes next. Each step is worth
// what it cost on average, which keeps a price from being a sure thing.
const (
	slotLen  = 10 * time.Minute
	daySlots = 144
	// Flow is compared with the week before, and kept a little longer.
	flowWeek = 7
	flowKeep = 9
	// Added to both sides of a flow's ratio, so a quiet market barely moves.
	flowDamp = 10.0
)

// tune is how a sort of asset moves. Sigma is a day's random walk, lean how
// much of the way back to its base it heads in a day, and beta how far a day
// of twice the usual flow moves it. A day never moves more than cap.
type tune struct {
	sigma, lean, beta, cap float64
	jumps                  bool
}

var tunes = map[string]tune{
	kindShare: {sigma: 0.01, lean: 0.01, beta: 0.03, cap: 0.05},
	// Goods lean back hard, since more coming in pushes them down a step at a time.
	kindGood: {sigma: 0.02, lean: 0.08, beta: -0.05, cap: 0.08},
	kindCoin: {sigma: 0.05, jumps: true},
}

// The coin spikes about once a month and crashes a little more often, each
// sized so a jump is worth nothing on average.
const (
	spikeDay = 1.0 / 30
	crashDay = spikeDay * 1.4
	// Past these it heads back, a tenth of the way a day.
	coinFloor = 25.0
	coinRoof  = 400.0
	coinLean  = 0.1
)

// Goods never leave this band around their base, so a lot bought always
// costs more than what it can be cooked into.
const goodLow, goodHigh = 0.7, 1.6

// draw is a slot's standard normal step and a uniform for the coin's jumps.
type draw func(slot int) (z, u float64)

func hmacDraw(pepper []byte, day, key string) draw {
	return func(slot int) (float64, float64) {
		mac := hmac.New(sha256.New, pepper)
		fmt.Fprintf(mac, "market|%s|%s|%d", day, key, slot)
		b := mac.Sum(nil)
		unit := func(i int) float64 { return float64(binary.BigEndian.Uint64(b[i:i+8])>>11) / (1 << 53) }
		u1, u2 := unit(0)+0x1p-54, unit(8)
		return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2), unit(16)
	}
}

// walk is the log move of the first n slots of a day.
func walk(t tune, d draw, n int) float64 {
	sd := t.sigma / math.Sqrt(daySlots)
	up, down := spikeDay/daySlots, crashDay/daySlots
	x := 0.0
	for i := range n {
		z, u := d(i)
		x += sd*z - sd*sd/2
		if !t.jumps {
			continue
		}
		switch {
		case u < up:
			x += math.Log(1.2 + 0.3*u/up)
		case u < up+down:
			x += math.Log(0.65 + 0.2*(u-up)/down)
		}
	}
	return x
}

// ---------------------------------------------------------------- flow

// flowBook is every pond's flow by key, day and hour.
type flowBook map[string]map[string]*[24]int

func dayShift(day string, by int) string {
	t, err := time.ParseInLocation("2006-01-02", day, pondZone)
	if err != nil {
		return day
	}
	return dayOf(t.Add(12*time.Hour).AddDate(0, 0, by))
}

func loadFlows(q querier, day string) (flowBook, error) {
	rows, err := q.Query(`SELECT key, day, hour, n FROM market_flow WHERE day >= ? AND day <= ?`, dayShift(day, -flowWeek-1), day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := flowBook{}
	for rows.Next() {
		var k, d string
		var h, n int
		if err := rows.Scan(&k, &d, &h, &n); err != nil {
			return nil, err
		}
		if h < 0 || h > 23 {
			continue
		}
		if out[k] == nil {
			out[k] = map[string]*[24]int{}
		}
		if out[k][d] == nil {
			out[k][d] = &[24]int{}
		}
		out[k][d][h] += n
	}
	return out, rows.Err()
}

// upTo is a day's flow before the hour h, plus frac of hour h.
func (b flowBook) upTo(key, day string, h int, frac float64) float64 {
	hrs := b[key][day]
	if hrs == nil {
		return 0
	}
	n := 0.0
	for i := range h {
		n += float64(hrs[i])
	}
	if h < 24 {
		n += float64(hrs[h]) * frac
	}
	return n
}

// ratio is a key's flow today against its usual, with the rest of today
// taken as usual so a morning doesn't swing on a few ponds.
func (b flowBook) ratio(key, day string, at time.Time, full bool) float64 {
	h, frac := 24, 0.0
	if !full {
		t := at.In(pondZone)
		h, frac = t.Hour(), float64(t.Minute())/60
	}
	usual, usualSoFar := 0.0, 0.0
	for i := 1; i <= flowWeek; i++ {
		d := dayShift(day, -i)
		usual += b.upTo(key, d, 24, 0)
		usualSoFar += b.upTo(key, d, h, frac)
	}
	usual, usualSoFar = usual/flowWeek, usualSoFar/flowWeek
	today := b.upTo(key, day, h, 1)
	return (today + usual - usualSoFar + flowDamp) / (usual + flowDamp)
}

// pull is the log of an asset's ratio, held to a doubling or a halving.
func (b flowBook) pull(a asset, day string, at time.Time, full bool) float64 {
	if a.flow == "" && a.Kind == kindShare {
		sum, n := 0.0, 0
		for _, o := range assets {
			if o.Kind == kindShare && o.flow != "" {
				sum += b.pull(o, day, at, full)
				n++
			}
		}
		return sum / float64(n)
	}
	if a.flow == "" {
		return 0
	}
	return max(-math.Ln2, min(math.Ln2, math.Log(b.ratio(a.flow, day, at, full))))
}

func addFlow(tx *sql.Tx, p *pond, key string, n int) error {
	if key == "" || n <= 0 || p.today == "" {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO market_flow (key, day, hour, n) VALUES (?, ?, ?, ?)
		ON CONFLICT (key, day, hour) DO UPDATE SET n = n + excluded.n`, key, p.today, p.at.In(pondZone).Hour(), n)
	return err
}

// ---------------------------------------------------------------- a price

func dayBounds(day string) (time.Time, time.Time) {
	t, _ := time.ParseInLocation("2006-01-02", day, pondZone)
	return t, t.AddDate(0, 0, 1)
}

// priceAt is an asset's price at a moment of day, from the day's open. With
// full it's the close, and div is what a share pays for the day.
func priceAt(a asset, d draw, book flowBook, day string, open float64, at time.Time, full bool) (price, div float64) {
	start, end := dayBounds(day)
	frac, n := 1.0, int(math.Ceil(float64(end.Sub(start))/float64(slotLen)))
	if !full {
		frac = min(1, max(0, float64(at.Sub(start))/float64(end.Sub(start))))
		n = min(n, max(0, int(at.Sub(start)/slotLen)))
	}
	t := tunes[a.Kind]
	x := walk(t, d, n)
	pull := book.pull(a, day, at, full)
	x += t.beta * pull / math.Ln2
	switch {
	case a.Kind == kindCoin && open < coinFloor:
		x += coinLean * frac * math.Log(coinFloor/open)
	case a.Kind == kindCoin && open > coinRoof:
		x += coinLean * frac * math.Log(coinRoof/open)
	case t.lean > 0:
		x += t.lean * frac * math.Log(a.base/open)
	}
	if t.cap > 0 {
		x = max(-t.cap, min(t.cap, x))
	}
	price = max(1, open*math.Exp(x))
	if a.Kind == kindGood {
		price = max(a.base*goodLow, min(a.base*goodHigh, price))
	}
	if a.Kind == kindShare {
		div = divYield * price * max(0.6, min(1.4, math.Exp(pull/2)))
	}
	return price, div
}
