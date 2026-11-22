package main

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /connect: %d %s", rec.Code, rec.Body.String())
	}
	m := regexp.MustCompile(`http-equiv="refresh" content="0; url=([^"]+)"`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("no refresh in %s", rec.Body.String())
	}
	loc, _ := url.Parse(html.UnescapeString(m[1]))
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

	// verify names it a connector so the other sites keep it to reading, and
	// the code only works once.
	verify := httptest.NewRequest("GET", "/verify", nil)
	verify.AddCookie(&http.Cookie{Name: sessionCookie, Value: got["session"]})
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, verify)
	var said map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &said)
	if rec.Code != http.StatusOK || said["ok"] != true || said["method"] != methodConnector {
		t.Fatalf("verify with the connector's session: %d %s", rec.Code, rec.Body.String())
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

func TestConnectorSessionOnlyReadsHere(t *testing.T) {
	s, stub := newTestSite(t)
	_ = runInit(s.db)
	browser := signIn(t, s, stub)
	value, err := newConnectorSession(s.db, httptest.NewRequest("POST", "/connect", nil), "Claude")
	if err != nil {
		t.Fatal(err)
	}
	agent := &http.Cookie{Name: sessionCookie, Value: value}

	req := httptest.NewRequest("GET", "/activity?format=json", nil)
	req.AddCookie(agent)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("connector GET /activity: %d", rec.Code)
	}

	req = postForm("/sessions/revoke-others", url.Values{})
	req.AddCookie(agent)
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("connector POST /sessions/revoke-others: %d, want 403", rec.Code)
	}
	if _, err := lookupSession(s.db, withCookie(browser)); err != nil {
		t.Fatalf("the browser's session didn't survive the connector's POST: %v", err)
	}

	req = postForm("/sessions/revoke-others", url.Values{})
	req.AddCookie(browser)
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("browser POST /sessions/revoke-others: %d", rec.Code)
	}
	if _, err := lookupSession(s.db, withCookie(agent)); err == nil {
		t.Fatal("the browser's revoke-others left the connector's session alive")
	}
}

func withCookie(c *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	return r
}

func TestConsentSaysWhenItStartedAndWarns(t *testing.T) {
	s, stub := newTestSite(t)
	_ = runInit(s.db)
	cookie := signIn(t, s, stub)
	q := url.Values{"req": {"r1"}, "client": {"Claude"}, "host": {"claude.ai"}, "back": {mcpBack}, "started": {"1700000000"}}
	req := httptest.NewRequest("GET", "/connect?"+q.Encode(), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "2023-11-14 22:13 UTC") || !strings.Contains(body, "just clicked Connect in Claude yourself") {
		t.Fatalf("consent page: %d %s", rec.Code, body)
	}
}

// gate:write only reaches mcp when the agent asked for it and it stayed ticked.
func TestConnectGrantsGateWriteOnlyWhenTicked(t *testing.T) {
	s, stub := newTestSite(t)
	_ = runInit(s.db)
	cookie := signIn(t, s, stub)

	page := httptest.NewRequest("GET", "/connect?"+url.Values{"req": {"r1"}, "client": {"Claude"}, "back": {mcpBack}, "scope": {"read gate:write"}}.Encode(), nil)
	page.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, page)
	if !strings.Contains(rec.Body.String(), `name="write"`) {
		t.Fatalf("the consent page doesn't offer gate:write: %d", rec.Code)
	}

	for _, c := range []struct {
		scope, write, want string
	}{
		{"read gate:write", "yes", "read gate:write"},
		{"read gate:write", "", "read"},
		{"read", "yes", "read"},
	} {
		form := url.Values{"req": {"r1"}, "client": {"Claude"}, "back": {mcpBack}, "allow": {"yes"}, "scope": {c.scope}, "write": {c.write}}
		req := postForm("/connect", form)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		m := regexp.MustCompile(`http-equiv="refresh" content="0; url=([^"]+)"`).FindStringSubmatch(rec.Body.String())
		if m == nil {
			t.Fatalf("no refresh in %s", rec.Body.String())
		}
		loc, _ := url.Parse(html.UnescapeString(m[1]))
		rec = httptest.NewRecorder()
		s.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/verify/handoff?code="+url.QueryEscape(loc.Query().Get("handoff")), nil))
		var got map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["scope"] != c.want {
			t.Errorf("asked %q, write=%q: granted %q, want %q", c.scope, c.write, got["scope"], c.want)
		}
	}
}
