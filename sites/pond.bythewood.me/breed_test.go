package main

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestTheEggBench(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	first := v.Frogs[0]
	if len(v.Frogs) != 1 || !first.Front || first.LayIn != -1 || v.Pads != startingPads || v.PadCost != 400 {
		t.Fatalf("a new pond: %+v pads %d cost %d", v.Frogs, v.Pads, v.PadCost)
	}
	if _, err := s.lay(id, first.ID, "", "", ""); !errors.Is(err, errNotYet) {
		t.Fatalf("an egg laid an egg: %v", err)
	}

	// Grown on day 13, it lays once and then waits a week.
	*clock = clock.Add(12 * 24 * time.Hour)
	l, err := s.lay(id, first.ID, "", "", "")
	if err != nil || l.Frog.Stage != "egg" || l.Frog.Mods == nil || len(l.Pond.Frogs) != 2 {
		t.Fatalf("lay: %+v %v", l, err)
	}
	if l.Pond.Frogs[1].Front || l.Pond.Frog.Seed != first.Seed {
		t.Error("the new egg came out front")
	}
	if _, err := s.lay(id, first.ID, "", "", ""); !errors.Is(err, errNotYet) {
		t.Errorf("laid twice in a week: %v", err)
	}
	*clock = clock.Add(layEvery)
	if _, err := s.lay(id, first.ID, "dye_gold", "", ""); !errors.Is(err, errNotHeld) {
		t.Errorf("laid with a dye it doesn't have: %v", err)
	}
	if err := s.write(func(tx *sql.Tx) error { return addItem(tx, id, "dye_gold", 1) }); err != nil {
		t.Fatal(err)
	}
	if l, err = s.lay(id, first.ID, "dye_gold", "", ""); err != nil || len(l.Pond.Items) != 0 {
		t.Fatalf("lay with a dye: %v, items %+v", err, l.Pond.Items)
	}

	// Three frogs fill three pads, and a fourth pad costs pebbles.
	*clock = clock.Add(layEvery)
	if _, err := s.lay(id, first.ID, "", "", ""); !errors.Is(err, errFullPads) {
		t.Errorf("laid onto a full pond: %v", err)
	}
	if _, err := s.buyPad(id); !errors.Is(err, errPoor) {
		t.Errorf("a pad with no pebbles: %v", err)
	}
	if err := s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ponds SET pebbles = 1000 WHERE id = ?`, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if v, err = s.buyPad(id); err != nil || v.Pads != 4 || v.Pebbles != 600 || v.PadCost != 800 {
		t.Fatalf("buy a pad: %v pads %d pebbles %d", err, v.Pads, v.Pebbles)
	}
	if _, err := s.lay(id, first.ID, "", "", ""); err != nil {
		t.Errorf("lay onto the new pad: %v", err)
	}
}

func TestBringingAFrogOutFront(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	first := v.Frogs[0].ID
	*clock = clock.Add(13 * 24 * time.Hour)
	l, err := s.lay(id, first, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	second := l.Pond.Frogs[1].ID
	if err := s.write(func(tx *sql.Tx) error { _, err := addHat(tx, id, hat{1, 0}); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.wear(id, hat{1, 0}); err != nil {
		t.Fatal(err)
	}

	v, err = s.front(id, second)
	if err != nil || v.Frog.Seed != l.Frog.Seed || v.Frog.Hat != (hat{}) || v.Frog.Mods == nil {
		t.Fatalf("front: %v %+v", err, v.Frog)
	}
	for _, f := range v.Frogs {
		if f.Front != (f.ID == second) {
			t.Errorf("frog %d front %v", f.ID, f.Front)
		}
		if f.ID == first && f.Hat != (hat{1, 0}) {
			t.Errorf("the first frog lost its hat going to the reeds: %+v", f.Hat)
		}
	}
	if v, _ = s.front(id, first); v.Frog.Hat != (hat{1, 0}) {
		t.Errorf("back out front without its hat: %+v", v.Frog.Hat)
	}
	if _, err := s.front(id, 99999); !errors.Is(err, errNotHeld) {
		t.Errorf("brought out somebody else's frog: %v", err)
	}
}

func TestBreedingLeansAndDrifts(t *testing.T) {
	parent := lookOf(1207)
	const n = 3000
	dyed, thrown := 0, 0
	for range n {
		m := breed(parent, randomSeed(), "dye_blue", "", "")
		if m.Morph == "BLUE" {
			dyed++
		}
		if m.BW < 10 || m.BW > 15.5 || m.Eye < 2.3 || m.Eye > 4.4 {
			t.Fatalf("a child out of shape: %+v", m)
		}
		m = breed(parent, randomSeed(), "", "", "")
		for _, k := range m.Took {
			if k == "throwback" {
				thrown++
			}
		}
	}
	if dyed < n*60/100 || dyed > n*75/100 {
		t.Errorf("a blue dye took %d times in %d", dyed, n)
	}
	if thrown < n*3/100 || thrown > n*10/100 {
		t.Errorf("%d throwbacks in %d", thrown, n)
	}
}

func TestBredFrogsDrawFromTheirMods(t *testing.T) {
	m := &mods{Morph: "GOLDEN", Pattern: "BANDS", Hue: 45, Sat: 85, Lit: 53, BW: 12, BH: 8, Eye: 3}
	g := genomeOf(1207, m)
	if g.Morph != "GOLDEN" || g.Pattern != "BANDS" || g.Name != newGenome(1207).Name {
		t.Errorf("genome: %+v", g)
	}
	if r := rarityOf(g); r.N <= rarityOf(newGenome(1207)).N {
		t.Errorf("a golden frog is no rarer: %+v", r)
	}
	if l := lookFor(1207, m); l.morph.Name != "GOLDEN" || l.hue != 45 || l.bw != 12 {
		t.Errorf("look: %+v", l)
	}
	if parseMods(`{"morph":"PURPLE"}`) != nil || parseMods("") != nil {
		t.Error("parsed mods that name no morph")
	}
}

// The live database was made before frogs could breed, so opening it has to
// add what's missing without touching what's there.
func TestAnOldDatabaseGainsTheNewColumns(t *testing.T) {
	path := t.TempDir() + "/old.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE ponds (id INTEGER PRIMARY KEY, key_hmac TEXT NOT NULL UNIQUE,
		public_id TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL,
		last_gift_at INTEGER NOT NULL, pebbles INTEGER NOT NULL, free_spins INTEGER NOT NULL,
		spin_at INTEGER NOT NULL, paid_spins INTEGER NOT NULL DEFAULT 0, paid_spins_day TEXT NOT NULL DEFAULT '',
		log_day TEXT NOT NULL DEFAULT '', dive_best INTEGER NOT NULL DEFAULT 0, dive_day TEXT NOT NULL DEFAULT '',
		dive_runs INTEGER NOT NULL DEFAULT 0, dive_paid INTEGER NOT NULL DEFAULT 0, dive_run TEXT NOT NULL DEFAULT '',
		dive_started_at INTEGER NOT NULL DEFAULT 0, key_saved_at INTEGER);
		CREATE TABLE frogs (id INTEGER PRIMARY KEY, pond_id INTEGER NOT NULL, seed INTEGER NOT NULL,
		name TEXT NOT NULL, hatched_at INTEGER NOT NULL, out_front INTEGER NOT NULL DEFAULT 1,
		hat_shape INTEGER NOT NULL DEFAULT 0, hat_colour INTEGER NOT NULL DEFAULT 0);
		INSERT INTO ponds (key_hmac, public_id, created_at, last_seen_at, last_gift_at, pebbles, free_spins, spin_at)
		VALUES ('x', 'y', 1, 1, 1, 20, 1, 1);
		INSERT INTO frogs (pond_id, seed, name, hatched_at) VALUES (1, 1207, 'PIP', 1);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := openStore(path, []byte("pepper"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.view(1)
	if err != nil || v.Pads != startingPads || len(v.Frogs) != 1 || v.Frogs[0].Mods != nil {
		t.Fatalf("old pond: %v %+v", err, v)
	}
}

func TestEyeDrops(t *testing.T) {
	parent := lookOf(1207)
	took, kept := 0, 0
	for range 2000 {
		m := breed(parent, randomSeed(), "", "", "drops_pale")
		if m.Iris == "#9fd8e8" {
			took++
			l := lookFor(1207, &m)
			if spriteColours(l, hat{})[4] != rgb("#9fd8e8") {
				t.Fatal("pale drops don't draw pale eyes")
			}
			if m.Morph != "RED EYE" {
				// A child of a frog with drops keeps them unless its colour changed.
				if c := breed(l, randomSeed(), "", "", ""); c.Iris == "#9fd8e8" {
					kept++
				}
			}
		}
	}
	if took < 1200 || took > 1600 {
		t.Errorf("pale drops took %d times in 2000", took)
	}
	if kept < took*85/100 {
		t.Errorf("%d of %d children kept their parent's drops", kept, took)
	}
	plain := genomeOf(1207, &mods{Morph: "GREEN", Pattern: "PLAIN"})
	dropped := genomeOf(1207, &mods{Morph: "GREEN", Pattern: "PLAIN", Iris: "#e2412d"})
	if rarityOf(dropped).N != rarityOf(plain).N*10 {
		t.Errorf("drops made 1 in %d into 1 in %d", rarityOf(plain).N, rarityOf(dropped).N)
	}
	if m := breed(lookFor(1207, &mods{Morph: "RED EYE", Pattern: "PLAIN", Hue: 118, Sat: 63, Lit: 45, BW: 12, BH: 8, Eye: 3}), randomSeed(), "", "", "drops_red"); m.Iris != "" && m.Morph == "RED EYE" {
		t.Error("red drops on a red eyed frog counted as drops")
	}
}
