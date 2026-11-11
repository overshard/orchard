package main

import (
	"database/sql"
	"errors"
	"slices"
	"time"
)

// The daily race is one Dive course a day, its gates dealt in the page off
// seed, against the residents' gate counts for the day off a hash. Only a
// pond's first finished run of the day goes on the board.
const (
	// A course gate comes along at most this often at Dive's top speed.
	raceGatesASec = 1.2
	raceMost      = 80
)

// What the board's podium pays, inside Dive's cap for the day.
var racePrize = [3]int{15, 10, 5}

type raceRow struct {
	day, run     string
	gates, place int
	startedAt    int64
}

func loadRace(q querier, id int64) (raceRow, error) {
	var r raceRow
	err := q.QueryRow(`SELECT day, gates, place, run, started_at FROM race WHERE pond_id = ?`, id).
		Scan(&r.day, &r.gates, &r.place, &r.run, &r.startedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	return r, err
}

func saveRace(tx *sql.Tx, id int64, r raceRow) error {
	_, err := tx.Exec(`INSERT INTO race (pond_id, day, gates, place, run, started_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (pond_id) DO UPDATE SET day = excluded.day, gates = excluded.gates, place = excluded.place,
		run = excluded.run, started_at = excluded.started_at`,
		id, r.day, r.gates, r.place, r.run, r.startedAt)
	return err
}

// raced is whether this row's run is on today's board.
func (r raceRow) raced(day string) bool { return r.day == day && r.place > 0 }

func raceSeed(day string) uint32 { return dayHash(day, "race course") }

type raceRival struct {
	Name  string `json:"name"`
	Seed  uint32 `json:"seed"`
	Hat   int    `json:"hat"`
	Gates int    `json:"gates"`
}

// raceBoard is how far each resident got on the day's course, the same in
// every pond. Rush beats all but the best of the rest every day.
func raceBoard(day string) []raceRival {
	out := make([]raceRival, len(residents))
	var others []int
	rush := 0
	for i, r := range residents {
		out[i] = raceRival{r.name, r.seed, r.hat, 3 + int(dayHash(day, "race "+r.name)%18)}
		if r.name == "Rush" {
			rush = i
		} else {
			others = append(others, out[i].Gates)
		}
	}
	slices.Sort(others)
	out[rush].Gates = others[len(others)-2] + 1 + int(dayHash(day, "race Rush")%2)
	return out
}

// racePlace is where gates comes on the board, a tie going to the pond.
func racePlace(gates int, board []raceRival) int {
	place := 1
	for _, r := range board {
		if r.Gates > gates {
			place++
		}
	}
	return place
}

type raceView struct {
	Board []raceRival `json:"board"`
	// Gates and Place are the pond's run on today's board, once it has one.
	Gates int `json:"gates"`
	Place int `json:"place"`
	Of    int `json:"of"`
}

func raceViewOf(q querier, id int64, now time.Time) (raceView, error) {
	day := dayOf(now)
	v := raceView{Board: raceBoard(day), Of: len(residents) + 1}
	r, err := loadRace(q, id)
	if err != nil || !r.raced(day) {
		return v, err
	}
	v.Gates, v.Place = r.gates, r.place
	return v, nil
}

type raceStarted struct {
	Run  string   `json:"run"`
	Seed uint32   `json:"seed"`
	Race raceView `json:"race"`
	Live bool     `json:"live"`
}

// raceStart hands out a run and the day's course. Live says this run goes
// on the board if it finishes, and a later one is practice.
func (s *store) raceStart(id int64) (*raceStarted, error) {
	out := &raceStarted{Run: randomHex(8)}
	_, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		r, err := loadRace(tx, p.ID)
		if err != nil {
			return err
		}
		day := dayOf(now)
		if r.day != day {
			r = raceRow{day: day}
		}
		out.Live, out.Seed = !r.raced(day), raceSeed(day)
		if out.Race, err = raceViewOf(tx, p.ID, now); err != nil {
			return err
		}
		r.run, r.startedAt = out.Run, now.Unix()
		return saveRace(tx, p.ID, r)
	})
	return out, err
}

type raceDone struct {
	Gates int  `json:"gates"`
	Place int  `json:"place"`
	Of    int  `json:"of"`
	Live  bool `json:"live"`
	// Prize is what a place on the board's podium paid, inside Dive's cap.
	Prize  int      `json:"prize"`
	Rested int      `json:"rested"`
	Race   raceView `json:"race"`
	Pond   *view    `json:"pond"`
}

func (s *store) raceFinish(id int64, run string, score, gates int) (*raceDone, error) {
	out := &raceDone{Of: len(residents) + 1}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		r, err := loadRace(tx, p.ID)
		if err != nil {
			return err
		}
		if run == "" || run != r.run {
			return errBadRun
		}
		took := now.Sub(time.Unix(r.startedAt, 0)) + time.Second
		r.run, r.startedAt = "", 0
		day := dayOf(now)
		if r.day != day {
			r = raceRow{day: day}
		}
		// The run is spent either way, and a gate count the course can't
		// have given in the time is no run at all.
		if gates < 0 || gates > score || gates > raceMost || float64(gates) > took.Seconds()*raceGatesASec+1 ||
			!divePlausible(score, took) {
			return saveRace(tx, p.ID, r)
		}
		board := raceBoard(day)
		out.Gates, out.Place = gates, racePlace(gates, board)
		if err := addFlow(tx, p, "played", 1); err != nil {
			return err
		}
		if !r.raced(day) {
			out.Live = true
			r.gates, r.place = gates, out.Place
			if out.Place <= len(racePrize) {
				out.Prize, out.Rested = p.payRun(racePrize[out.Place-1], diveCap-p.DivePaid, now)
				p.DivePaid += out.Prize
				p.Pebbles += out.Prize
			} else {
				p.PlayedAt = now.Unix()
			}
		}
		if err := saveRace(tx, p.ID, r); err != nil {
			return err
		}
		out.Race, err = raceViewOf(tx, p.ID, now)
		return err
	})
	out.Pond = v
	return out, err
}
