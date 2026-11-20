package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"time"
)

// The page plays the pile out and the server only counts, taking a save that
// adds up, so a pusher can't pay more than its allowance and starting pile.
const (
	pushDaily = 20
	pushKeep  = 60
	// A new table's one time gift, a pile as if it had been pushed a while,
	// and the only pebbles the pusher makes apart from the allowance.
	pushStart, pushSpill = 90, 10
	// A pile can't be cleared by pushing, the front row always holds some.
	pushFloor = 40
	// The most bodies a layout holds, well over a full table.
	pushMost = 300
	// At most this many prizes on the table, and each sits this many drops
	// before it can come off.
	pushPrizes, pushRipe = 3, 30
	// A held finger drops one every half second.
	pushRate = 2.5
)

type pushPrize struct {
	ID   int    `json:"id"`
	Kind string `json:"kind"`
	// At is how many drops the table had taken when it went down.
	At  int    `json:"at"`
	Day string `json:"day"`
}

type pushRow struct {
	held, spill, drops int
	allowanceDay       string
	dropped, won       int
	prizes             []pushPrize
	prizeDay           string
	layout             []byte
	run                string
	savedAt            int64
}

// loadPush reads the pond's table. A pond that never had one gets the new
// table, with its allowance building up from the day the pond was made.
func loadPush(q querier, p *pond) (pushRow, error) {
	r := pushRow{held: pushStart, spill: pushSpill, drops: pushDaily, allowanceDay: dayOf(time.Unix(p.CreatedAt, 0))}
	id := p.ID
	var prizes string
	err := q.QueryRow(`SELECT held, spill, drops, allowance_day, dropped, won, prizes, prize_day, layout, run, saved_at
		FROM pusher WHERE pond_id = ?`, id).
		Scan(&r.held, &r.spill, &r.drops, &r.allowanceDay, &r.dropped, &r.won, &prizes, &r.prizeDay, &r.layout, &r.run, &r.savedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal([]byte(prizes), &r.prizes)
}

func savePush(tx *sql.Tx, id int64, r pushRow) error {
	if r.prizes == nil {
		r.prizes = []pushPrize{}
	}
	prizes, err := json.Marshal(r.prizes)
	if err != nil {
		return err
	}
	if r.layout == nil {
		r.layout = []byte{}
	}
	_, err = tx.Exec(`INSERT INTO pusher (pond_id, held, spill, drops, allowance_day, dropped, won, prizes, prize_day, layout, run, saved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (pond_id) DO UPDATE SET held = excluded.held, spill = excluded.spill, drops = excluded.drops,
		allowance_day = excluded.allowance_day, dropped = excluded.dropped, won = excluded.won, prizes = excluded.prizes,
		prize_day = excluded.prize_day, layout = excluded.layout, run = excluded.run, saved_at = excluded.saved_at`,
		id, r.held, r.spill, r.drops, r.allowanceDay, r.dropped, r.won, string(prizes), r.prizeDay, r.layout, r.run, r.savedAt)
	return err
}

// topUp adds the allowance for every day since it was last topped up, kept
// to pushKeep, so three days away is a big sitting.
func (r *pushRow) topUp(today string) {
	days := int(dayNumber(today)) - int(dayNumber(r.allowanceDay))
	if days > 0 {
		r.drops = min(pushKeep, r.drops+days*pushDaily)
	}
	if days != 0 {
		r.allowanceDay = today
	}
}

// What the pusher puts on the pile once a day, by weight out of a hundred.
// A trinket is any common one that isn't a pet.
var pushFinds = []struct {
	kind string
	w    int
}{{"trinket", 45}, {"geode", 25}, {"agate", 20}, {"quartz", 10}}

func pushPrizeOf(pepper []byte, id int64, day string) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte("pusher|" + day + "|"))
	binary.Write(mac, binary.BigEndian, id)
	b := mac.Sum(nil)
	r := rand.New(rand.NewPCG(binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:16])))
	n, acc := r.IntN(100), 0
	for _, f := range pushFinds {
		acc += f.w
		if n >= acc {
			continue
		}
		if f.kind != "trinket" {
			return f.kind
		}
		var pool []trinket
		for _, t := range trinkets {
			if t.tier == 0 && pets[t.kind] == "" {
				pool = append(pool, t)
			}
		}
		return pool[r.IntN(len(pool))].kind
	}
	return "agate"
}

