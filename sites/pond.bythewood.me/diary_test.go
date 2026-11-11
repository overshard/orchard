package main

import (
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"
)

func diaryAll(t *testing.T, s *store, id int64) []diaryRow {
	t.Helper()
	rows, err := s.r.Query(`SELECT day, line FROM diary WHERE pond_id = ? ORDER BY day`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []diaryRow
	for rows.Next() {
		var r diaryRow
		if err := rows.Scan(&r.Day, &r.Line); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// kindOf finds the pool a written line came off, with its slots put back.
func kindOf(line string) string {
	for kind, pool := range diaryLines {
		all := append([]string{}, pool.any...)
		for _, ls := range pool.by {
			all = append(all, ls...)
		}
		for _, l := range all {
			re := "^" + regexp.MustCompile(`\\\{[a-z_]+\\\}`).ReplaceAllString(regexp.QuoteMeta(l), ".+") + "$"
			if regexp.MustCompile(re).MatchString(line) {
				return kind
			}
		}
	}
	return ""
}

func TestTheDiaryIsOneLineADay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if got := diaryAll(t, s, id); len(got) != 0 {
		t.Fatalf("a diary on the day it was laid: %v", got)
	}
	for range 3 {
		*clock = clock.Add(time.Hour)
		s.greet(id, false)
	}
	if got := diaryAll(t, s, id); len(got) != 0 {
		t.Fatalf("a line before there's been a yesterday: %v", got)
	}
	*clock = clock.Add(24 * time.Hour)
	for range 3 {
		if _, err := s.greet(id, false); err != nil {
			t.Fatal(err)
		}
	}
	got := diaryAll(t, s, id)
	if len(got) != 1 || got[0].Day != "2026-11-10" || kindOf(got[0].Line) != "egg" {
		t.Fatalf("after the first night: %v", got)
	}
	// Five days away is one line for the day before coming back and no more.
	*clock = clock.Add(5 * 24 * time.Hour)
	s.greet(id, false)
	got = diaryAll(t, s, id)
	if len(got) != 2 || got[1].Day != "2026-11-15" {
		t.Fatalf("after five days away: %v", got)
	}
	frog := frontFrog(t, s, id).ID
	page, err := s.diaryOf(id, frog, "")
	if err != nil || len(page.Lines) != 2 || page.Lines[0].Day != "2026-11-15" || page.More {
		t.Fatalf("the page isn't newest first: %+v %v", page, err)
	}
	if page, _ := s.diaryOf(id, frog+99, ""); len(page.Lines) != 0 {
		t.Errorf("another frog's diary has %d lines", len(page.Lines))
	}
}

func TestTheDiaryMakesNothingUp(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	// Thirty days of only turning up, with no weather read.
	for range 30 {
		*clock = clock.Add(24 * time.Hour)
		if _, err := s.greet(id, false); err != nil {
			t.Fatal(err)
		}
	}
	grew := 0
	has := func(query, day, line string) bool {
		rows, err := s.r.Query(query, id, day)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var a, b string
			rows.Scan(&a, &b)
			name := a
			if h, ok := hatOfKind("hat_" + a + "_" + b); ok {
				name = h.name()
			}
			for _, k := range keepsakes {
				if k.kind == a {
					name = k.name
				}
			}
			for _, k := range badges {
				if k.kind == a {
					name = k.name
				}
			}
			if strings.Contains(line, strings.ToLower(name)) {
				return true
			}
		}
		return false
	}
	for _, r := range diaryAll(t, s, id) {
		k := kindOf(r.Line)
		switch k {
		case "grew":
			grew++
		case "quiet", "egg":
		case "keepsake":
			if !has(`SELECT kind, '' FROM keepsakes WHERE pond_id = ? AND day = ?`, r.Day, r.Line) {
				t.Errorf("%s: no keepsake was kept that day: %s", r.Day, r.Line)
			}
		case "pin":
			if !has(`SELECT kind, '' FROM badges WHERE pond_id = ? AND day = ?`, r.Day, r.Line) {
				t.Errorf("%s: no badge was earned that day: %s", r.Day, r.Line)
			}
		case "hat":
			if !has(`SELECT shape, colour FROM hats WHERE pond_id = ? AND first_day = ?`, r.Day, r.Line) {
				t.Errorf("%s: no hat was found that day: %s", r.Day, r.Line)
			}
		default:
			t.Errorf("%s: a %q line with nothing behind it: %s", r.Day, k, r.Line)
		}
		if strings.ContainsAny(r.Line, "{}0123456789") {
			t.Errorf("%s: %s", r.Day, r.Line)
		}
	}
	// Egg to tadpole, froglet, frog and elder.
	if grew != 4 {
		t.Errorf("it wrote about growing %d times in thirty days", grew)
	}
}

func TestTheDiaryWritesWhatHappened(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	// Clear of midwinter, a full moon and the keepsakes for days at the pond.
	*clock = clock.Add(47 * 24 * time.Hour)
	s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ponds SET days_visited = 8, last_gift_at = ? WHERE id = ?`, clock.Add(30*24*time.Hour).Unix(), id)
		return err
	})
	s.greet(id, false)
	next := func() string {
		t.Helper()
		*clock = clock.Add(24 * time.Hour)
		if _, err := s.greet(id, false); err != nil {
			t.Fatal(err)
		}
		all := diaryAll(t, s, id)
		return all[len(all)-1].Line
	}
	next()

	// A first score is no record, and the day after beating it is one.
	dive(t, s, clock, id, 10)
	if l := next(); kindOf(l) != "quiet" {
		t.Errorf("a first score made the diary: %s", l)
	}
	dive(t, s, clock, id, 14)
	if l := next(); kindOf(l) != "record" || !strings.Contains(l, "14") || !strings.Contains(l, "Dive") {
		t.Errorf("after a new best: %s", l)
	}
	play(t, s, clock, id, "fish", 40)
	next()
	play(t, s, clock, id, "fish", 60)
	if l := next(); kindOf(l) != "fish" || !strings.Contains(l, "60") {
		t.Errorf("after a best at fishing: %s", l)
	}

	if _, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		return award(tx, p, &prize{Hat: &hat{5, 0}})
	}); err != nil {
		t.Fatal(err)
	}
	if l := next(); kindOf(l) != "hat" || !strings.Contains(l, "bobble hat") {
		t.Errorf("after finding a hat: %s", l)
	}
	// A spare of the same hat is nothing new.
	s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error { return award(tx, p, &prize{Hat: &hat{5, 0}}) })
	if l := next(); kindOf(l) != "quiet" {
		t.Errorf("after a spare: %s", l)
	}

	if _, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		_, err := befriend(tx, p, 2, friendAt[1], "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if l := next(); kindOf(l) != "friend" || !strings.Contains(l, residents[2].name) {
		t.Errorf("after a heart with a neighbour: %s", l)
	}

	s.day.pinned.Store(true)
	s.day.last.Store(&reading{At: *clock, Temp: 30, HasTemp: true, Cond: "Clear"})
	*clock = clock.Add(time.Hour)
	s.greet(id, false)
	s.day.last.Store(nil)
	if l := next(); kindOf(l) != "keepsake" || !strings.Contains(l, "frosted leaf") {
		t.Errorf("after a frosted leaf: %s", l)
	}
	s.day.last.Store(&reading{At: *clock, Temp: 60, HasTemp: true, Cond: "Light Rain", Rain: 80})
	*clock = clock.Add(time.Hour)
	s.greet(id, false)
	s.day.last.Store(nil)
	if l := next(); kindOf(l) != "rain" {
		t.Errorf("after a day of rain: %s", l)
	}

	for range 12 {
		if _, err := s.pet(id, "head"); err != nil {
			t.Fatal(err)
		}
	}
	if l := next(); kindOf(l) != "quiet" {
		t.Errorf("a rub short of a heart made the diary: %s", l)
	}
	s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE frogs SET bond = ? WHERE pond_id = ?`, bondHeart-petPoints, id)
		return err
	})
	if out, err := s.pet(id, "head"); err != nil || !out.Heart {
		t.Fatalf("no heart: %+v %v", out, err)
	}
	if l := next(); kindOf(l) != "heart" {
		t.Errorf("after a new heart: %s", l)
	}

	if _, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		_, err := befriend(tx, p, 4, friendAt[bestFriend], "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if l := next(); kindOf(l) != "memento" || !strings.Contains(l, "Bramble") || !strings.Contains(l, "old trowel") {
		t.Errorf("after a memento: %s", l)
	}

	v, _ := s.view(id)
	if _, err := s.setJob(id, v.Frogs[0].ID, "sticks", ""); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(3 * time.Hour)
	if _, err := s.collectJobs(id); err != nil {
		t.Fatal(err)
	}
	if l := next(); kindOf(l) != "firstgo" || !strings.Contains(l, "twigs") {
		t.Errorf("after the first twigs: %s", l)
	}
	exec(t, s, `UPDATE mastery SET top_day = ? WHERE pond_id = ?`, dayOf(*clock), id)
	if l := next(); kindOf(l) != "mastered" || !strings.Contains(l, "twigs") {
		t.Errorf("after mastering twigs: %s", l)
	}
}

