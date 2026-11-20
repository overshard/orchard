package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// snap is a rule as the change log keeps it, everything a restore needs.
type snap struct {
	Name     string `json:"name"`
	Note     string `json:"note,omitempty"`
	When     string `json:"when"`
	Action   string `json:"action"`
	Status   int    `json:"status,omitempty"`
	Message  string `json:"message,omitempty"`
	Rate     int    `json:"rate,omitempty"`
	BanAfter int    `json:"ban_after,omitempty"`
	BanFor   int64  `json:"ban_for,omitempty"`
	Quiet    bool   `json:"quiet,omitempty"`
	Watching bool   `json:"watching,omitempty"`
	Expires  int64  `json:"expires,omitempty"`
}

func snapOf(r *Rule) string {
	if r == nil {
		return ""
	}
	b, _ := json.Marshal(snap{Name: r.Name, Note: r.Note, When: r.When, Action: r.Action, Status: r.Status, Message: r.Message,
		Rate: r.Rate, BanAfter: r.BanAfter, BanFor: int64(r.BanFor / time.Second), Quiet: r.Quiet, Watching: r.Watching, Expires: unix(r.Expires)})
	return string(b)
}

func ruleOfSnap(raw string) (Rule, bool) {
	var x snap
	if raw == "" || json.Unmarshal([]byte(raw), &x) != nil || x.Name == "" {
		return Rule{}, false
	}
	return Rule{Name: x.Name, Note: x.Note, When: x.When, Action: x.Action, Status: x.Status, Message: x.Message, Rate: x.Rate,
		BanAfter: x.BanAfter, BanFor: time.Duration(x.BanFor) * time.Second, Quiet: x.Quiet, Watching: x.Watching, Expires: fromUnix(x.Expires)}, true
}

// ruleLines is a rule written out a line a setting, the way the log compares
// two versions of it.
func ruleLines(r Rule) []string {
	out := []string{"then " + r.Then()}
	if r.Watching {
		out = append(out, "only watching")
	}
	if r.Message != "" {
		out = append(out, "says "+r.Message)
	}
	if r.BanAfter > 0 {
		out = append(out, fmt.Sprintf("bans after %d for %dh", r.BanAfter, int(r.BanFor/time.Hour)))
	}
	if r.Quiet {
		out = append(out, "kept out of the access log")
	}
	if !r.Expires.IsZero() {
		out = append(out, "ends "+r.Expires.UTC().Format("2006-01-02 15:04"))
	}
	if r.Note != "" {
		out = append(out, "note "+r.Note)
	}
	return append(out, lines(r.When)...)
}

// DiffLine is one line of a change, Op "-" for gone, "+" for new, " " for kept.
type DiffLine struct{ Op, Text string }

// Diff is the lines that changed between before and after, with the lines that
// stayed left out unless nothing changed at all.
func (c Change) Diff() []DiffLine {
	b, hasB := ruleOfSnap(c.Before)
	a, hasA := ruleOfSnap(c.After)
	var bl, al []string
	if hasB {
		bl = ruleLines(b)
	}
	if hasA {
		al = ruleLines(a)
	}
	in := func(l []string, x string) bool {
		for _, y := range l {
			if y == x {
				return true
			}
		}
		return false
	}
	var out []DiffLine
	for _, x := range bl {
		if !in(al, x) {
			out = append(out, DiffLine{"-", x})
		}
	}
	for _, x := range al {
		if !in(bl, x) {
			out = append(out, DiffLine{"+", x})
		}
	}
	return out
}

// Restorable is a change with an older version of a rule to go back to.
func (c Change) Restorable() bool {
	_, ok := ruleOfSnap(c.Before)
	return ok
}

// restore puts a rule back the way it was before a change, which is itself a
// change. A version that would catch signing in comes back watching.
func (s *site) restore(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	c, err := s.store.ChangeByID(id)
	old, ok := ruleOfSnap(c.Before)
	if err != nil || !ok {
		s.notFound(w, r)
		return
	}
	if !old.Expires.IsZero() && old.Expires.Before(time.Now()) {
		old.Expires = time.Time{}
	}
	note := ""
	if !old.Watching && old.validate() == nil && len(lockout(&old)) > 0 {
		old.Watching = true
		note = ", watching since it would catch signing in"
	}
	who := "you"
	if cur, err := s.store.RuleNamed(old.Name); err == nil {
		old.ID, old.Author, old.Created = cur.ID, cur.Author, cur.Created
		err = s.store.UpdateRule(&old, who)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		old.Author = who
		if err := s.store.AddRule(&old, who); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	_ = s.reload()
	msg := "Restored " + old.Name + " to before change " + strconv.FormatInt(c.ID, 10) + note
	http.Redirect(w, r, "/rules?msg="+url.QueryEscape(msg)+"#changes", http.StatusSeeOther)
}
