package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// A stream only runs past the rain barrel while it really rains, and a sieve
// on the way in catches what it washes off the bank, a deal for each point of
// wet in the rain table. Dry nights leave dew, and it's tipped once a day.
const (
	// About six hours of steady rain, or three days of showers.
	barrelCap = 54
	// Night hours in pond time that leave dew in the barrel when it's dry.
	dewFrom, dewTo = 22, 6
	// Watering the garden from the barrel doubles it this long, like rain.
	barrelWaters = 3 * time.Hour
)

const (
	tipSieve  = "sieve"
	tipGarden = "garden"
)

// What the rain can wash in, out of a thousand a point. An empty kind is
// silt, peb is pebbles, and crop, rock and trinket are dealt again below.
type washIn struct {
	kind string
	n, w int
}

var washRain = []washIn{
	{"", 0, 410},
	{"peb", 1, 170}, {"peb", 2, 115}, {"peb", 3, 57}, {"peb", 4, 38},
	{"sticks", 1, 70}, {"reeds", 1, 50}, {"clay", 1, 40},
	{"snail", 1, 15}, {"crop", 1, 15}, {"bait_worms", 1, 8},
	{"rock", 1, 7}, {"trinket", 1, 5},
}

var washDew = []washIn{
	{"", 0, 720}, {"peb", 1, 250}, {"snail", 1, 20}, {"bait_worms", 1, 10},
}

type barrelThing struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	N    int    `json:"n"`
	Tier int    `json:"tier"`
}

// sieve is what the barrel holds now. Level is points of rain and dew, rain
// first, so a storm after a dry week still fills it.
type sieve struct {
	Level   int           `json:"level"`
	Rain    int           `json:"rain"`
	Dew     int           `json:"dew"`
	Pebbles int           `json:"pebbles"`
	Things  []barrelThing `json:"things"`
}

func (sv *sieve) add(kind string, n int) {
	if kind == "peb" {
		sv.Pebbles += n
		return
	}
	for i := range sv.Things {
		if sv.Things[i].Kind == kind {
			sv.Things[i].N += n
			return
		}
	}
	sv.Things = append(sv.Things, barrelThing{Kind: kind, Name: thingName(kind), N: n, Tier: thingTier(kind)})
}

func thingTier(kind string) int {
	if t, ok := trinketOf(kind); ok {
		return t.tier
	}
	for _, r := range rocks {
		if r.kind == kind {
			return r.tier
		}
	}
	return 0
}

func barrelRand(pepper []byte, what string) *rand.Rand {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte("barrel|" + what))
	b := mac.Sum(nil)
	return rand.New(rand.NewPCG(binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:16])))
}

// washed deals one point off its own hash, so a point is the same thing
// however often the barrel is looked at.
func washed(table []washIn, r *rand.Rand, at time.Time) (string, int) {
	n, acc := r.IntN(1000), 0
	for _, w := range table {
		acc += w.w
		if n < acc {
			switch w.kind {
			case "crop":
				return ingredients[cropOf(at, true, r.IntN)].kind, 1
			case "rock":
				if r.IntN(7) < 5 {
					return "agate", 1
				}
				return "quartz", 1
			case "trinket":
				var pool []trinket
				for _, t := range trinkets {
					if t.tier == 0 && pets[t.kind] == "" {
						pool = append(pool, t)
					}
				}
				return pool[r.IntN(len(pool))].kind, 1
			}
			return w.kind, w.n
		}
	}
	return "", 0
}

func dewHour(t time.Time) bool {
	h := t.In(pondZone).Hour()
	return h >= dewFrom || h < dewTo
}

// dealBarrel fills the barrel from the rain that fell after from and the
// dry night hours since, up to the cap.
func dealBarrel(q querier, pepper []byte, id int64, from, to time.Time) (sieve, error) {
	sv := sieve{Things: []barrelThing{}}
	rows, err := q.Query(`SELECT slot, wet FROM rain WHERE slot > ? AND slot <= ? ORDER BY slot`,
		rainSlot(from), rainSlot(to))
	if err != nil {
		return sv, err
	}
	wetSlots := map[int64]bool{}
	type slotWet struct {
		slot int64
		wet  int
	}
	var fell []slotWet
	for rows.Next() {
		var sw slotWet
		if err := rows.Scan(&sw.slot, &sw.wet); err != nil {
			rows.Close()
			return sv, err
		}
		fell = append(fell, sw)
		wetSlots[sw.slot] = true
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return sv, err
	}
	for _, sw := range fell {
		at := time.Unix(sw.slot*rainSlotSecs, 0)
		for i := range sw.wet {
			if sv.Level >= barrelCap {
				break
			}
			sv.Level++
			sv.Rain++
			kind, n := washed(washRain, barrelRand(pepper, fmt.Sprintf("%d|%d|%d", id, sw.slot, i)), at)
			if n > 0 {
				sv.add(kind, n)
			}
		}
	}
	perHour := int64(3600 / rainSlotSecs)
	for h := (from.Unix() + 3599) / 3600; (h+1)*3600 <= to.Unix() && sv.Level < barrelCap; h++ {
		at := time.Unix(h*3600, 0)
		if !dewHour(at) {
			continue
		}
		dry := true
		for k := range perHour {
			dry = dry && !wetSlots[h*perHour+k]
		}
		if !dry {
			continue
		}
		sv.Level++
		sv.Dew++
		kind, n := washed(washDew, barrelRand(pepper, fmt.Sprintf("%d|dew|%d", id, h)), at)
		if n > 0 {
			sv.add(kind, n)
		}
	}
	return sv, nil
}

