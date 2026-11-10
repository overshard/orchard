package main

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"time"
)

type greeting struct {
	First   bool        `json:"first"`
	Line    string      `json:"line"`
	Missed  [][2]string `json:"missed"`
	Present *prize      `json:"present"`
	Pond    *view       `json:"pond"`
}

func plural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// greet is the one call a visit makes. A present is handed over when the last
// one was long enough ago and stamped there and then, so a reload finds none.
func (s *store) greet(id int64, first bool) (*greeting, error) {
	g := &greeting{First: first, Missed: [][2]string{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		gen := newGenome(p.Seed)
		hatched := time.Unix(p.HatchedAt, 0)
		stage, _ := stageOf(now.Sub(hatched))
		away := now.Sub(time.Unix(p.LastSeenAt, 0))

		if first {
			g.Line = "Hello. This one's yours. Its name is " + titled(gen.Name) + " and it has decided it likes you."
			g.Missed = [][2]string{
				{"Egg", "laid just now"},
				{"Purse", plural(starterPebbles, "pebble") + " to start"},
				{"Wheel", "one free spin waiting"},
			}
			return nil
		}

		g.Line = greetLine(gen, stage, away)
		if was, _ := stageOf(time.Unix(p.LastSeenAt, 0).Sub(hatched)); was != stage && away > 0 {
			g.Missed = append(g.Missed, [2]string{"Grew", "from " + was + " to " + stage})
		}
		if p.FreeSpins > 0 {
			g.Missed = append(g.Missed, [2]string{"Wheel", plural(p.FreeSpins, "free spin") + " saved"})
		}
		if p.LogDay != dayOf(now) {
			g.Missed = append(g.Missed, [2]string{"Old log", "not turned over yet today"})
		}

		if gap := now.Sub(time.Unix(p.LastGiftAt, 0)); gap >= giftGap {
			z := rollGift(gap)
			if err := award(tx, p, &z); err != nil {
				return err
			}
			g.Present = &z
			p.LastGiftAt = now.Unix()
		}
		p.LastSeenAt = now.Unix()
		return nil
	})
	g.Pond = v
	return g, err
}

type spun struct {
	Slice int   `json:"slice"`
	Prize prize `json:"prize"`
	Pond  *view `json:"pond"`
}

// spin picks the slice here and the page only animates to it. Every slice is
// a prize, and the smallest one hands a bought spin its pebbles back.
func (s *store) spin(id int64, paid bool) (*spun, error) {
	out := &spun{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if paid {
			if p.PaidSpins >= paidSpinsADay {
				return errNotYet
			}
			if p.Pebbles < paidSpinCost {
				return errPoor
			}
			p.Pebbles -= paidSpinCost
			p.PaidSpins++
		} else {
			if p.FreeSpins < 1 {
				return errNotYet
			}
			// A full pond was not counting toward the next spin, so the wait
			// for it starts now.
			if p.FreeSpins >= maxFreeSpins {
				p.SpinAt = now.Unix()
			}
			p.FreeSpins--
		}
		out.Slice = rand.IntN(len(wheelSlices))
		out.Prize = wheelPrize(wheelSlices[out.Slice])
		if err := award(tx, p, &out.Prize); err != nil {
			return err
		}
		if paid && out.Prize.Pebbles == paidSpinCost {
			out.Prize.Text = "25 pebbles, so that one was free."
		}
		return nil
	})
	out.Pond = v
	return out, err
}

type opened struct {
	Prize prize `json:"prize"`
	Pond  *view `json:"pond"`
}

func (s *store) box(id int64) (*opened, error) {
	out := &opened{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if p.Pebbles < boxCost {
			return errPoor
		}
		p.Pebbles -= boxCost
		h := rollBox()
		out.Prize = prize{Hat: &h}
		return award(tx, p, &out.Prize)
	})
	out.Pond = v
	return out, err
}

// wear puts a found hat on the frog out front. Shape 0 takes the hat off.
func (s *store) wear(id int64, h hat) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if h.Shape == 0 {
			p.Wearing = hat{}
			return nil
		}
		var count int
		err := tx.QueryRow(`SELECT count FROM hats WHERE pond_id = ? AND shape = ? AND colour = ?`,
			p.ID, h.Shape, h.Colour).Scan(&count)
		if err == sql.ErrNoRows || (err == nil && count < 1) {
			return errNotHeld
		}
		if err != nil {
			return err
		}
		p.Wearing = h
		return nil
	})
}

// sell lets Wren have a spare. The last copy of a hat is never a spare, so
// nothing leaves the collection.
func (s *store) sell(id int64, h hat) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		res, err := tx.Exec(`UPDATE hats SET count = count - 1
			WHERE pond_id = ? AND shape = ? AND colour = ? AND count > 1`, p.ID, h.Shape, h.Colour)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotHeld
		}
		p.Pebbles += wrenPays(h)
		return nil
	})
}

type turned struct {
	Finds [3]find `json:"finds"`
	Pick  int     `json:"pick"`
	Pond  *view   `json:"pond"`
}

// turnLog pays once a day for one log of three.
func (s *store) turnLog(id int64, pick int) (*turned, error) {
	out := &turned{Pick: pick}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		today := dayOf(now)
		if p.LogDay == today {
			return errNotYet
		}
		p.LogDay = today
		for i := range out.Finds {
			out.Finds[i] = rollFind()
		}
		p.Pebbles += out.Finds[pick].Pebbles
		return nil
	})
	out.Pond = v
	return out, err
}

func (s *store) crack(id int64) (*opened, error) {
	out := &opened{}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		res, err := tx.Exec(`UPDATE items SET count = count - 1
			WHERE pond_id = ? AND kind = 'geode' AND count > 0`, p.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotHeld
		}
		out.Prize = rollGeode()
		return award(tx, p, &out.Prize)
	})
	out.Pond = v
	return out, err
}

// diveStart hands out a run id that finish has to bring back, once.
func (s *store) diveStart(id int64) (string, error) {
	run := randomHex(8)
	_, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		p.DiveRun, p.DiveStart = run, now.Unix()
		return nil
	})
	return run, err
}

type dived struct {
	Score int   `json:"score"`
	Paid  int   `json:"paid"`
	Best  bool  `json:"best"`
	Pond  *view `json:"pond"`
}

func (s *store) diveFinish(id int64, run string, score int) (*dived, error) {
	out := &dived{Score: score}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		if run == "" || run != p.DiveRun {
			return errBadRun
		}
		took := now.Sub(time.Unix(p.DiveStart, 0))
		p.DiveRun, p.DiveStart = "", 0
		// The run id is spent either way, so the save below has to happen and
		// an impossible score just pays nothing.
		if !divePlausible(score, took+time.Second) {
			out.Score = 0
			return nil
		}
		p.DiveRuns++
		out.Paid = min(divePays(score, p.DiveRuns), diveCap-p.DivePaid)
		p.DivePaid += out.Paid
		p.Pebbles += out.Paid
		if score > p.DiveBest {
			p.DiveBest, out.Best = score, true
		}
		return nil
	})
	out.Pond = v
	return out, err
}
