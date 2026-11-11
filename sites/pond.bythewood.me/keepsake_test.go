package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestKeepsakeOncePerDay(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	s.day.last.Store(&reading{At: *clock, Temp: 30, HasTemp: true, Cond: "Clear"})
	*clock = clock.Add(time.Hour)
	g, err := s.greet(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasLine(g.Missed, "Frosted leaf, for a hard frost") {
		t.Fatalf("no frosted leaf on a frost, got %v", g.Missed)
	}
	*clock = clock.Add(time.Minute)
	if g, _ = s.greet(id, false); hasLine(g.Missed, "Frosted leaf, for a hard frost") {
		t.Fatal("a reload handed the frosted leaf over again")
	}
	*clock = clock.Add(24 * time.Hour)
	s.day.last.Store(&reading{At: *clock, Temp: 31, HasTemp: true, Cond: "Clear"})
	if _, err := s.greet(id, false); err != nil {
		t.Fatal(err)
	}
	v, err := s.view(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range v.Keepsakes {
		if k.Kind == "k_frost" && (k.Days != 2 || k.First != "2026-11-10") {
			t.Fatalf("frost on the shelf = %+v, want two days from 2026-11-10", k)
		}
		if k.Kind == "k_snow" && k.Days != 0 {
			t.Fatal("snow on the shelf without any snow")
		}
	}
}

func hasLine(lines [][2]string, want string) bool {
	for _, l := range lines {
		if l[1] == want {
			return true
		}
	}
	return false
}

func TestFullMoons(t *testing.T) {
	for _, d := range []string{"2026-01-03", "2026-05-31", "2026-08-28", "2026-11-24", "2027-02-20"} {
		day, _ := time.ParseInLocation("2006-01-02 15", d+" 20", pondZone)
		if !fullMoon(day) {
			t.Errorf("%s is a full moon and wasn't counted", d)
		}
		if fullMoon(day.AddDate(0, 0, 4)) || fullMoon(day.AddDate(0, 0, -4)) {
			t.Errorf("four days from the full moon on %s still counts", d)
		}
	}
}

func TestSolsticeWindow(t *testing.T) {
	at := func(s string) time.Time { d, _ := time.ParseInLocation("2006-01-02 15:04", s, pondZone); return d }
	if !near(at("2026-12-20 00:05"), time.December, 21) || !near(at("2026-12-22 23:55"), time.December, 21) {
		t.Fatal("midwinter misses a day either side")
	}
	if near(at("2026-12-23 00:05"), time.December, 21) || near(at("2026-12-19 23:55"), time.December, 21) {
		t.Fatal("midwinter runs past a day either side")
	}
}

func TestEveryKeepsakeHasAnIcon(t *testing.T) {
	raw, err := os.ReadFile("frontend/src/icons.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keepsakes {
		if !strings.Contains(string(raw), "\n  "+k.kind+": [[") {
			t.Errorf("%s has no icon", k.kind)
		}
	}
}

func TestDaysAtThePond(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	visit := func() *greeting {
		g, err := s.greet(id, false)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	// The same day twice is one day.
	*clock = clock.Add(time.Hour)
	if g := visit(); g.Pond.Visited != 1 {
		t.Fatalf("%d days after two visits on the first", g.Pond.Visited)
	}
	for range 5 {
		*clock = clock.Add(24 * time.Hour)
		visit()
	}
	// A fortnight away takes nothing back, and the seventh day brings the pebble.
	*clock = clock.Add(14 * 24 * time.Hour)
	g := visit()
	if g.Pond.Visited != 7 || !hasLine(g.Missed, "Worn pebble, for seven days at the pond") {
		t.Fatalf("day %d: %v", g.Pond.Visited, g.Missed)
	}
	*clock = clock.Add(24 * time.Hour)
	if g := visit(); g.Pond.Visited != 8 || hasLine(g.Missed, "Worn pebble, for seven days at the pond") {
		t.Fatalf("day %d handed the pebble over again: %v", g.Pond.Visited, g.Missed)
	}
	for range 365 - 8 {
		*clock = clock.Add(24 * time.Hour)
		visit()
	}
	v, _ := s.view(id)
	got := 0
	for _, k := range v.Keepsakes {
		if k.Set == "visits" {
			got++
			if k.Days != 1 {
				t.Errorf("%s was kept on %d days", k.Name, k.Days)
			}
		}
	}
	if v.Visited != 365 || got != 4 {
		t.Errorf("%d days and %d of the four keepsakes", v.Visited, got)
	}
}

// A kind is stored by name and the shelf is drawn in this order, so a new
// keepsake only ever goes on the end.
func TestTheDayKeepsakesAreOnTheEnd(t *testing.T) {
	want := []string{"k_days7", "k_days30", "k_days100", "k_days365"}
	for i, k := range keepsakes[len(keepsakes)-len(want):] {
		if k.kind != want[i] || k.days == 0 {
			t.Errorf("%s is where %s goes", k.kind, want[i])
		}
	}
	if keepsakes[16].kind != "k_spawn" {
		t.Errorf("the list moved, %s is seventeenth", keepsakes[16].kind)
	}
}
