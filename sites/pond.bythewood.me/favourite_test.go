package main

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func frontFrog(t *testing.T, s *store, id int64) frogRow {
	t.Helper()
	frogs, err := loadFrogs(s.r, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frogs {
		if f.Front {
			return f
		}
	}
	t.Fatal("no frog out front")
	return frogRow{}
}

func putIn(t *testing.T, s *store, id int64, kind string, n int) {
	t.Helper()
	if err := s.write(func(tx *sql.Tx) error { return addItem(tx, id, kind, n) }); err != nil {
		t.Fatal(err)
	}
}

func TestAFavouriteComesOffTheSeedWithNothingElse(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	*clock = clock.Add(5 * 24 * time.Hour)
	s.greet(id, false)
	if f := frontFrog(t, s, id); f.FavDish != "" || f.FavHat != "" {
		t.Fatalf("a tadpole has favourites: %q %q", f.FavDish, f.FavHat)
	}
	*clock = clock.Add(10 * 24 * time.Hour)
	g, err := s.greet(id, false)
	if err != nil {
		t.Fatal(err)
	}
	f := frontFrog(t, s, id)
	if f.FavDish != seedDish(f.Seed) || f.FavHat != seedHat(f.Seed) {
		t.Fatalf("favourites %q and %q, want the seed's %q and %q", f.FavDish, f.FavHat, seedDish(f.Seed), seedHat(f.Seed))
	}
	d, _ := dishOfKind(f.FavDish)
	if ingredients[d.a].trinket || ingredients[d.b].trinket {
		t.Errorf("%s can't be cooked out of a garden", d.name)
	}
	if h, _ := hatOfKind(f.FavHat); !h.valid() || h.tier() > 2 {
		t.Errorf("%s is a lot to ask for", h.name())
	}
	if fv := g.Pond.Frogs[0].Fav; fv == nil || fv.Dish.Kind != f.FavDish || fv.Hat != (hat{}) && hatKind(fv.Hat) != f.FavHat {
		t.Errorf("the view's favourites: %+v", fv)
	}
}

func TestAFavouriteIsWhatItAteAndWoreMost(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	often, once := dishOf(0, 1).kind(), dishOf(1, 1).kind()
	putIn(t, s, id, often, 2)
	putIn(t, s, id, once, 1)
	worn := hat{5, 0}
	if err := s.write(func(tx *sql.Tx) error { _, err := addHat(tx, id, worn, ""); return err }); err != nil {
		t.Fatal(err)
	}
	frog := frontFrog(t, s, id).ID
	for i, kind := range []string{often, once, often} {
		*clock = clock.Add(3 * 24 * time.Hour)
		if _, err := s.greet(id, false); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := s.wear(id, worn); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.feed(id, frog, kind); err != nil {
			t.Fatal(err)
		}
	}
	*clock = clock.Add(5 * 24 * time.Hour)
	s.greet(id, false)
	f := frontFrog(t, s, id)
	if f.FavDish != often || f.FavHat != hatKind(worn) {
		t.Fatalf("favourites %q and %q, want %q and %q", f.FavDish, f.FavHat, often, hatKind(worn))
	}
	// Once it has them they stay, whatever it eats after.
	putIn(t, s, id, once, 3)
	for range 3 {
		*clock = clock.Add(3 * 24 * time.Hour)
		s.greet(id, false)
		if _, err := s.feed(id, frog, once); err != nil {
			t.Fatal(err)
		}
	}
	if f := frontFrog(t, s, id); f.FavDish != often {
		t.Errorf("the favourite moved to %q", f.FavDish)
	}
}

func TestTheFavouriteDishKeepsItFullHalfAsLongAgain(t *testing.T) {
	d := dishOf(0, 1)
	f := frogRow{Fed: d.kind(), FedAt: 1000}
	plain := fullUntil(f) - f.FedAt
	f.FavDish = d.kind()
	if fav := fullUntil(f) - f.FedAt; plain != int64(mealHours[d.tier]*3600) || fav != plain*3/2 {
		t.Errorf("full for %d seconds, and %d on its favourite", plain, fav)
	}
	f.FavDish = dishOf(1, 1).kind()
	if other := fullUntil(f) - f.FedAt; other != plain {
		t.Errorf("another frog's favourite kept it full for %d", other)
	}
	// The best dish there is, eaten as a favourite by the best diver there
	// could be, has to bring in less than the dish sells for.
	top := tierCount - 1
	extra := mealHours[top] * favFull * mealBoost[top] * speedAt(maxLevel) * 1.25
	if extra >= float64(dishWorth[top]) {
		t.Errorf("a favourite legendary dish is worth %.0f pebbles of diving and sells for %d", extra, dishWorth[top])
	}
}

func TestTheBubbleNamesAFavouriteNowAndThen(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	*clock = clock.Add(15 * 24 * time.Hour)
	s.greet(id, false)
	f := frontFrog(t, s, id)
	d, _ := dishOfKind(f.FavDish)
	h, _ := hatOfKind(f.FavHat)
	said := 0
	for range 60 {
		*clock = clock.Add(24 * time.Hour)
		g, err := s.greet(id, false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(g.Line, strings.ToLower(d.name)) || strings.Contains(g.Line, strings.ToLower(h.name())) {
			said++
		}
		if strings.ContainsAny(g.Line, "—–;") {
			t.Errorf("the line has a dash or a semicolon: %s", g.Line)
		}
	}
	if said < 8 || said > 35 {
		t.Errorf("it named a favourite on %d days of 60", said)
	}
}