// barrelRow is the barrel's own record. A pond with no row has had its
// barrel since it was made, so nobody gets back pay from before that.
type barrelRow struct {
	at, waterAt int64
	day         string
	// Seconds of watering from earlier days, already on the garden's clock.
	watered float64
}

func loadBarrel(q querier, p *pond) (barrelRow, error) {
	b := barrelRow{at: p.CreatedAt}
	err := q.QueryRow(`SELECT at, day, water_at, watered FROM barrel WHERE pond_id = ?`, p.ID).
		Scan(&b.at, &b.day, &b.waterAt, &b.watered)
	if errors.Is(err, sql.ErrNoRows) {
		return b, nil
	}
	return b, err
}

// extra is how far ahead of every other garden the barrel has put this
// pond's, which runs on at one more second a second while it's watering.
func (b barrelRow) extra(now time.Time) float64 {
	if b.waterAt == 0 {
		return b.watered
	}
	run := min(max(0, now.Unix()-b.waterAt), int64(barrelWaters/time.Second))
	return b.watered + float64(run)
}

func (b barrelRow) watering(now time.Time) int64 {
	if b.waterAt == 0 {
		return 0
	}
	return max(0, b.waterAt+int64(barrelWaters/time.Second)-now.Unix())
}

// gardenClock is the growing clock as this pond's garden reads it.
func (s *store) gardenClock(q querier, id int64, now time.Time) (float64, error) {
	clock, err := s.growth(q, now)
	if err != nil {
		return 0, err
	}
	var waterAt int64
	var watered float64
	err = q.QueryRow(`SELECT water_at, watered FROM barrel WHERE pond_id = ?`, id).Scan(&waterAt, &watered)
	if errors.Is(err, sql.ErrNoRows) {
		return clock, nil
	}
	if err != nil {
		return 0, err
	}
	return clock + barrelRow{waterAt: waterAt, watered: watered}.extra(now), nil
}

type barrelView struct {
	sieve
	Cap   int  `json:"cap"`
	Ready bool `json:"ready"`
	// Raining is how hard it's raining now, 0 when the stream is dry.
	Raining int `json:"raining"`
	// Water is why the garden can't be watered from it now, or empty.
	Water string `json:"water"`
	// Seconds the barrel's water has left on the garden.
	Watering int64 `json:"watering"`
}

// worth says the sieve has enough in it to put on the Next strip.
func (b barrelView) worth() bool {
	return b.Ready && (b.Rain > 0 || len(b.Things) > 0 || b.Pebbles >= 4)
}

func waterWhy(r *reading, now time.Time, garden int) string {
	if garden < 1 {
		return "There's no garden to water yet"
	}
	switch _, why := growRate(r, now); why {
	case "rain":
		return "It's raining, the garden's had plenty"
	case "frost":
		return "It's frosty, the garden's waiting for it to lift"
	}
	return ""
}

func (s *store) barrelOf(q querier, p *pond, now time.Time) (barrelView, error) {
	b, err := loadBarrel(q, p)
	if err != nil {
		return barrelView{}, err
	}
	v := barrelView{Cap: barrelCap, Watering: b.watering(now)}
	if v.sieve, err = dealBarrel(q, s.pepper, p.ID, time.Unix(b.at, 0), now); err != nil {
		return v, err
	}
	r := s.day.reading(now)
	v.Raining = wetOf(r)
	v.Ready = b.day != dayOf(now) && v.Level > 0
	v.Water = waterWhy(r, now, p.GardenLevel)
	return v, nil
}

type tipped struct {
	How   string `json:"how"`
	Sieve sieve  `json:"sieve"`
	Pond  *view  `json:"pond"`
}

// tipBarrel empties the barrel once a day and keeps what the sieve caught.
// Watering the garden with it keeps the sieve too and runs the garden at the
// rain's double for a few hours, which is only offered on a dry day.
func (s *store) tipBarrel(id int64, how string) (*tipped, error) {
	out := &tipped{How: how}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		b, err := loadBarrel(tx, p)
		if err != nil {
			return err
		}
		if b.day == p.today {
			return errNotYet
		}
		sv, err := dealBarrel(tx, s.pepper, p.ID, time.Unix(b.at, 0), now)
		if err != nil {
			return err
		}
		if sv.Level == 0 {
			return errNotYet
		}
		if how == tipGarden {
			if waterWhy(s.day.reading(now), now, p.GardenLevel) != "" {
				return errNotYet
			}
			b.watered = b.extra(now)
			b.waterAt = now.Unix()
		}
		if err := award(tx, p, &prize{Pebbles: sv.Pebbles}); err != nil {
			return err
		}
		for _, t := range sv.Things {
			for range t.N {
				if err := award(tx, p, &prize{Item: t.Kind}); err != nil {
					return err
				}
			}
		}
		out.Sieve = sv
		_, err = tx.Exec(`INSERT INTO barrel (pond_id, at, day, water_at, watered) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (pond_id) DO UPDATE SET at = excluded.at, day = excluded.day,
			water_at = excluded.water_at, watered = excluded.watered`,
			p.ID, now.Unix(), p.today, b.waterAt, b.watered)
		return err
	})
	out.Pond = v
	return out, err
}
