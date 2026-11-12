package main

import (
	"errors"
	"fmt"
	"strings"
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

func TestGiftSaysItsPoints(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	r := residents[1]
	fav := r.favDish()
	plain := residents[0].favDish()
	if plain == fav {
		t.Fatal("pick two neighbours with different favourites")
	}
	if _, err := s.w.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, ?, 1), (?, ?, 1)`, id, plain.kind(), id, fav.kind()); err != nil {
		t.Fatal(err)
	}
	b, err := s.gift(id, 1, plain.kind())
	if err != nil {
		t.Fatal(err)
	}
	if b.Points != giftPoints[plain.tier] || b.Total != b.Points || b.Prizes == nil || b.Pond.Neighbours.All[1].Points != b.Total {
		t.Fatalf("a plain dish: %+v", b)
	}
	n := b.Pond.Neighbours.All[1]
	if n.FavKind != fav.kind() || n.FavTier != fav.tier || n.FavA != "" {
		t.Errorf("a stranger's favourite: %+v", n)
	}
	*clock = clock.Add(24 * time.Hour)
	b, err = s.gift(id, 1, fav.kind())
	if err != nil || b.Points != giftPoints[fav.tier]*3 || b.Total != giftPoints[plain.tier]+b.Points {
		t.Fatalf("its favourite: %v %+v", err, b)
	}
}

func TestTheHeartLadder(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	hs := v.Neighbours.Hearts
	if len(hs) != bestFriend || len(v.Neighbours.GiftPoints) != tierCount {
		t.Fatalf("%+v", v.Neighbours)
	}
	for i, h := range hs {
		l := i + 1
		z := friendPresent(l)
		if h.At != friendAt[l] || h.Name != friendNames[l] || (z.Pebbles > 0 && !strings.Contains(h.Brings, fmt.Sprint(z.Pebbles))) {
			t.Errorf("heart %d: %+v", l, h)
		}
		if z.Hat != nil && !strings.Contains(h.Brings, strings.ToLower(tierLabels[z.Hat.tier()])) {
			t.Errorf("heart %d names the wrong hat: %+v", l, h)
		}
	}
}
