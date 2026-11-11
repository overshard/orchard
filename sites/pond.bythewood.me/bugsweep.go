package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Bug sweep is a grid of lily pads with wasps under some. The day's grid is
// the same in every pond and pays once, and every go after it is a fresh
// grid for nothing. The page only ever sends a pad, and the board is here.
const (
	bugCols, bugRows = 8, 10
	bugPads          = bugCols * bugRows
	bugWasps         = 12
	bugSafe          = bugPads - bugWasps
	// Clearing the day's grid pays bugClear, and a go a wasp ended pays its
	// share of bugPart for the pads it opened.
	bugClear, bugPart = 32, 16
)

const (
	bugNone = iota
	bugPlay
	bugLost
	bugWon
)

var bugStates = [...]string{"", "play", "lost", "won"}

var errBadTap = errors.New("that pad is already open")

// bugSeed is what the day's grid is dealt from. It's keyed with the pepper
// so the grid can't be worked out from the source and the date.
func (s *store) bugSeed(day string) string {
	mac := hmac.New(sha256.New, s.pepper)
	mac.Write([]byte("bug sweep " + day))
	return hex.EncodeToString(mac.Sum(nil))
}

func bugNear(i int, fn func(j int)) {
	c, r := i%bugCols, i/bugCols
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			if nc, nr := c+dc, r+dr; (dc != 0 || dr != 0) && nc >= 0 && nc < bugCols && nr >= 0 && nr < bugRows {
				fn(nr*bugCols + nc)
			}
		}
	}
}

type bugBoard struct {
	wasp, open [bugPads]bool
	opened     int
	state      int
}

// bugDeal shuffles the pads off a seed and puts a wasp under the first few.
// A wasp on the first pad tapped or next to it moves to the next pad in that
// shuffle clear of both, so the same seed and first pad always deal the same.
func bugDeal(seed string, first int) *bugBoard {
	sum := sha256.Sum256([]byte(seed))
	x := binary.LittleEndian.Uint64(sum[:8]) | 1
	order := make([]int, bugPads)
	for i := range order {
		order[i] = i
	}
	for i := bugPads - 1; i > 0; i-- {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		j := int(x % uint64(i+1))
		order[i], order[j] = order[j], order[i]
	}
	b := &bugBoard{state: bugPlay}
	for _, i := range order[:bugWasps] {
		b.wasp[i] = true
	}
	var clear [bugPads]bool
	clear[first] = true
	bugNear(first, func(j int) { clear[j] = true })
	spare := order[bugWasps:]
	for _, i := range order[:bugWasps] {
		if !clear[i] {
			continue
		}
		for clear[spare[0]] {
			spare = spare[1:]
		}
		b.wasp[i], b.wasp[spare[0]] = false, true
		spare = spare[1:]
	}
	return b
}

func (b *bugBoard) near(i int) int {
	n := 0
	bugNear(i, func(j int) {
		if b.wasp[j] {
			n++
		}
	})
	return n
}

// tap opens a pad and, where no wasp is next door, every pad round it. It
// gives back what opened as pad and count pairs, and refuses a pad that's
// open already or any pad once the go is over.
func (b *bugBoard) tap(i int) ([][2]int, error) {
	if b.state != bugPlay {
		return nil, errBadRun
	}
	if i < 0 || i >= bugPads || b.open[i] {
		return nil, errBadTap
	}
	if b.wasp[i] {
		b.state = bugLost
		return nil, nil
	}
	var out [][2]int
	todo := []int{i}
	b.open[i] = true
	for len(todo) > 0 {
		at := todo[0]
		todo = todo[1:]
		n := b.near(at)
		out = append(out, [2]int{at, n})
		b.opened++
		if n > 0 {
			continue
		}
		bugNear(at, func(j int) {
			if !b.open[j] {
				b.open[j] = true
				todo = append(todo, j)
			}
		})
	}
	if b.opened == bugSafe {
		b.state = bugWon
	}
	return out, nil
}