func TestTheDiaryDoesntRepeatInAWeek(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	*clock = clock.Add(40 * 24 * time.Hour)
	s.greet(id, false)
	for range 60 {
		*clock = clock.Add(24 * time.Hour)
		if _, err := s.greet(id, false); err != nil {
			t.Fatal(err)
		}
	}
	all := diaryAll(t, s, id)
	if len(all) != 61 {
		t.Fatalf("%d lines in sixty one days", len(all))
	}
	for i, r := range all {
		for _, old := range all[max(0, i-6):i] {
			if old.Line == r.Line {
				t.Fatalf("%s says what %s said: %s", r.Day, old.Day, r.Line)
			}
		}
	}
}

var unkind = regexp.MustCompile(`(?i)\b(sad|hungry|starv\w*|cross|angry|lonely|alone|missed|miss|waited|waiting|forgot|forgotten|left me|where were|without you|at last|finally)\b`)

func TestTheDiaryPoolsAreKind(t *testing.T) {
	slots := regexp.MustCompile(`\{[a-z_]+\}`)
	allowed := map[string][]string{"grew": {"{a_stage}"}, "fish": {"{score}"}, "record": {"{game}", "{score}"}, "hat": {"{hat}"}, "friend": {"{who}"}, "keepsake": {"{keepsake}"}, "memento": {"{who}", "{thing}"}, "set": {"{set}", "{thing}"}, "pin": {"{badge}"},
		"biggest": {"{a_fish}", "{fish}", "{who}"}, "catch": {"{a_fish}", "{fish}"}, "firstgo": {"{target}"}, "mastered": {"{target}"}, "rank": {"{rank}"}}
	for kind, pool := range diaryLines {
		for _, nature := range natures {
			if n := len(pool.any) + len(pool.by[nature]); n < 3 {
				t.Errorf("%s has %d lines for %s", kind, n, nature)
			}
		}
		all := append([]string{}, pool.any...)
		for nature, ls := range pool.by {
			found := false
			for _, n := range natures {
				found = found || n == nature
			}
			if !found {
				t.Errorf("%s has lines for %q, which isn't a nature", kind, nature)
			}
			all = append(all, ls...)
		}
		for _, l := range all {
			if strings.ContainsAny(l, "—–;:!") || unkind.MatchString(l) || !strings.HasSuffix(l, ".") {
				t.Errorf("%s: %s", kind, l)
			}
			for _, sl := range slots.FindAllString(l, -1) {
				ok := false
				for _, a := range allowed[kind] {
					ok = ok || a == sl
				}
				if !ok {
					t.Errorf("%s uses %s, which it isn't given: %s", kind, sl, l)
				}
			}
		}
	}
	// A quiet week can't run out of quiet lines.
	for _, nature := range natures {
		if n := len(diaryLines["quiet"].any) + len(diaryLines["quiet"].by[nature]); n <= diaryWeek {
			t.Errorf("%d quiet lines for %s, and a week is %d", n, nature, diaryWeek)
		}
	}
}

