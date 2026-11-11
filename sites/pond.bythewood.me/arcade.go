package main

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"hash/fnv"
	"strings"
	"time"
)

// Side games pay into the same purse as Dive, each with its own day's cap.
// A run pays in full for the first few of a day and a quarter after.
type arcadeGame struct {
	key, name string
	cap       int
	// The most points a second of play can make, and the most a run can.
	rate float64
	most int
	// Points to a pebble, for the games that score fast enough to fill their
	// cap in one run at a pebble a point. Lily leap scores in metres.
	per int
}

var arcadeGames = []arcadeGame{
	{"flick", "Tongue flick", 50, 5, 400, 1},
	{"cross", "Crossing", 60, 8, 1500, 2},
	{"raft", "Raft run", 40, 4, 600, 1},
	{"leap", "Lily leap", 40, 20, 3000, 4},
	{"school", "Tadpoles", 40, 6, 600, 1},
	{"fish", "Fishing", 80, 8, 900, 2},
	{"stack", "Lily stack", 40, 6, 3000, 1},
	{"shell", "Shell game", 40, 4, 2000, 1},
	{"skip", "Pebble skip", 40, 12, 400, 2},
	{"drop", "Pebble drop", 40, 4, dropMost, 2},
	{"sweep", "Bug sweep", 40, 0, bugSafe, 1},
	{"chorus", "Chorus", 40, 7, chorusMost, 15},
}

// Every beat of chorus right on time is all a song can score, before a
// singer's quarter.
const chorusMost = 601

// A game with no rate is played out on the server a tap at a time, so it
// never takes a score from the page. It pays once a day and is never the
// disk of the day.
func (g arcadeGame) replayed() bool { return g.rate == 0 }

// Five pebbles a round and eight for the middle pot is all pebble drop has.
const dropMost, dropRows = 40, 9

// dropDeal is how many pixels each row of pebble drop's pegs sits off its
// place for a day, the same for everybody, so no spot is the best one for good.
func dropDeal(day string) []int {
	sum := sha256.Sum256([]byte("drop " + day))
	out := make([]int, dropRows)
	for r := range out {
		out[r] = int(sum[r])%17 - 8
	}
	return out
}

const arcadeFullRuns = 3

// A frog that hasn't played anything for restAfter is well rested, and the
// next run of any game pays a quarter more, inside that game's cap for the day.
const restAfter = 8 * time.Hour

func (p *pond) rested(now time.Time) bool {
	return now.Sub(time.Unix(p.PlayedAt, 0)) >= restAfter
}

// payRun is what a run's pay comes to with room left under the cap, and how
// much of it was for being rested. It marks the pond as having played.
func (p *pond) payRun(pay, room int, now time.Time) (paid, bonus int) {
	room = max(0, room)
	plain := min(pay, room)
	paid = plain
	if p.rested(now) {
		paid = min(pay+pay/4, room)
	}
	p.PlayedAt = now.Unix()
	return paid, paid - plain
}

// One side game a day is the disk of the day, the same for everybody, and
// pays in full for one run more. Its cap is the same as any other day's.
func diskOfDay(day string) string {
	var keys []string
	for _, g := range arcadeGames {
		if !g.replayed() {
			keys = append(keys, g.key)
		}
	}
	h := fnv.New32a()
	h.Write([]byte("disk " + day))
	return keys[h.Sum32()%uint32(len(keys))]
}

func fullRuns(key string, now time.Time) int {
	if g, _ := arcadeOf(key); g.replayed() {
		return 1
	}
	if key == diskOfDay(dayOf(now)) {
		return arcadeFullRuns + 1
	}
	return arcadeFullRuns
}

func arcadeOf(key string) (arcadeGame, bool) {
	for _, g := range arcadeGames {
		if g.key == key {
			return g, true
		}
	}
	return arcadeGame{}, false
}

type arcadeRow struct {
	day, run         string
	runs, paid, best int
	startedAt        int64
	// Fishing's last round can keep one of these kinds for the kitchen, and
	// kept is how many it has kept today.
	keep string
	kept int
}

func loadArcade(q querier, id int64, key string) (arcadeRow, error) {
	var r arcadeRow
	err := q.QueryRow(`SELECT day, runs, paid, best, run, started_at, keep, kept FROM arcade WHERE pond_id = ? AND game = ?`, id, key).
		Scan(&r.day, &r.runs, &r.paid, &r.best, &r.run, &r.startedAt, &r.keep, &r.kept)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	return r, err
}

func saveArcade(tx *sql.Tx, id int64, key string, r arcadeRow) error {
	_, err := tx.Exec(`INSERT INTO arcade (pond_id, game, day, runs, paid, best, run, started_at, keep, kept) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (pond_id, game) DO UPDATE SET day = excluded.day, runs = excluded.runs, paid = excluded.paid,
		best = excluded.best, run = excluded.run, started_at = excluded.started_at, keep = excluded.keep, kept = excluded.kept`,
		id, key, r.day, r.runs, r.paid, r.best, r.run, r.startedAt, r.keep, r.kept)
	return err
}

// settled starts the day's counts again on a new day.
func (r arcadeRow) settled(now time.Time) arcadeRow {
	if d := dayOf(now); r.day != d {
		r.day, r.runs, r.paid, r.kept = d, 0, 0, 0
	}
	return r
}

