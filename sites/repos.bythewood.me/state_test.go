package main

import (
	"net/http/httptest"
	"testing"
)

func TestSuiteStateListsRepositories(t *testing.T) {
	s, _, _ := accessFixture(t, true)
	st := s.suiteState(httptest.NewRequest("GET", "/suite/state", nil))
	if st.Level != "ok" {
		t.Fatalf("got level %q", st.Level)
	}
	var repos int
	for _, it := range st.Items {
		if it.Kind == "repo" {
			repos++
		}
	}
	if repos == 0 {
		t.Fatalf("no repositories in %+v", st.Items)
	}
}
