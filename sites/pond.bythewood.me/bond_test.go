package main

import (
	"net/http"
	"testing"
	"time"
)

func otherSpot(seed uint32) string {
	for _, s := range petSpots {
		if s != favSpot(seed) {
			return s
		}
	}
	return ""
}

func TestBondStopsAtTheDaysWorth(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	p, _ := loadPond(s.r, id)
	plain := otherSpot(p.Seed)
	for i := range 40 {
		out, err := s.pet(id, plain)
		if err != nil {
			t.Fatal(err)
		}
		if want := min((i+1)*petPoints, bondADay); out.Bond.Fill != want || out.Bond.Left != bondADay-want || out.Fav {
			t.Fatalf("rub %d: %+v", i+1, out)
		}
	}
	// The day's worth is the same on the favourite spot, it only gets there sooner.
	*clock = clock.Add(24 * time.Hour)
	for i := range bondADay {
		out, err := s.pet(id, favSpot(p.Seed))
		if err != nil {
			t.Fatal(err)
		}
		if want := bondADay + min((i+1)*petPoints*2, bondADay); out.Bond.Fill != want || !out.Fav {
			t.Fatalf("favourite rub %d: %+v", i+1, out)
		}
	}
	v, _ := s.view(id)
	if v.Bond.Fill != 2*bondADay || v.Bond.Left != 0 || v.Bond.Hearts != 0 {
		t.Errorf("the view: %+v", v.Bond)
	}
}

func TestBondNeverDropsAndStopsAtFive(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	hearts, last := 0, -1
	for day := range 40 {
		// A week away in the middle costs nothing.
		if day == 10 {
			*clock = clock.Add(7 * 24 * time.Hour)
		}
		*clock = clock.Add(24 * time.Hour)
		for range 12 {
			out, err := s.pet(id, "head")
			if err != nil {
				t.Fatal(err)
			}
			if out.Heart {
				hearts++
			}
			now := out.Bond.Hearts*bondHeart + out.Bond.Fill
			if now < last {
				t.Fatalf("bond fell from %d to %d", last, now)
			}
			last = now
		}
	}
	v, _ := s.view(id)
	if v.Bond.Hearts != bondHearts || v.Bond.Fill != 0 || v.Bond.Left != 0 || hearts != bondHearts {
		t.Errorf("after forty days: %+v and %d hearts said", v.Bond, hearts)
	}
	var day string
	if err := s.r.QueryRow(`SELECT heart_day FROM frogs WHERE pond_id = ?`, id).Scan(&day); err != nil || day == "" {
		t.Errorf("the day of the last heart: %q %v", day, err)
	}
}

func TestEverySpotIsSomeFrogsFavourite(t *testing.T) {
	seen := map[string]int{}
	for seed := range uint32(300) {
		seen[favSpot(seed)]++
	}
	for _, sp := range petSpots {
		if seen[sp] < 60 {
			t.Errorf("%s is the favourite of %d frogs in 300", sp, seen[sp])
		}
	}
}

func TestPetRoute(t *testing.T) {
	b, _, _ := testSite(t)
	b.do(http.MethodPost, "/api/greet", "{}")
	var out petted
	if code := b.post("/api/frogs/pet", `{"spot":"nowhere"}`, &out); code != http.StatusOK {
		t.Fatalf("pet: %d", code)
	}
	if out.Bond.Fill != petPoints || out.Fav {
		t.Errorf("a rub nowhere in particular: %+v", out)
	}
}