// putPrize puts the day's prize on the back of the pile, once a day while
// there's room for one.
func (r *pushRow) putPrize(pepper []byte, id int64, today string) {
	if r.prizeDay == today || len(r.prizes) >= pushPrizes {
		return
	}
	used := map[int]bool{}
	for _, z := range r.prizes {
		used[z.ID] = true
	}
	n := 1
	for used[n] {
		n++
	}
	r.prizes = append(r.prizes, pushPrize{ID: n, Kind: pushPrizeOf(pepper, id, today), At: r.dropped, Day: today})
	r.prizeDay = today
}

type pushPrizeView struct {
	ID   int    `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Tier int    `json:"tier"`
	// Ripe is how many more drops it has to sit before it can come off.
	Ripe int `json:"ripe"`
}

// pushTable is the table as the page loads it.
type pushTable struct {
	Run     string          `json:"run,omitempty"`
	Drops   int             `json:"drops"`
	Keep    int             `json:"keep"`
	Daily   int             `json:"daily"`
	On      int             `json:"on"`
	Spill   int             `json:"spill"`
	Won     int             `json:"won"`
	Layout  []byte          `json:"layout"`
	Prizes  []pushPrizeView `json:"prizes"`
	Full    bool            `json:"full"`
	Floor   int             `json:"floor"`
	Dropped int             `json:"dropped"`
}

func (r pushRow) table() pushTable {
	t := pushTable{Run: r.run, Drops: r.drops, Keep: pushKeep, Daily: pushDaily, On: r.held, Spill: r.spill, Won: r.won,
		Layout: r.layout, Prizes: []pushPrizeView{}, Full: r.drops >= pushKeep, Floor: pushFloor, Dropped: r.dropped}
	if t.Layout == nil {
		t.Layout = []byte{}
	}
	for _, z := range r.prizes {
		t.Prizes = append(t.Prizes, pushPrizeView{z.ID, z.Kind, thingName(z.Kind), thingTier(z.Kind), max(0, z.At+pushRipe-r.dropped)})
	}
	return t
}

// pusherOf is the table for the pond view, with the allowance as it stands
// today but nothing written.
func pusherOf(q querier, p *pond, now time.Time) (pushTable, error) {
	r, err := loadPush(q, p)
	if err != nil {
		return pushTable{}, err
	}
	r.topUp(dayOf(now))
	r.run = ""
	return r.table(), nil
}

// pushStartRun tops up the allowance, puts the day's prize down and hands
// the page the table with a run to save against.
func (s *store) pushStartRun(id int64) (*pushTable, error) {
	var out pushTable
	_, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		r, err := loadPush(tx, p)
		if err != nil {
			return err
		}
		r.topUp(p.today)
		r.putPrize(s.pepper, p.ID, p.today)
		r.run, r.savedAt = randomHex(8), now.Unix()
		out = r.table()
		return savePush(tx, p.ID, r)
	})
	return &out, err
}

// pushReport is what the page says happened since the last save. Spilled
// went down the side gutters into the spill jar, and back came out of it in
// a shower. Prizes are the ids that went over the edge.
type pushReport struct {
	Run     string `json:"run"`
	Dropped int    `json:"dropped"`
	Won     int    `json:"won"`
	Spilled int    `json:"spilled"`
	Back    int    `json:"back"`
	Prizes  []int  `json:"prizes"`
	Layout  []byte `json:"layout"`
}

type pushSaved struct {
	OK    bool      `json:"ok"`
	Why   string    `json:"why,omitempty"`
	Paid  int       `json:"paid"`
	Got   []prize   `json:"got"`
	Table pushTable `json:"table"`
	Pond  *view     `json:"pond"`
}

// check says what's wrong with a report against the table, or nothing. Every
// pebble has to be accounted for, on the table, in the spill jar or won.
func (r pushRow) check(in pushReport, took time.Duration) string {
	switch {
	case in.Dropped < 0 || in.Won < 0 || in.Spilled < 0 || in.Back < 0:
		return "a count is below nothing"
	case in.Dropped > r.drops:
		return "more drops than the allowance"
	case float64(in.Dropped) > pushRate*took.Seconds():
		return "dropped faster than a hand can"
	case in.Back > r.spill+in.Spilled:
		return "a shower bigger than the spill jar"
	case in.Won > 3*in.Dropped+12:
		return "won more than a push can"
	}
	after := r.held + in.Dropped + in.Back - in.Won - in.Spilled
	switch {
	case after < pushFloor:
		return "fewer on the table than the front row holds"
	case after > pushMost:
		return "more on the table than it holds"
	}
	held := map[int]pushPrize{}
	for _, z := range r.prizes {
		held[z.ID] = z
	}
	for _, id := range in.Prizes {
		z, ok := held[id]
		if !ok {
			return "a prize that isn't on the table"
		}
		if r.dropped+in.Dropped-z.At < pushRipe {
			return "a prize that hasn't sat long enough"
		}
		delete(held, id)
	}
	if len(in.Layout)%3 != 0 || len(in.Layout)/3 > pushMost {
		return "a layout that doesn't read"
	}
	pebbles := 0
	for i := 2; i < len(in.Layout); i += 3 {
		k := int(in.Layout[i] & 0x7f)
		if k == 0 {
			pebbles++
			continue
		}
		if _, ok := held[k]; !ok {
			return "a prize in the layout that isn't held"
		}
		delete(held, k)
	}
	if pebbles != after {
		return "a layout with the wrong count"
	}
	if len(held) > 0 {
		return "a prize missing from the layout"
	}
	return ""
}

// pushSave takes a report and, if it adds up, pays what was won, gives the
// prizes and keeps the layout. One that doesn't add up spends the run and
// keeps nothing, so the table goes back to how it was.
func (s *store) pushSave(id int64, in pushReport) (*pushSaved, error) {
	out := &pushSaved{Got: []prize{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		r, err := loadPush(tx, p)
		if err != nil {
			return err
		}
		if in.Run == "" || in.Run != r.run {
			return errBadRun
		}
		took := now.Sub(time.Unix(r.savedAt, 0)) + time.Second
		r.run = ""
		if out.Why = r.check(in, took); out.Why != "" {
			out.Table = r.table()
			return savePush(tx, p.ID, r)
		}
		out.OK, out.Paid = true, in.Won
		r.drops -= in.Dropped
		r.dropped += in.Dropped
		r.held += in.Dropped + in.Back - in.Won - in.Spilled
		r.spill += in.Spilled - in.Back
		r.won += in.Won
		if err := award(tx, p, &prize{Pebbles: in.Won}); err != nil {
			return err
		}
		gone := map[int]bool{}
		for _, id := range in.Prizes {
			gone[id] = true
		}
		kept := r.prizes[:0]
		for _, z := range r.prizes {
			if !gone[z.ID] {
				kept = append(kept, z)
				continue
			}
			got := prize{Item: z.Kind, Text: thingName(z.Kind)}
			if err := award(tx, p, &got); err != nil {
				return err
			}
			out.Got = append(out.Got, got)
		}
		r.prizes = kept
		r.layout = in.Layout
		r.run, r.savedAt = randomHex(8), now.Unix()
		out.Table = r.table()
		return savePush(tx, p.ID, r)
	})
	out.Pond = v
	return out, err
}
