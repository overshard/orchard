package main

import (
	"strings"
	"testing"
	"time"
)

func TestEveryResidentHasAFolk(t *testing.T) {
	if len(folks) != len(residents) {
		t.Fatalf("%d folks for %d residents", len(folks), len(residents))
	}
	for i, f := range folks {
		if f.bio == "" || f.voice == "" || f.press == "" || len(f.doing) < 4 || len(f.thinks) < 3 || len(f.says) < 2 {
			t.Errorf("%s is missing lines", residents[i].name)
		}
	}
}

func TestHandFolkLinesPassTheirOwnChecks(t *testing.T) {
	seen := map[string]bool{}
	check := func(field string, ls []string) {
		rule, _ := folkRule(field)
		for _, l := range ls {
			if _, ok := checkLine(l, rule, seen); !ok {
				t.Errorf("%s: %q", field, l)
			}
		}
	}
	for _, f := range folks {
		check("doing", f.doing)
		check("thinks", f.thinks)
		check("says", f.says)
		check("press", []string{f.press})
	}
	for w, ls := range weatherThinks {
		check(w, ls)
	}
	check("read", readSays)
}

func TestCheckLineForFolk(t *testing.T) {
	cases := []struct {
		field, line string
		ok          bool
	}{
		{"thinks", "I think I'll have a nap, then I'm off to the reeds.", true},
		{"thinks", "I wonder if Sedge caught anything today.", false},
		{"doing", "Sweeping leaves off the lily pads.", true},
		{"doing", "It sweeps leaves off the lily pads.", false},
		{"read", "Did you see {name} in the gazette this morning?", true},
		{"read", "Did you see the gazette this morning?", false},
		{"says", "Hello there. Sedge says you cook.", false},
		{"says", "Hello there. Tell me, do you cook?", true},
		{"thinks", "Thinks: I hope the rain keeps everybody cosy tonight.", true},
		{"thinks", "I keep thinking about the very long walk round the whole pond and back again, slowly.", false},
	}
	for _, c := range cases {
		rule, _ := folkRule(c.field)
		if _, ok := checkLine(c.line, rule, map[string]bool{}); ok != c.ok {
			t.Errorf("%s %q: got %v", c.field, c.line, ok)
		}
	}
}

func TestFolkOfFillsAndReacts(t *testing.T) {
	for d := 0; d < 30; d++ {
		now := time.Date(2026, 11, 1, 9, 0, 0, 0, pondZone).AddDate(0, 0, d).Add(time.Duration(d) * 3 * time.Hour)
		for i := range residents {
			for _, mood := range []string{moodPlain, moodRain, moodGloom2} {
				f := folkOf(i, 2, mood, []int{(i + 1) % len(residents)}, now)
				for _, l := range []string{f.Doing, f.Thinks, f.Says} {
					if l == "" || strings.Contains(l, "{") || strings.Contains(l, residents[i].name+" ") {
						t.Fatalf("%s on %s: %q", residents[i].name, mood, l)
					}
				}
			}
		}
	}
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, pondZone)
	if f := folkOf(3, 0, moodPlain, []int{3}, now); !f.Paper || f.Says != folks[3].press {
		t.Errorf("in the paper: %+v", f)
	}
	if f := folkOf(3, 0, moodPlain, nil, now); f.Paper || !contains(strangerSays, f.Says) {
		t.Errorf("a stranger: %+v", f)
	}
	if f := folkOf(3, 4, moodSombre, nil, now); f.Thinks != "" || !contains(quietLines, f.Doing) {
		t.Errorf("a sombre day: %+v", f)
	}
}

func TestInPaper(t *testing.T) {
	got := inPaper([]string{"Pip and Moth settle an argument", "A quiet day"})
	if len(got) != 2 || got[0] != residentNamed("Pip") || got[1] != residentNamed("Moth") {
		t.Errorf("got %v", got)
	}
}

func contains(ls []string, s string) bool {
	for _, l := range ls {
		if l == s {
			return true
		}
	}
	return false
}
