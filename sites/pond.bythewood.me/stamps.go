package main

import (
	"database/sql"
	"errors"
	"time"
)

// A frog's share page can be stamped by a visitor with a pond of its own,
// once a day a page, and the owner sees who at its next greeting. Nothing is
// typed, so a stamp is one of these and a frog's look and name.
var stampKinds = []string{"heart", "fly", "wave", "star", "leaf", "pebble"}

const (
	// Pages one pond can stamp in a New York day.
	stampsADay = 12
	// Stamps a page holds until its pond opens, so a crowd can't pile up
	// rows for a pond nobody comes back to.
	stampsHeld = 60
)

var (
	errOwnPage    = errors.New("that's your own frog's page")
	errStamped    = errors.New("you've stamped this page today")
	errStampsDone = errors.New("that's all the stamping for today, there's more tomorrow")
	errStampsFull = errors.New("this frog's stamp book is full until it comes home")
)

type stampState struct {
	// State is none with no pond, own, open, done or spent.
	State string `json:"state"`
	// Stamp is today's, once it's done.
	Stamp string `json:"stamp,omitempty"`
}

func pageID(q querier, publicID string) (int64, error) {
	var id int64
	err := q.QueryRow(`SELECT id FROM ponds WHERE public_id = ?`, publicID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNoPond
	}
	return id, err
}

func stampOf(q querier, from, page int64, day string) (stampState, error) {
	if from == 0 {
		return stampState{State: "none"}, nil
	}
	if from == page {
		return stampState{State: "own"}, nil
	}
	var kind int
	err := q.QueryRow(`SELECT stamp FROM stamps WHERE page_id = ? AND from_id = ? AND day = ?`, page, from, day).Scan(&kind)
	if err == nil && kind >= 0 && kind < len(stampKinds) {
		return stampState{State: "done", Stamp: stampKinds[kind]}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return stampState{}, err
	}
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM stamps WHERE from_id = ? AND day = ?`, from, day).Scan(&n); err != nil {
		return stampState{}, err
	}
	if n >= stampsADay {
		return stampState{State: "spent"}, nil
	}
	return stampState{State: "open"}, nil
}

// stampsFor is what a share page asks once it's loaded, since the page
// itself is cached at the edge for everybody. from is 0 with no pond.
func (s *store) stampsFor(from int64, publicID string) (stampState, error) {
	page, err := pageID(s.r, publicID)
	if err != nil {
		return stampState{}, err
	}
	return stampOf(s.r, from, page, dayOf(s.now()))
}

func (s *store) stamp(from int64, publicID, kind string) (stampState, error) {
	k := -1
	for i, sk := range stampKinds {
		if sk == kind {
			k = i
		}
	}
	if k < 0 {
		return stampState{}, errNotHeld
	}
	var out stampState
	err := s.write(func(tx *sql.Tx) error {
		page, err := pageID(tx, publicID)
		if err != nil {
			return err
		}
		day := dayOf(s.now())
		st, err := stampOf(tx, from, page, day)
		if err != nil {
			return err
		}
		switch st.State {
		case "own":
			return errOwnPage
		case "done":
			return errStamped
		case "spent":
			return errStampsDone
		}
		var held int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM stamps WHERE page_id = ? AND seen = 0`, page).Scan(&held); err != nil {
			return err
		}
		if held >= stampsHeld {
			return errStampsFull
		}
		if _, err := tx.Exec(`INSERT INTO stamps (page_id, from_id, day, stamp) VALUES (?, ?, ?, ?)`, page, from, day, k); err != nil {
			return err
		}
		out = stampState{State: "done", Stamp: kind}
		return nil
	})
	return out, err
}

type stampView struct {
	Stamp string `json:"stamp"`
	// From is nil for a pond that keeps its frog home.
	From *caller `json:"from"`
}

// stampsIn hands over the stamps a pond hasn't seen and marks them seen.
func stampsIn(tx *sql.Tx, p *pond, now time.Time) ([]stampView, error) {
	rows, err := tx.Query(`SELECT from_id, stamp FROM stamps WHERE page_id = ? AND seen = 0 ORDER BY day, from_id`, p.ID)
	if err != nil {
		return nil, err
	}
	type row struct {
		from int64
		kind int
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.from, &r.kind); err != nil {
			rows.Close()
			return nil, err
		}
		got = append(got, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := []stampView{}
	for _, r := range got {
		if r.kind < 0 || r.kind >= len(stampKinds) {
			continue
		}
		c, err := frontOf(tx, r.from, now)
		if err != nil {
			return nil, err
		}
		out = append(out, stampView{Stamp: stampKinds[r.kind], From: c})
	}
	_, err = tx.Exec(`UPDATE stamps SET seen = 1 WHERE page_id = ? AND seen = 0`, p.ID)
	return out, err
}
