package main

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// A held change is an agent's edit to a rule that's enforcing, kept until
// Isaac applies or drops it on the overview. Only a note goes straight in, so
// a prompt injected edit can't quietly switch a rule off.
const heldSchema = `
CREATE TABLE IF NOT EXISTS held (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    at     INTEGER NOT NULL,
    rule   INTEGER NOT NULL,
    name   TEXT    NOT NULL,
    why    TEXT    NOT NULL,
    before TEXT    NOT NULL,
    after  TEXT    NOT NULL
);
`

// maxHeld bounds the changes that can wait unseen, like maxWaiting.
const maxHeld = 20

type Held struct {
	ID     int64
	At     time.Time
	RuleID int64
	Name   string
	Why    string
	Change Change
}

// holds says whether an agent's edit has to wait for Isaac. A rule that
// refuses nothing yet can change at once, and so can its note.
func holds(old, rule Rule) bool {
	if old.Watching || old.Pending {
		return false
	}
	rule.Note = old.Note
	return snapOf(&rule) != snapOf(&old)
}

func (s *Store) Hold(old, rule Rule, why string) (int64, error) {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM held`).Scan(&n)
	if n >= maxHeld {
		return 0, errTooManyHeld
	}
	res, err := s.db.Exec(`INSERT INTO held (at, rule, name, why, before, after) VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().Unix(), old.ID, old.Name, why, snapOf(&old), snapOf(&rule))
	if err != nil {
		return 0, err
	}
	s.Change("claude", old.Name, "asked for a change, waiting on you, why: "+why)
	return res.LastInsertId()
}

var errTooManyHeld = errors.New("too many changes are already waiting on Isaac")

func scanHeld(sc interface{ Scan(...any) error }) (Held, error) {
	var h Held
	var at int64
	err := sc.Scan(&h.ID, &at, &h.RuleID, &h.Name, &h.Why, &h.Change.Before, &h.Change.After)
	h.At = fromUnix(at)
	h.Change.At, h.Change.Rule, h.Change.Who = h.At, h.Name, "claude"
	return h, err
}

func (s *Store) HeldChanges() ([]Held, error) {
	rows, err := s.db.Query(`SELECT id, at, rule, name, why, before, after FROM held ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Held
	for rows.Next() {
		h, err := scanHeld(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) HeldChange(id int64) (Held, error) {
	h, err := scanHeld(s.db.QueryRow(`SELECT id, at, rule, name, why, before, after FROM held WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return h, errNoRule
	}
	return h, err
}

func (s *Store) Unhold(id int64) {
	_, _ = s.db.Exec(`DELETE FROM held WHERE id = ?`, id)
}

// heldOp applies or drops a held change. One made against a version of the
// rule that has since changed is dropped, since it no longer says what it did.
func (s *site) heldOp(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	h, err := s.store.HeldChange(id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	done := func(msg string) {
		http.Redirect(w, r, "/?msg="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	switch r.PathValue("op") {
	case "drop":
		s.store.Unhold(h.ID)
		s.store.Change("you", h.Name, "dropped claude's change")
		done("Dropped the change to " + h.Name)
		return
	case "apply":
	default:
		s.notFound(w, r)
		return
	}
	cur, err := s.store.Rule(h.RuleID)
	if err != nil || snapOf(&cur) != h.Change.Before {
		s.store.Unhold(h.ID)
		s.store.Change("you", h.Name, "dropped claude's change, the rule changed after it was asked for")
		done(h.Name + " changed after Claude asked, so its change was dropped")
		return
	}
	rule, ok := ruleOfSnap(h.Change.After)
	if !ok {
		s.store.Unhold(h.ID)
		done("That change couldn't be read, so it was dropped")
		return
	}
	rule.ID, rule.Author, rule.Created, rule.Pending = cur.ID, cur.Author, cur.Created, cur.Pending
	if lock := s.needsConfirm(r, &rule); len(lock) > 0 {
		done("That change to " + h.Name + " would catch signing in, so make it from the rule's own page")
		return
	}
	if err := s.store.UpdateRuleWhy(&rule, "claude", h.Why+", applied by you"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.store.Unhold(h.ID)
	_ = s.reload()
	done("Applied the change to " + h.Name)
}
