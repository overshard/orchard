package main

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func mementoPrizes(zs []prize) []string {
	var out []string
	for _, z := range zs {
		if strings.HasPrefix(z.Item, "m_") {
			out = append(out, z.Item)
		}
	}
	return out
}

func TestEveryResidentHasAMemento(t *testing.T) {
	if len(mementos) != len(residents) {
		t.Fatalf("%d mementos for %d residents", len(mementos), len(residents))
	}
	raw, err := os.ReadFile("frontend/src/icons.js")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for who, m := range mementos {
		if seen[m.kind] || seen[m.thing] || !strings.HasPrefix(m.kind, "m_") {
			t.Errorf("%s is a second %s", m.kind, m.thing)
		}
		seen[m.kind], seen[m.thing] = true, true
		if !strings.Contains(string(raw), "\n  "+m.kind+": [[") {
			t.Errorf("%s has no icon", m.kind)
		}
		if !strings.HasPrefix(thingName(m.kind), residents[who].name+"'s ") {
			t.Errorf("%s is named %q", m.kind, thingName(m.kind))
		}
		if strings.ContainsAny(m.line, "—–;:!") || unkind.MatchString(m.line) || !strings.HasSuffix(m.line, ".") {
			t.Errorf("%s: %s", m.kind, m.line)
		}
		if _, ok := itemNames[m.kind]; ok {
			t.Errorf("%s is an item", m.kind)
		}
	}
}

func TestABestFriendHandsOverItsMementoOnce(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	var got []prize
	befriendBy := func(points int) {
		t.Helper()
		if _, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
			var err error
			got, err = befriend(tx, p, 4, points, "")
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	befriendBy(friendAt[bestFriend] - 1)
	if m := mementoPrizes(got); len(m) != 0 {
		t.Fatalf("four hearts handed over %v", m)
	}
	v, _ := s.view(id)
	if v.Neighbours.All[4].Memento != "" || v.Mementos[4].Day != "" || v.Share.Power.part("mementos").Have != 0 {
		t.Fatalf("before five hearts: %+v", v.Mementos[4])
	}
	before := v.Share.Power.Total

	befriendBy(1)
	if m := mementoPrizes(got); len(m) != 1 || m[0] != "m_trowel" {
		t.Fatalf("the fifth heart handed over %v", m)
	}
	last := got[len(got)-1]
	if last.Item != "m_trowel" || last.HatTier != mementoTier || last.Text != mementos[4].line {
		t.Errorf("the memento comes last, after the hat: %+v", got)
	}
	v, _ = s.view(id)
	if v.Neighbours.All[4].Memento != "m_trowel" || v.Mementos[4].Day != v.Today || v.Mementos[4].Name != "Bramble's old trowel" || v.Mementos[4].From != "Bramble" {
		t.Errorf("after five hearts: %+v on %+v", v.Mementos[4], v.Neighbours.All[4])
	}
	// The level five hat counts too, so only the memento's share is checked.
	if p := v.Share.Power; p.part("mementos").Have != 1 || p.Total <= before {
		t.Errorf("power went from %d to %+v", before, p)
	}
	for i, m := range v.Mementos {
		if i != 4 && (m.Day != "" || v.Neighbours.All[i].Memento != "") {
			t.Errorf("%s was handed over too", m.Kind)
		}
	}

	befriendBy(40)
	if m := mementoPrizes(got); len(m) != 0 {
		t.Errorf("it handed over a second: %v", m)
	}
	if g, err := s.greet(id, false); err != nil || len(g.Prizes) != 0 {
		t.Errorf("the next greeting handed over %+v: %v", g.Prizes, err)
	}
	var rows, items int
	_ = s.r.QueryRow(`SELECT COUNT(*) FROM mementos WHERE pond_id = ?`, id).Scan(&rows)
	_ = s.r.QueryRow(`SELECT COUNT(*) FROM items WHERE pond_id = ? AND kind LIKE 'm\_%' ESCAPE '\'`, id).Scan(&items)
	if rows != 1 || items != 0 {
		t.Errorf("%d mementos kept and %d held as items", rows, items)
	}
}

// A pond that was best friends before there were mementos gets them at the
// next greeting, each from the neighbour it belongs to.
func TestAnOldBestFriendHandsItOverAtTheGreeting(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.w.Exec(`INSERT INTO neighbours (pond_id, who, points) VALUES (?, 0, 100), (?, 11, 250), (?, 5, 99)`, id, id, id); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Hour)
	g, err := s.greet(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Prizes) != 2 || len(g.From) != 2 || g.From[0] != 0 || g.From[1] != 11 || g.Prizes[0].Item != "m_float" || g.Prizes[1].Item != "m_shiny" {
		t.Fatalf("the greeting handed over %+v from %v", g.Prizes, g.From)
	}
	if g.Pond.Share.Power.part("mementos").Have != 2 || g.Pond.Neighbours.All[11].Memento != "m_shiny" || g.Pond.Neighbours.All[5].Memento != "" {
		t.Errorf("after the greeting: %+v", g.Pond.Share.Power)
	}
	*clock = clock.Add(time.Hour)
	if g, _ = s.greet(id, false); len(g.Prizes) != 0 || len(g.From) != 0 {
		t.Errorf("a second greeting handed over %+v", g.Prizes)
	}
}

