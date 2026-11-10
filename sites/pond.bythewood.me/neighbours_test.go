package main

import (
	"errors"
	"testing"
	"time"
)

func TestResidentsAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range residents {
		n := r.name
		if seen[n] {
			t.Errorf("two neighbours called %s", n)
		}
		seen[n] = true
		d := r.favDish()
		if ingredients[d.a].trinket || ingredients[d.b].trinket {
			t.Errorf("%s's favourite needs a trinket", n)
		}
	}
}

func TestGiftsMakeFriends(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	r := residents[0]
	fav := r.favDish().kind()
	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, ?, 5)`, id, fav); err != nil {
		t.Fatal(err)
	}
	b, err := s.gift(id, 0, fav)
	if err != nil {
		t.Fatal(err)
	}
	pts := giftPoints[r.favDish().tier] * 3
	if b.Level != friendLevel(pts) || len(b.Prizes) != b.Level || b.Pond.Neighbours.All[0].Points != pts || !b.Pond.Neighbours.All[0].Gave {
		t.Fatalf("a favourite: %+v %+v", b, b.Pond.Neighbours.All[0])
	}
	if _, err := s.gift(id, 0, fav); !errors.Is(err, errGiven) {
		t.Errorf("two gifts in a day: %v", err)
	}
	if _, err := s.gift(id, 1, "dish_9_9"); !errors.Is(err, errNotHeld) {
		t.Errorf("gave a dish it didn't have: %v", err)
	}
	for range 30 {
		*clock = clock.Add(24 * time.Hour)
		if _, err := s.gift(id, 0, fav); err != nil {
			break
		}
	}
	v, _ := s.view(id)
	if n := v.Neighbours.All[0]; n.Level < recipeAt || n.FavA == "" {
		t.Errorf("after a few days of favourites: %+v", n)
	}
}

func TestTheFavour(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	f := v.Neighbours.Favour
	if _, err := s.favour(id); err == nil {
		t.Fatal("did a favour with nothing")
	}
	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, ?, ?)`, id, f.Kind, f.N*2); err != nil {
		t.Fatal(err)
	}
	b, err := s.favour(id)
	if err != nil || b.Pond.Pebbles != v.Pebbles+favourPebbles+prizePebbles(b.Prizes) || !b.Pond.Neighbours.Favour.Done {
		t.Fatalf("%v %+v", err, b)
	}
	if _, err := s.favour(id); !errors.Is(err, errNotYet) {
		t.Errorf("the same favour twice: %v", err)
	}
	*clock = clock.Add(24 * time.Hour)
	if v, _ := s.view(id); v.Neighbours.Favour.Done {
		t.Error("tomorrow's favour is already done")
	}
}

func prizePebbles(zs []prize) int {
	n := 0
	for _, z := range zs {
		n += z.Pebbles
	}
	return n
}