// A new rung is a reveal and a line in the next day's diary.
func TestARankUpIsNotedForTheDiary(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	*clock = clock.Add(3 * 24 * time.Hour)
	if _, err := s.wear(id, hat{}); err != nil {
		t.Fatal(err)
	}
	// Every badge, fish and memento is enough for the second rung.
	for _, b := range badges {
		if _, err := s.w.Exec(`INSERT INTO badges (pond_id, kind, day) VALUES (?, ?, '2026-11-01') ON CONFLICT DO NOTHING`, id, b.kind); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range fishKinds {
		if _, err := s.w.Exec(`INSERT INTO fish (pond_id, kind, count, best, first_day) VALUES (?, ?, 1, 20, '2026-11-01')`, id, f.kind); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range mementos {
		if _, err := s.w.Exec(`INSERT INTO mementos (pond_id, kind, day) VALUES (?, ?, '2026-11-01')`, id, m.kind); err != nil {
			t.Fatal(err)
		}
	}
	v, err := s.wear(id, hat{})
	if err != nil {
		t.Fatal(err)
	}
	r := v.Share.Power.Rise
	if r == nil || r.Rank != v.Share.Power.Rank || v.Share.Power.at < 1 {
		t.Fatalf("at %d: %+v", v.Share.Power.Total, r)
	}
	var day string
	var top int
	if err := s.r.QueryRow(`SELECT rank_top, rank_day FROM ponds WHERE id = ?`, id).Scan(&top, &day); err != nil || top != v.Share.Power.at || day != dayOf(*clock) {
		t.Errorf("kept rung %d on %q (%v)", top, day, err)
	}
	p, _ := loadPond(s.r, id)
	f, _ := loadFrogs(s.r, id)
	n := clock.In(pondZone)
	from := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, pondZone)
	facts := []diaryFact{}
	if err := s.write(func(tx *sql.Tx) error {
		var err error
		facts, err = diaryFacts(tx, p, f[0], day, from, from.AddDate(0, 0, 1))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range facts {
		found = found || (x.kind == "rank" && x.slots[1] == titled(r.Rank))
	}
	if !found {
		t.Errorf("no rank in the diary's facts: %+v", facts)
	}
}
