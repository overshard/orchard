package main

import (
	"database/sql"
	"errors"
	"hash/fnv"
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
	h := fnv.New32a()
	h.Write([]byte("disk " + day))
	return arcadeGames[h.Sum32()%uint32(len(arcadeGames))].key
}

func fullRuns(key string, now time.Time) int {
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
}

func loadArcade(q querier, id int64, key string) (arcadeRow, error) {
	var r arcadeRow
	err := q.QueryRow(`SELECT day, runs, paid, best, run, started_at FROM arcade WHERE pond_id = ? AND game = ?`, id, key).
		Scan(&r.day, &r.runs, &r.paid, &r.best, &r.run, &r.startedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	return r, err
}

func saveArcade(tx *sql.Tx, id int64, key string, r arcadeRow) error {
	_, err := tx.Exec(`INSERT INTO arcade (pond_id, game, day, runs, paid, best, run, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (pond_id, game) DO UPDATE SET day = excluded.day, runs = excluded.runs, paid = excluded.paid,
		best = excluded.best, run = excluded.run, started_at = excluded.started_at`,
		id, key, r.day, r.runs, r.paid, r.best, r.run, r.startedAt)
	return err
}

// settled starts the day's counts again on a new day.
func (r arcadeRow) settled(now time.Time) arcadeRow {
	if d := dayOf(now); r.day != d {
		r.day, r.runs, r.paid = d, 0, 0
	}
	return r
}

type arcadeStarted struct {
	Run string `json:"run"`
	// What's biting and how often, for fishing.
	Fish map[string]float64 `json:"fish,omitempty"`
}

func (s *store) arcadeStart(id int64, key string) (*arcadeStarted, error) {
	if _, ok := arcadeOf(key); !ok {
		return nil, errNotHeld
	}
	out := &arcadeStarted{Run: randomHex(8)}
	_, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		r, err := loadArcade(tx, p.ID, key)
		if err != nil {
			return err
		}
		r = r.settled(now)
		r.run, r.startedAt = out.Run, now.Unix()
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
	// Catch is what a round of fishing put in the fish book.
	Catch []caught `json:"catch,omitempty"`
	Pond  *view    `json:"pond"`
}

// landed is what a round of fishing says it caught, by kind.
func (s *store) arcadeFinish(id int64, key, run string, score int, landed map[string]int) (*arcadeDone, error) {
	g, ok := arcadeOf(key)
	if !ok {
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
		r.run, r.startedAt = "", 0
		// The run is spent either way, and an impossible score pays nothing.
		if score < 0 || score > g.most || float64(score) > took.Seconds()*g.rate+5 {
			out.Score = 0
			return saveArcade(tx, p.ID, key, r)
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
		out = append(out, arcadeView{g.key, g.name, r.best, r.paid, g.cap, r.runs, full, full > arcadeFullRuns})
	}
	return out, nil
}
