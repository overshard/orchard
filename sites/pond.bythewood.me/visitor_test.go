package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAFrogIsOutAndAboutUntilKeptHome(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	if v.Home {
		t.Fatal("a new pond's frog starts at home")
	}
	if v, _ = s.keepHome(id, true); !v.Home {
		t.Fatal("staying home didn't take")
	}
	if v, _ = s.keepHome(id, false); v.Home {
		t.Fatal("going out again didn't take")
	}
}

func TestOneVisitorADayAndItLeavesSomethingCommon(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	g, _ := s.greet(id, false)
	if g.Visitor != nil {
		t.Fatal("a visitor came on the day the pond was made")
	}
	for range 30 {
		*clock = clock.Add(24 * time.Hour)
		before, _ := s.view(id)
		g, err := s.greet(id, false)
		if err != nil {
			t.Fatal(err)
		}
		if g.Visitor == nil {
			t.Fatal("no visitor on a new day")
		}
		v := g.Visitor
		if v.Gift.Pebbles != 0 || v.Name == "" {
			t.Fatalf("a visitor: %+v", v)
		}
		tier := -1
		if tr, ok := trinketOf(v.Gift.Item); ok {
			tier = tr.tier
		} else if i, ok := ingredientOf(v.Gift.Item); ok {
			tier = ingredients[i].tier
		} else {
			for _, r := range rocks {
				if r.kind == v.Gift.Item {
					tier = r.tier
				}
			}
		}
		if tier != 0 {
			t.Errorf("%s isn't common", v.Gift.Item)
		}
		moved := g.Pond.Pebbles - before.Pebbles
		if g.Present != nil {
			moved -= g.Present.Pebbles
		}
		if moved != 0 {
			t.Errorf("a visitor moved the purse by %d", moved)
		}
		again, _ := s.greet(id, false)
		if again.Visitor != nil {
			t.Fatal("a second visitor the same day")
		}
		if again.Pond.Visitor == nil || again.Pond.Visitor.Name != v.Name {
			t.Fatal("the footprint went before the day was out")
		}
	}
}

// The visitor that comes is a real pond's frog when one was open and lets
// its frog out, and never one that keeps it home.
func TestAVisitorIsARealPondOnlyWhenOneWasOpenAndOut(t *testing.T) {
	s, clock := testStore(t)
	_, a := newPond(t, s)
	_, b := newPond(t, s)
	*clock = clock.Add(3 * 24 * time.Hour)
	if _, err := s.greet(a, false); err != nil {
		t.Fatal(err)
	}
	g, _ := s.greet(b, false)
	front, _ := s.view(a)
	if g.Visitor.From != "pond" || g.Visitor.Seed != front.Frog.Seed || g.Visitor.Name != titled(front.Frog.Name) {
		t.Fatalf("b's visitor wasn't a's frog: %+v", g.Visitor)
	}
	raw, _ := json.Marshal(g)
	for _, leak := range []string{front.Share.Path[3:], `"id"`, "public"} {
		if strings.Contains(string(raw), leak) && leak != `"id"` {
			t.Errorf("the greeting carries %q", leak)
		}
	}
	if strings.Contains(string(mustJSON(t, g.Visitor)), `"id"`) {
		t.Error("the visitor carries an id")
	}

	if _, err := s.keepHome(a, true); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		*clock = clock.Add(24 * time.Hour)
		if _, err := s.greet(a, false); err != nil {
			t.Fatal(err)
		}
		if g, _ := s.greet(b, false); g.Visitor.From == "pond" {
			t.Fatal("a pond that keeps its frog home sent it visiting")
		}
	}
	// A pond nobody opened for a few days sends nobody.
	if _, err := s.keepHome(a, false); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(3 * 24 * time.Hour)
	if g, _ := s.greet(b, false); g.Visitor.From == "pond" {
		t.Fatal("a shut pond's frog came visiting")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
