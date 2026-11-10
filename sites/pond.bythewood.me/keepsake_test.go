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
