package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMergedNeedsASession(t *testing.T) {
	s, _ := newTestSite(t)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/suite/merged", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

// The other tools aren't there in a test, so each comes back as not
// answering and auth's own entry still leads.
func TestMergedListsEveryTool(t *testing.T) {
	s, stub := newTestSite(t)
	_ = runInit(s.db)
	cookie := signIn(t, s, stub)
	req := httptest.NewRequest("GET", "/suite/merged", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var m merged
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Tools) != len(stateTools)+1 || m.Tools[0].Tool != "auth" || len(m.Tools[0].Items) == 0 {
		t.Fatalf("got %+v", m.Tools)
	}
}
