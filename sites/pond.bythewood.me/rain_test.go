package main

import (
	"testing"
	"time"
)

func rainRows(t *testing.T, s *store) map[int64]int {
	t.Helper()
	rows, err := s.r.Query(`SELECT slot, wet FROM rain`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[int64]int{}
	for rows.Next() {
		var slot int64
		var wet int
		if err := rows.Scan(&slot, &wet); err != nil {
			t.Fatal(err)
		}
		got[slot] = wet
	}
	return got
}

func TestHowWetAReadingIs(t *testing.T) {
	for _, c := range []struct {
		cond string
		rain int
		want int
	}{
		{"Sunny", 0, 0},
		{"Cloudy", 59, 0},
		{"Cloudy", 60, 2},
		{"Light Drizzle", 30, 1},
		{"Light Rain", 70, 2},
		{"Rain Showers", 40, 2},
		{"Heavy Rain", 85, 3},
		{"Thunderstorms", 20, 3},
	} {
		r := &reading{Cond: c.cond, Rain: c.rain}
		if got := wetOf(r); got != c.want {
			t.Errorf("%s at %d%% is %d, want %d", c.cond, c.rain, got, c.want)
		}
		if dayOfReading(r, time.Now()).Rain != (c.want > 0) {
			t.Errorf("%s at %d%%: the day and the rain table disagree", c.cond, c.rain)
		}
	}
	if wetOf(nil) != 0 {
		t.Error("no reading is wet")
	}
}

func TestRainIsKeptOncePerSlot(t *testing.T) {
	s, clock := testStore(t)
	*clock = time.Date(2026, 11, 10, 15, 1, 0, 0, time.UTC)
	s.day.last.Store(&reading{At: *clock, Cond: "Light Rain", Rain: 70})
	for range 2 {
		if err := s.keepRain(); err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(4 * time.Minute)
	}
	got := rainRows(t, s)
	if len(got) != 1 || got[rainSlot(*clock)] != 2 {
		t.Fatalf("two polls in one slot kept %v", got)
	}

	*clock = clock.Add(10 * time.Minute)
	s.day.last.Store(&reading{At: *clock, Cond: "Sunny"})
	if err := s.keepRain(); err != nil {
		t.Fatal(err)
	}
	if got := rainRows(t, s); len(got) != 1 {
		t.Errorf("a dry slot was kept: %v", got)
	}
}

func TestStaleRainIsNotKept(t *testing.T) {
	s, clock := testStore(t)
	s.day.last.Store(&reading{At: clock.Add(-dayStale - time.Minute), Cond: "Thunderstorms"})
	if err := s.keepRain(); err != nil {
		t.Fatal(err)
	}
	if got := rainRows(t, s); len(got) != 0 {
		t.Fatalf("a stale reading kept %v", got)
	}

	s.day.pinned.Store(true)
	if err := s.keepRain(); err != nil {
		t.Fatal(err)
	}
	if got := rainRows(t, s); got[rainSlot(*clock)] != 3 {
		t.Errorf("a pinned dev day kept %v, want thunder now", got)
	}
}

func TestOldRainIsSwept(t *testing.T) {
	s, clock := testStore(t)
	s.day.pinned.Store(true)
	s.day.last.Store(&reading{Cond: "Rain"})
	old := *clock
	if err := s.keepRain(); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add((rainKept - 1) * 24 * time.Hour)
	if err := s.keepRain(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.sweep(); err != nil {
		t.Fatal(err)
	}
	if got := rainRows(t, s); len(got) != 2 {
		t.Fatalf("a month of rain was swept early: %v", got)
	}
	*clock = clock.Add(2 * 24 * time.Hour)
	if _, err := s.sweep(); err != nil {
		t.Fatal(err)
	}
	got := rainRows(t, s)
	if _, ok := got[rainSlot(old)]; ok || len(got) != 1 {
		t.Errorf("rain older than %d days is still kept: %v", rainKept, got)
	}
}