type arcadeStarted struct {
	Run string `json:"run"`
	// What's biting and how often, for fishing.
	Fish map[string]float64 `json:"fish,omitempty"`
}

func (s *store) arcadeStart(id int64, key string) (*arcadeStarted, error) {
	if g, ok := arcadeOf(key); !ok || g.replayed() {
		return nil, errNotHeld
	}
	out := &arcadeStarted{Run: randomHex(8)}
	_, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		r, err := loadArcade(tx, p.ID, key)
		if err != nil {
			return err
		}
		r = r.settled(now)
		r.run, r.startedAt, r.keep = out.Run, now.Unix(), ""
		if key == "fish" {
			out.Fish = s.fishTableAt(now)
		}
		return saveArcade(tx, p.ID, key, r)
	})
	return out, err
}

type arcadeDone struct {
	Score int  `json:"score"`
	Paid  int  `json:"paid"`
	Best  bool `json:"best"`
	// Rested is how much of Paid was for being well rested.
	Rested int `json:"rested"`
	// Singer is a chorus scored a quarter more for the frog out front.
	Singer bool `json:"singer,omitempty"`
	// Catch is what a round of fishing put in the fish book.
	Catch []caught `json:"catch,omitempty"`
	// Sedge has something to say when a pond record goes.
	Sedge string `json:"sedge,omitempty"`
	// Keeps is how many more fish can be kept for the kitchen today, when
	// this round landed one that can.
	Keeps int   `json:"keeps,omitempty"`
	Pond  *view `json:"pond"`
}

// landed is what a round of fishing says it caught, by kind.
func (s *store) arcadeFinish(id int64, key, run string, score int, landed map[string]int) (*arcadeDone, error) {
	g, ok := arcadeOf(key)
	if !ok || g.replayed() {
		return nil, errNotHeld
	}
	out := &arcadeDone{Score: score}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		r, err := loadArcade(tx, p.ID, key)
		if err != nil {
			return err
		}
		if run == "" || run != r.run {
			return errBadRun
		}
		began := time.Unix(r.startedAt, 0)
		took := now.Sub(began) + time.Second
		r = r.settled(now)
		r.run, r.startedAt, r.keep = "", 0, ""
		// The run is spent either way, and an impossible score pays nothing.
		if score < 0 || score > g.most || float64(score) > took.Seconds()*g.rate+5 {
			out.Score = 0
			return saveArcade(tx, p.ID, key, r)
		}
		// A singer out front scores a quarter more, added here so the page
		// can't claim it for a frog that isn't one.
		if key == "chorus" && frogOf(p, now).genome.Nature == "SINGER" {
			score += score / 4
			out.Score, out.Singer = score, true
		}
		r.runs++
		pay := score / g.per
		if r.runs > fullRuns(key, now) {
			pay = score / (4 * g.per)
		}
		out.Paid, out.Rested = p.payRun(pay, g.cap-r.paid, now)
		r.paid += out.Paid
		p.Pebbles += out.Paid
		if score > r.best {
			if err := noteRecord(tx, p.ID, key, score, r.best, now); err != nil {
				return err
			}
			r.best, out.Best = score, true
		}
		if key == "fish" {
			// The day can turn over mid round, so a fish biting at either end counts.
			tables := []map[string]float64{s.fishTableAt(began), s.fishTableAt(now)}
			if out.Catch, err = landFish(tx, p, landed, score, tables); err != nil {
				return err
			}
			var kinds []string
			for _, c := range out.Catch {
				if f, _ := fishOf(c.Kind); c.Record {
					out.Sedge = sedgeSays(f, residentNamed(c.From))
				}
				kinds = append(kinds, c.Kind)
			}
			// The kitchen is in the works, so nothing is offered before they open.
			if r.kept < fishKeeps && len(kinds) > 0 && openOf(p, now).Works {
				r.keep, out.Keeps = strings.Join(kinds, ","), fishKeeps-r.kept
			}
		}
		return saveArcade(tx, p.ID, key, r)
	})
	out.Pond = v
	return out, err
}

type arcadeView struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Best int    `json:"best"`
	Paid int    `json:"paid"`
	Cap  int    `json:"cap"`
	Runs int    `json:"runs"`
	Full int    `json:"full"`
	// Day is set on the disk of the day.
	Day bool `json:"day"`
	// Deal is the day's peg rows, for pebble drop.
	Deal []int `json:"deal,omitempty"`
	// Daily is a game with one paid go a day and free ones after.
	Daily bool `json:"daily,omitempty"`
}

func arcadeViews(q querier, id int64, now time.Time) ([]arcadeView, error) {
	var out []arcadeView
	for _, g := range arcadeGames {
		r, err := loadArcade(q, id, g.key)
		if err != nil {
			return nil, err
		}
		r = r.settled(now)
		full := fullRuns(g.key, now)
		a := arcadeView{Key: g.key, Name: g.name, Best: r.best, Paid: r.paid, Cap: g.cap, Runs: r.runs, Full: full, Day: full > arcadeFullRuns, Daily: g.replayed()}
		if g.key == "drop" {
			a.Deal = dropDeal(dayOf(now))
		}
		out = append(out, a)
	}
	return out, nil
}
