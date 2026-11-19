package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSuiteStateFlagsAFailingJob(t *testing.T) {
	s, _ := newTestSite(t)
	next := time.Now().Add(time.Hour)
	if _, err := s.store.Insert(Job{Name: "ok one", Kind: "notify", Message: "hi", Cron: "0 9 * * *", Enabled: true, NextRun: next}); err != nil {
		t.Fatal(err)
	}
	id, err := s.store.Insert(Job{Name: "broken", Kind: "prompt", Message: "look", Cron: "0 9 * * *", Enabled: true, NextRun: next})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.Reschedule(id, next, 1, time.Now(), "llm: timeout"); err != nil {
		t.Fatal(err)
	}
	st := s.suiteState(httptest.NewRequest("GET", "/suite/state", nil))
	if st.Level != "bad" || st.Count != 1 {
		t.Fatalf("got level %q count %d, want bad 1", st.Level, st.Count)
	}
	var found bool
	for _, it := range st.Items {
		if it.Name == "broken" {
			found = it.Level == "bad"
		}
	}
	if !found {
		t.Fatalf("the failing job isn't marked bad: %+v", st.Items)
	}
}