// bugRow is a pond's go in play and how the day's paid go went.
type bugRow struct {
	day string
	// The go in play, the seed its grid is dealt from and its taps in order.
	run, seed, taps string
	daily           bool
	startedAt       int64
	// How the day's paid go ended, with none meaning it's still to play.
	dayState, daySecs, dayTaps, dayOpen int
}

func loadBugs(q querier, id int64) (bugRow, error) {
	var r bugRow
	err := q.QueryRow(`SELECT day, run, seed, taps, daily, started_at, day_state, day_secs, day_taps, day_open FROM bugsweep WHERE pond_id = ?`, id).
		Scan(&r.day, &r.run, &r.seed, &r.taps, &r.daily, &r.startedAt, &r.dayState, &r.daySecs, &r.dayTaps, &r.dayOpen)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	return r, err
}

func saveBugs(tx *sql.Tx, id int64, r bugRow) error {
	_, err := tx.Exec(`INSERT INTO bugsweep (pond_id, day, run, seed, taps, daily, started_at, day_state, day_secs, day_taps, day_open) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (pond_id) DO UPDATE SET day = excluded.day, run = excluded.run, seed = excluded.seed, taps = excluded.taps, daily = excluded.daily,
		started_at = excluded.started_at, day_state = excluded.day_state, day_secs = excluded.day_secs, day_taps = excluded.day_taps, day_open = excluded.day_open`,
		id, r.day, r.run, r.seed, r.taps, r.daily, r.startedAt, r.dayState, r.daySecs, r.dayTaps, r.dayOpen)
	return err
}

// settled starts a new day clean. A go left unfinished overnight is gone
// with its day, since the grid it was on isn't today's.
func (r bugRow) settled(now time.Time) bugRow {
	if d := dayOf(now); r.day != d {
		return bugRow{day: d}
	}
	return r
}

