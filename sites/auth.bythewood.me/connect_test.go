package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestConnectHandsASessionToMCP(t *testing.T) {
	s, stub := newTestSite(t)
	_ = runInit(s.db)
	cookie := signIn(t, s, stub)
	form := url.Values{"req": {"r1"}, "client": {"Claude"}, "host": {"claude.ai"}, "back": {mcpBack}, "allow": {"yes"}}
	req := postForm("/connect", form)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /connect: %d %s", rec.Code, rec.Body.String())
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if !strings.HasPrefix(loc.String(), mcpBack) || loc.Query().Get("req") != "r1" {
		t.Fatalf("redirected to %s", loc)
	}
	code := loc.Query().Get("handoff")

	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/verify/handoff?code="+url.QueryEscape(code), nil))
	var got map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got["session"] == "" {
		t.Fatalf("handoff: %d %s", rec.Code, rec.Body.String())
	}

	// The session works anywhere a cookie does, and the code only works once.
	verify := httptest.NewRequest("GET", "/verify", nil)
	verify.AddCookie(&http.Cookie{Name: sessionCookie, Value: got["session"]})
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, verify)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify with the connector's session: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/verify/handoff?code="+url.QueryEscape(code), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second handoff: %d", rec.Code)
	}
}

func TestConnectRefusesAnotherBack(t *testing.T) {
	s, stub := newTestSite(t)
	_ = runInit(s.db)
	cookie := signIn(t, s, stub)
	form := url.Values{"req": {"r1"}, "client": {"x"}, "back": {"https://evil.example/back"}, "allow": {"yes"}}
	req := postForm("/connect", form)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}
