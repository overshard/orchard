package main

import (
	"database/sql"
	"math"
	"strings"
	"time"
)

// A keepsake is handed over on a visit on a day when something happened,
// stamped with that day. They're never spent or sold, so the shelf only grows.
type keepsake struct {
	kind, name, set string
	tier            int
	line            string
	on              func(r *reading, now time.Time) bool
}

var keepsakes = []keepsake{
	{"k_frost", "Frosted leaf", "weather", 1, "a hard frost",
		func(r *reading, _ time.Time) bool { return r != nil && r.HasTemp && r.Temp <= 32 }},
	{"k_web", "Dewy web", "weather", 1, "a foggy morning",
		func(r *reading, _ time.Time) bool { return r != nil && condHas(r, "fog", "mist", "haze") }},
	{"k_storm", "Storm twig", "weather", 2, "a thunderstorm",
		func(r *reading, _ time.Time) bool { return r != nil && condHas(r, "thunder") }},
	{"k_shell", "Sunbaked shell", "weather", 2, "a scorcher over 92°",
		func(r *reading, _ time.Time) bool { return r != nil && r.HasTemp && r.Temp >= 92 }},
	{"k_snow", "Tiny snowball", "weather", 3, "snow",
		func(r *reading, _ time.Time) bool { return r != nil && condHas(r, "snow", "flurr") }},
	{"k_lily", "Lucky lily", "market", 2, "a big up day on the market",
		func(r *reading, _ time.Time) bool { return r != nil && r.HasSP && r.SP >= 1.5 }},
	{"k_penny", "Sunk penny", "market", 2, "a big down day on the market",
		func(r *reading, _ time.Time) bool { return r != nil && r.HasSP && r.SP <= -1.5 }},
	{"k_kite", "Kite string", "market", 3, "the VIX over 30",
		func(r *reading, _ time.Time) bool { return r != nil && r.VIX >= 30 }},
	{"k_wing", "Dragonfly wing", "news", 1, "a big news day",
		func(r *reading, _ time.Time) bool { return r != nil && r.Major }},
	{"k_amber", "Gnat in amber", "news", 1, "a swarm on Hacker News",
		func(r *reading, _ time.Time) bool { return r != nil && r.Swarm }},
	{"k_heron", "Heron feather", "news", 2, "one of the sites going down",
		func(r *reading, _ time.Time) bool { return r != nil && r.Down != "" }},
	{"k_moon", "Moonlit petal", "calendar", 1, "a full moon",
		func(_ *reading, now time.Time) bool { return fullMoon(now) }},
	{"k_blossom", "Cherry blossom", "calendar", 2, "the spring equinox",
		func(_ *reading, now time.Time) bool { return near(now, time.March, 20) }},
	{"k_daisy", "Midsummer daisy", "calendar", 2, "midsummer",
		func(_ *reading, now time.Time) bool { return near(now, time.June, 21) }},
	{"k_maple", "Red maple leaf", "calendar", 2, "the autumn equinox",
		func(_ *reading, now time.Time) bool { return near(now, time.September, 22) }},
	{"k_holly", "Holly sprig", "calendar", 2, "midwinter",
		func(_ *reading, now time.Time) bool { return near(now, time.December, 21) }},
	{"k_spawn", "Frogspawn bead", "calendar", 3, "a warm rain in late winter",
		func(r *reading, now time.Time) bool { return r != nil && spawning(r, now) }},
}

func condHas(r *reading, words ...string) bool {
	c := strings.ToLower(r.Cond)
	for _, w := range words {
		if strings.Contains(c, w) {
			return true
		}
	}
	return false
}

// near is a day either side of a date, since a solstice or an equinox moves
// about a day from year to year and one day is easy to miss.
func near(now time.Time, m time.Month, d int) bool {
	n := now.In(pondZone)
	at := time.Date(n.Year(), m, d, 12, 0, 0, 0, pondZone)
	gap := n.Sub(at)
	return gap > -36*time.Hour && gap < 36*time.Hour
}

const synodic = 29.530588853

var newMoon = time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)

// fullMoon is the pond's day nearest a full moon, worked out from the mean
// month, which drifts up to about half a day from the real one.
func fullMoon(now time.Time) bool {
	n := now.In(pondZone)
	noon := time.Date(n.Year(), n.Month(), n.Day(), 12, 0, 0, 0, pondZone)
	age := math.Mod(noon.Sub(newMoon).Hours()/24, synodic)
	return math.Abs(age-synodic/2) < 1
}

// Frogspawn in Georgia comes on the first warm rain of late winter.
func spawnSeason(now time.Time) bool {
	n := now.In(pondZone)
	return (n.Month() == time.February && n.Day() >= 10) || (n.Month() == time.March && n.Day() <= 20)
}

func spawning(r *reading, now time.Time) bool {
	return spawnSeason(now) && r.HasTemp && r.Temp >= 50 && (r.Rain >= 60 || condHas(r, "rain", "shower", "drizzle"))
}

// stampKeepsakes hands over whatever today brings that the pond hasn't had
// today, and returns the lines for the greeting.
func stampKeepsakes(tx *sql.Tx, id int64, r *reading, now time.Time) ([][2]string, error) {
	out := [][2]string{}
	day := dayOf(now)
	for _, k := range keepsakes {
		if !k.on(r, now) {
			continue
		}
		res, err := tx.Exec(`INSERT INTO keepsakes (pond_id, kind, day) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, id, k.kind, day)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			out = append(out, [2]string{"Keepsake", k.name + ", for " + k.line})
		}
	}
	return out, nil
}

type keepsakeView struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Set   string `json:"set"`
	Tier  int    `json:"tier"`
	First string `json:"first"`
	Days  int    `json:"days"`
	// What brings it, shown on a shadow so there's something to wait for.
	Hint string `json:"hint"`
}

func keepsakesOf(q querier, id int64) ([]keepsakeView, error) {
	type seen struct {
		first string
		days  int
	}
	got := map[string]seen{}
	rows, err := q.Query(`SELECT kind, MIN(day), COUNT(*) FROM keepsakes WHERE pond_id = ? GROUP BY kind`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var s seen
		if err := rows.Scan(&k, &s.first, &s.days); err != nil {
			return nil, err
		}
		got[k] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]keepsakeView, 0, len(keepsakes))
	for _, k := range keepsakes {
		s := got[k.kind]
		out = append(out, keepsakeView{k.kind, k.name, k.set, k.tier, s.first, s.days, "Comes with " + k.line + "."})
	}
	return out, nil
}
