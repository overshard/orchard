package main

import (
	"database/sql"
	"errors"
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
}

const arcadeFullRuns = 3

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
		return saveArcade(tx, p.ID, key, r)
	})
	return out, err
}

type arcadeDone struct {
	Score int   `json:"score"`
	Paid  int   `json:"paid"`
	Best  bool  `json:"best"`
	Pond  *view `json:"pond"`
}

func (s *store) arcadeFinish(id int64, key, run string, score int) (*arcadeDone, error) {
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
		took := now.Sub(time.Unix(r.startedAt, 0)) + time.Second
		r = r.settled(now)
		r.run, r.startedAt = "", 0
		// The run is spent either way, and an impossible score pays nothing.
		if score < 0 || score > g.most || float64(score) > took.Seconds()*g.rate+5 {
			out.Score = 0
			return saveArcade(tx, p.ID, key, r)
		}
		r.runs++
		pay := score / g.per
		if r.runs > arcadeFullRuns {
			pay = score / (4 * g.per)
		}
		out.Paid = max(0, min(pay, g.cap-r.paid))
		r.paid += out.Paid
		p.Pebbles += out.Paid
		if score > r.best {
			r.best, out.Best = score, true
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
}

func arcadeViews(q querier, id int64, now time.Time) ([]arcadeView, error) {
	var out []arcadeView
	for _, g := range arcadeGames {
		r, err := loadArcade(q, id, g.key)
		if err != nil {
			return nil, err
		}
		r = r.settled(now)
		out = append(out, arcadeView{g.key, g.name, r.best, r.paid, g.cap, r.runs, arcadeFullRuns})
	}
	return out, nil
}