func TestAMementoCantBeSoldOrSpent(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.w.Exec(`INSERT INTO mementos (pond_id, kind, day) VALUES (?, 'm_float', '2026-11-10')`, id); err != nil {
		t.Fatal(err)
	}
	for _, m := range mementos {
		if _, ok := worthOf(m.kind); ok {
			t.Errorf("%s has a going rate", m.kind)
		}
		if _, ok := wizardTier(m.kind); ok {
			t.Errorf("the wizard takes %s", m.kind)
		}
	}
	v, _ := s.view(id)
	for _, b := range v.Bag {
		if strings.HasPrefix(b.Kind, "m_") {
			t.Errorf("%s is in the bag", b.Kind)
		}
	}
	if _, err := s.stallList(id, "m_float", 100); !errors.Is(err, errNotHeld) {
		t.Errorf("put a memento on the stall: %v", err)
	}
	if _, err := s.wizard(id, "m_float"); !errors.Is(err, errNotHeld) {
		t.Errorf("took a memento to the wizard: %v", err)
	}
	if _, err := s.sellTrinket(id, "m_float"); !errors.Is(err, errNotHeld) {
		t.Errorf("sold a memento to Moth: %v", err)
	}
	if v, _ = s.view(id); v.Pebbles != starterPebbles || v.Mementos[0].Day == "" {
		t.Errorf("after trying: %d pebbles, %+v", v.Pebbles, v.Mementos[0])
	}
}

func TestMementosCountInPower(t *testing.T) {
	had := map[string]bool{"m_float": true, "m_shiny": true}
	best := bestFinds(nil, had, 8)
	if len(best) != 2 || best[0].Name != "Sedge's lucky float" || best[0].Tier != mementoTier {
		t.Errorf("best finds: %+v", best)
	}

	// The share page reads them off the table and agrees with the pond.
	s, _ := testStore(t)
	_, id := newPond(t, s)
	if _, err := s.w.Exec(`INSERT INTO mementos (pond_id, kind, day) VALUES (?, 'm_poem', '2026-11-10')`, id); err != nil {
		t.Fatal(err)
	}
	v, _ := s.view(id)
	if m := v.Share.Power.part("mementos"); m.Have != 1 || m.Pts != m.Of/len(mementos) {
		t.Errorf("one memento counts %+v", m)
	}
	var public string
	_ = s.r.QueryRow(`SELECT public_id FROM ponds WHERE id = ?`, id).Scan(&public)
	sh, err := s.shownBy(public)
	if err != nil || sh.Power.part("mementos") != v.Share.Power.part("mementos") || sh.Power.Total != v.Share.Power.Total {
		t.Errorf("shown %+v, the pond has %+v (%v)", sh.Power, v.Share.Power, err)
	}
}
