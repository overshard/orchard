package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewPlaceNeedsAKnownPlaceToCompareWith(t *testing.T) {
	now := time.Now()
	ev := func(daysAgo int, city, country string) Event {
		return Event{TS: now.AddDate(0, 0, -daysAgo), Kind: evLogin, City: city, Country: country}
	}
	p := placesFrom([]Event{ev(40, "Lisbon", "PT"), ev(10, "Springfield", "US"), ev(2, "Columbus", "US")})

	for _, c := range []struct {
		where string
		at    time.Time
		want  bool
	}{
		{"Springfield, US", now, false},
		{"Lisbon, PT", now, true},
		{"Columbus, US", now.AddDate(0, 0, -2), true},
		{"Lisbon, PT", now.AddDate(0, 0, -40), false},
		{"", now, false},
	} {
		if got := p.isNew(c.where, c.at); got != c.want {
			t.Errorf("isNew(%q, %s) = %v, want %v", c.where, c.at.Format(time.DateOnly), got, c.want)
		}
	}
}

// seedSession adds a session as if it signed in from somewhere, with its
// login event, and returns its id.
func seedSession(t *testing.T, s *site, ua, city, country, method string, ago time.Duration) int64 {
	t.Helper()
	at := time.Now().Add(-ago).Unix()
	res, err := s.db.Exec(`INSERT INTO sessions (hash, created, last_seen, expires, ip, country, city, ua, method)
        VALUES (randomblob(32), ?, ?, ?, '192.0.2.1', ?, ?, ?, ?)`, at, at, at+86400*30, country, city, ua, method)
	if err != nil {
		t.Fatal(err)
	}
	how := "a code"
	if method == methodRecovery {
		how = "a recovery code"
	}
	if method != methodConnector {
		if _, err := s.db.Exec(`INSERT INTO events (ts, kind, country, city, ua, detail) VALUES (?, ?, ?, ?, ?, ?)`,
			at, evLogin, country, city, ua, how); err != nil {
			t.Fatal(err)
		}
	}
	id, _ := res.LastInsertId()
	return id
}

func TestSessionsSplitConnectorsAndFlagNewPlaces(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}
	seedSession(t, s, "Mozilla/5.0 Firefox/140.0", "Springfield", "US", methodCode, 9*24*time.Hour)
	seedSession(t, s, "Mozilla/5.0 Chrome/140.0", "Lisbon", "PT", methodRecovery, 2*time.Hour)
	seedSession(t, s, connectorUA+"Claude Code", "Springfield", "US", methodConnector, 20*24*time.Hour)
	cookie := signIn(t, s, stub)

	var method string
	if err := s.db.QueryRow(`SELECT method FROM sessions ORDER BY id DESC LIMIT 1`).Scan(&method); err != nil || method != methodCode {
		t.Fatalf("a phone code sign in stored method %q, %v", method, err)
	}

	ov, err := buildOverview(s.db, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Browsers) != 3 || len(ov.Connectors) != 1 {
		t.Fatalf("%d browsers and %d connectors", len(ov.Browsers), len(ov.Connectors))
	}
	if !ov.Browsers[0].NewPlace || ov.Browsers[0].Where != "Lisbon, PT" {
		t.Fatalf("the new place didn't sort first: %+v", ov.Browsers[0])
	}
	if c := ov.Connectors[0]; !c.Idle || c.Name != "Claude Code" || len(c.Days) != stripDays {
		t.Fatalf("connector row %+v", c)
	}
	if len(ov.Checkup) != 8 {
		t.Fatalf("%d checkup tiles", len(ov.Checkup))
	}

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"new place", "recovery code", "Claude Code", `id="connectors"`, "data-undo=", "data-confirm="} {
		if !strings.Contains(body, want) {
			t.Errorf("/sessions is missing %q", want)
		}
	}
}

// Undo sends the revoke as a beacon when the page is left early, a same
// origin urlencoded POST whose answer nobody reads.
func TestRevokeTakesABeacon(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}
	other := seedSession(t, s, connectorUA+"Claude", "", "US", methodConnector, time.Hour)
	cookie := signIn(t, s, stub)

	req := postForm("/sessions/revoke", url.Values{"id": {strconv.FormatInt(other, 10)}})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("beacon revoke: %d %s", rec.Code, rec.Body.String())
	}
	var revoked int
	_ = s.db.QueryRow(`SELECT revoked FROM sessions WHERE id = ?`, other).Scan(&revoked)
	if revoked != 1 {
		t.Fatal("the beacon didn't revoke the session")
	}
	var detail string
	_ = s.db.QueryRow(`SELECT detail FROM events WHERE kind = ? ORDER BY id DESC LIMIT 1`, evSessionRevoked).Scan(&detail)
	if detail != connectorUA+"Claude" {
		t.Fatalf("revoke recorded %q", detail)
	}
}

func TestActivityFilters(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}
	seedSession(t, s, "Mozilla/5.0 Firefox/140.0", "Springfield", "US", methodCode, 9*24*time.Hour)
	seedSession(t, s, "Mozilla/5.0 Chrome/140.0", "Lisbon", "PT", methodCode, 2*time.Hour)
	cookie := signIn(t, s, stub)

	get := func(path string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		return rec.Body.String()
	}
	if body := get("/activity?f=new"); !strings.Contains(body, "Lisbon, PT") || strings.Contains(body, "Springfield, US</td>") {
		t.Fatal("the new place filter didn't narrow to Lisbon")
	}
	if body := get("/activity?f=refused"); !strings.Contains(body, "Nothing under this filter") {
		t.Fatal("refused should be empty")
	}
	get("/activity?f=nonsense")
	if body := get("/activity?format=json"); !strings.Contains(body, `"sign_ins"`) {
		t.Fatalf("json form changed: %s", body)
	}
}

func TestConsentNamesAnExistingConnector(t *testing.T) {
	s, _ := newTestSite(t)
	seedSession(t, s, connectorUA+"Claude", "Lisbon", "PT", methodConnector, time.Hour)
	c := consentFor(s.db, "Claude", time.Now())
	if len(c.Existing) != 1 || len(c.Tools) == 0 || time.Until(c.Expires) < 89*24*time.Hour {
		t.Fatalf("consent %+v", c)
	}
	if c := consentFor(s.db, "Other", time.Now()); len(c.Existing) != 0 {
		t.Fatal("a different name matched")
	}
}
