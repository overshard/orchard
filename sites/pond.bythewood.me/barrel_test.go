package main

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"
)

func putRain(t *testing.T, s *store, from time.Time, slots, wet int) {
	t.Helper()
	err := s.write(func(tx *sql.Tx) error {
		for i := range slots {
			if _, err := tx.Exec(`INSERT INTO rain (slot, wet) VALUES (?, ?) ON CONFLICT DO NOTHING`,
				rainSlot(from)+int64(i), wet); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A barrel looked at by day, so no dew has formed since the pond was made.
func dayBarrel(t *testing.T) (*store, *time.Time, int64) {
	t.Helper()
	s, clock := testStore(t)
	*clock = time.Date(2026, 11, 10, 14, 0, 0, 0, pondZone)
	_, id := newPond(t, s)
	return s, clock, id
}

func TestTheBarrelFillsFromTheRainTable(t *testing.T) {
	s, clock, id := dayBarrel(t)
	start := *clock
	*clock = clock.Add(5 * time.Hour)
	putRain(t, s, start.Add(time.Hour), 6, 2)
	putRain(t, s, start.Add(3*time.Hour), 3, 1)
	v, err := s.view(id)
	if err != nil {
		t.Fatal(err)
	}
	if b := v.Barrel; b.Level != 15 || b.Rain != 15 || b.Dew != 0 || !b.Ready {
		t.Fatalf("six slots of rain and three of drizzle gave %+v", b)
	}
	again, _ := s.view(id)
	if !reflect.DeepEqual(v.Barrel.sieve, again.Barrel.sieve) {
		t.Errorf("the sieve changed on a second look, %+v then %+v", v.Barrel.sieve, again.Barrel.sieve)
	}

	putRain(t, s, start.Add(4*time.Hour), 30, 3)
	*clock = start.Add(10 * time.Hour)
	v, _ = s.view(id)
	if v.Barrel.Level != barrelCap {
		t.Errorf("a storm filled it to %d, want the cap of %d", v.Barrel.Level, barrelCap)
	}
}

func TestDryNightsLeaveDew(t *testing.T) {
	s, clock, id := dayBarrel(t)
	start := *clock
	*clock = start.Add(24 * time.Hour)
	v, _ := s.view(id)
	if v.Barrel.Dew != dewTo+24-dewFrom || v.Barrel.Rain != 0 {
		t.Fatalf("a dry night gave %+v", v.Barrel.sieve)
	}
	// Rain in a night hour takes that hour's dew.
	night := time.Date(2026, 11, 10, 23, 0, 0, 0, pondZone)
	putRain(t, s, night.Add(10*time.Minute), 1, 1)
	v, _ = s.view(id)
	if v.Barrel.Dew != dewTo+24-dewFrom-1 || v.Barrel.Rain != 1 {
		t.Errorf("a wet hour left %d dew and %d rain", v.Barrel.Dew, v.Barrel.Rain)
	}
	*clock = start.Add(10 * 24 * time.Hour)
	v, _ = s.view(id)
	if v.Barrel.Level != barrelCap {
		t.Errorf("ten dry nights fill it to %d, want %d", v.Barrel.Level, barrelCap)
	}
}

func TestTheDealIsTheSameWhoeverLooks(t *testing.T) {
	s, _ := testStore(t)
	from := time.Date(2026, 11, 1, 12, 0, 0, 0, pondZone)
	putRain(t, s, from.Add(time.Hour), 27, 2)
	a, err := dealBarrel(s.r, s.pepper, 7, from, from.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := dealBarrel(s.r, s.pepper, 7, from, from.Add(48*time.Hour))
	other, _ := dealBarrel(s.r, s.pepper, 8, from, from.Add(48*time.Hour))
	if !reflect.DeepEqual(a, b) {
		t.Error("the same pond and rain dealt twice came out different")
	}
	if reflect.DeepEqual(a, other) {
		t.Error("two ponds got the same sieve off the same rain")
	}
	for _, th := range a.Things {
		if th.Name == "" {
			t.Errorf("%s has no name", th.Kind)
		}
	}
}

// Every kind the barrel can deal is a thing a pond already holds.
func TestWhatWashesInHasNames(t *testing.T) {
	r := barrelRand([]byte("names"), "x")
	at := time.Date(2026, 6, 1, 12, 0, 0, 0, pondZone)
	for range 20000 {
		kind, n := washed(washRain, r, at)
		if n > 0 && kind != "peb" && thingName(kind) == "" {
			t.Fatalf("%s has no name", kind)
		}
	}
	for _, table := range [][]washIn{washRain, washDew} {
		sum := 0
		for _, w := range table {
			sum += w.w
		}
		if sum != 1000 {
			t.Errorf("a wash table sums to %d", sum)
		}
	}
}

func TestTheBarrelTipsOnceADay(t *testing.T) {
	s, clock, id := dayBarrel(t)
	start := *clock
	*clock = start.Add(5 * time.Hour)
	putRain(t, s, start.Add(time.Hour), 27, 2)
	before, _ := s.view(id)
	d, err := s.tipBarrel(id, tipSieve)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Pond.Pebbles - before.Pebbles; got != before.Barrel.Pebbles || got != d.Sieve.Pebbles {
		t.Errorf("tipped %d pebbles out of a sieve showing %d", got, before.Barrel.Pebbles)
	}
	if d.Pond.Barrel.Level != 0 || d.Pond.Barrel.Ready {
		t.Errorf("a tipped barrel shows %+v", d.Pond.Barrel)
	}
	putRain(t, s, clock.Add(10*time.Minute), 6, 2)
	*clock = clock.Add(2 * time.Hour)
	if _, err := s.tipBarrel(id, tipSieve); !errors.Is(err, errNotYet) {
		t.Errorf("tipped twice in a day, %v", err)
	}
	*clock = clock.Add(24 * time.Hour)
	d, err = s.tipBarrel(id, tipSieve)
	if err != nil {
		t.Fatal(err)
	}
	if d.Sieve.Rain != 12 {
		t.Errorf("the next day's tip had %d rain, want only the 12 since the last", d.Sieve.Rain)
	}
}

func TestAnEmptyBarrelWontTip(t *testing.T) {
	s, clock, id := dayBarrel(t)
	*clock = clock.Add(time.Hour)
	if _, err := s.tipBarrel(id, tipSieve); !errors.Is(err, errNotYet) {
		t.Errorf("an empty barrel tipped, %v", err)
	}
}

func TestWateringTheGardenFromTheBarrel(t *testing.T) {
	s, clock, id := dayBarrel(t)
	start := *clock
	*clock = start.Add(5 * time.Hour)
	putRain(t, s, start.Add(time.Hour), 6, 2)
	if _, err := s.tipBarrel(id, tipGarden); !errors.Is(err, errNotYet) {
		t.Fatalf("watered a pond with no garden, %v", err)
	}
	if _, err := s.w.Exec(`UPDATE ponds SET garden_level = 1 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	s.day.last.Store(&reading{At: *clock, Cond: "Light Rain", Rain: 70})
	v, _ := s.view(id)
	if v.Barrel.Water == "" {
		t.Error("watering is offered in the rain")
	}
	if _, err := s.tipBarrel(id, tipGarden); !errors.Is(err, errNotYet) {
		t.Errorf("watered the garden in the rain, %v", err)
	}
	s.day.last.Store(&reading{At: *clock, Cond: "Sunny", HasTemp: true, Temp: 60})
	if err := s.windGrowth(); err != nil {
		t.Fatal(err)
	}
	clock0, _ := s.gardenClock(s.r, id, *clock)
	if _, err := s.tipBarrel(id, tipGarden); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(5 * time.Hour)
	s.day.last.Store(&reading{At: *clock, Cond: "Sunny", HasTemp: true, Temp: 60})
	clock1, _ := s.gardenClock(s.r, id, *clock)
	global, _ := s.growth(s.r, *clock)
	if got := clock1 - clock0; got < 8*3600-1 || got > 8*3600+1 {
		t.Errorf("five hours with three of them watered moved the garden %.0f seconds, want 8 hours", got)
	}
	if extra := clock1 - global; extra != 3*3600 {
		t.Errorf("the barrel put this garden %.0f seconds ahead, want 3 hours", extra)
	}
	v, _ = s.view(id)
	if v.Barrel.Watering != 0 {
		t.Errorf("the watering still runs after it ended, %d", v.Barrel.Watering)
	}
}
