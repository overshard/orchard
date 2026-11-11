package main

import "testing"

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