func (r bugRow) pads() []int {
	var out []int
	for _, t := range strings.Fields(r.taps) {
		if n, err := strconv.Atoi(t); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// replay plays a go's taps back, so the board is never stored, and gives
// what's open as pad and count pairs.
func (r bugRow) replay() (*bugBoard, [][2]int) {
	taps := r.pads()
	if len(taps) == 0 {
		return nil, nil
	}
	b := bugDeal(r.seed, taps[0])
	var open [][2]int
	for _, t := range taps {
		got, _ := b.tap(t)
		open = append(open, got...)
	}
	return b, open
}

type bugGo struct {
	Run   string `json:"run"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	Wasps int    `json:"wasps"`
	// Daily is the day's paid go, and anything else is a free one.
	Daily bool `json:"daily"`
	// Open and Secs are where a go that was left and come back to stands.
	Open [][2]int `json:"open"`
	Secs int      `json:"secs"`
}

// bugStart hands back the go in play, or deals a new one, the day's grid
// first and a fresh one each time after that.
func (s *store) bugStart(id int64) (*bugGo, error) {
	out := &bugGo{Cols: bugCols, Rows: bugRows, Wasps: bugWasps, Open: [][2]int{}}
	err := s.write(func(tx *sql.Tx) error {
		now := s.now()
		r, err := loadBugs(tx, id)
		if err != nil {
			return err
		}
		r = r.settled(now)
		if r.run == "" {
			r.run, r.taps, r.startedAt = randomHex(8), "", 0
			r.daily = r.dayState == bugNone
			r.seed = randomHex(8)
			if r.daily {
				r.seed = s.bugSeed(r.day)
				r.dayState = bugPlay
			}
		}
		out.Run, out.Daily = r.run, r.daily
		if _, open := r.replay(); open != nil {
			out.Open, out.Secs = open, int(now.Unix()-r.startedAt)
		}
		return saveBugs(tx, id, r)
	})
	return out, err
}

type bugTapped struct {
	Open  [][2]int `json:"open"`
	State string   `json:"state"`
	// The rest is only sent once the go is over.
	Wasps  []int `json:"wasps,omitempty"`
	Hit    int   `json:"hit"`
	Secs   int   `json:"secs,omitempty"`
	Taps   int   `json:"taps,omitempty"`
	Pads   int   `json:"pads,omitempty"`
	Of     int   `json:"of,omitempty"`
	Daily  bool  `json:"daily,omitempty"`
	Paid   int   `json:"paid"`
	Rested int   `json:"rested"`
	Best   bool  `json:"best"`
	Pond   *view `json:"pond,omitempty"`
}

var errBugOver = errors.New("that tap ends the go")

// bugStep taps one pad of the go in play and keeps the tap, without paying.
func bugStep(tx *sql.Tx, id int64, run string, pad int, now time.Time) (bugRow, *bugBoard, [][2]int, error) {
	r, err := loadBugs(tx, id)
	if err != nil {
		return r, nil, nil, err
	}
	r = r.settled(now)
	if run == "" || run != r.run {
		return r, nil, nil, errBadRun
	}
	b, _ := r.replay()
	if b == nil {
		if pad < 0 || pad >= bugPads {
			return r, nil, nil, errBadTap
		}
		b = bugDeal(r.seed, pad)
		r.startedAt = now.Unix()
	}
	open, err := b.tap(pad)
	if err != nil {
		return r, nil, nil, err
	}
	r.taps = strings.TrimSpace(r.taps + " " + strconv.Itoa(pad))
	return r, b, open, nil
}

// bugTap opens one pad. A tap that leaves the go in play only touches the
// go, and the one that ends it goes through act to pay and build the pond.
func (s *store) bugTap(id int64, run string, pad int) (*bugTapped, error) {
	out := &bugTapped{State: bugStates[bugPlay], Hit: -1}
	err := s.write(func(tx *sql.Tx) error {
		r, b, open, err := bugStep(tx, id, run, pad, s.now())
		if err != nil {
			return err
		}
		if b.state != bugPlay {
			return errBugOver
		}
		out.Open = open
		return saveBugs(tx, id, r)
	})
	if !errors.Is(err, errBugOver) {
		return out, err
	}
	g, _ := arcadeOf("sweep")
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		r, b, open, err := bugStep(tx, id, run, pad, now)
		if err != nil {
			return err
		}
		out.Open, out.State, out.Daily = open, bugStates[b.state], r.daily
		if out.Open == nil {
			out.Open = [][2]int{}
		}
		if b.state == bugLost {
			out.Hit = pad
		}
		for i, w := range b.wasp {
			if w {
				out.Wasps = append(out.Wasps, i)
			}
		}
		out.Secs, out.Taps, out.Pads, out.Of = int(now.Unix()-r.startedAt), len(r.pads()), b.opened, bugSafe
		a, err := loadArcade(tx, id, g.key)
		if err != nil {
			return err
		}
		a = a.settled(now)
		if r.daily {
			pay := bugClear
			if b.state == bugLost {
				pay = bugPart * b.opened / bugSafe
			}
			out.Paid, out.Rested = p.payRun(pay, g.cap-a.paid, now)
			a.runs++
			a.paid += out.Paid
			p.Pebbles += out.Paid
			r.dayState, r.daySecs, r.dayTaps, r.dayOpen = b.state, out.Secs, out.Taps, b.opened
		}
		a.plays++
		if b.opened > a.best {
			a.best, out.Best = b.opened, true
		}
		r.run, r.seed, r.taps, r.daily, r.startedAt = "", "", "", false, 0
		if err := saveArcade(tx, id, g.key, a); err != nil {
			return err
		}
		return saveBugs(tx, id, r)
	})
	out.Pond = v
	return out, err
}

// bugNote is the day's bug sweep as the paper and the share card tell it.
type bugNote struct {
	// State is empty until the day's go is started, then play, lost or won.
	State string `json:"state"`
	Secs  int    `json:"secs,omitempty"`
	Taps  int    `json:"taps,omitempty"`
	Pads  int    `json:"pads,omitempty"`
	Of    int    `json:"of"`
}

func bugNoteOf(q querier, id int64, now time.Time) (*bugNote, error) {
	r, err := loadBugs(q, id)
	if err != nil {
		return nil, err
	}
	r = r.settled(now)
	return &bugNote{bugStates[r.dayState], r.daySecs, r.dayTaps, r.dayOpen, bugSafe}, nil
}
