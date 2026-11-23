package main

import (
	"database/sql"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"analytics.bythewood.me/suite"
	"analytics.bythewood.me/web"
)

// testSite renders the real templates, and a request with a session cookie
// is signed in.
func testSite(t *testing.T) (*site, uuid.UUID, int64) {
	t.Helper()
	db, id, base := twoVisitors(t)
	s, _, _ := testSiteOn(t, db, id)
	if _, err := db.Exec(`INSERT INTO goals (property_id, event, created_at) VALUES (?, 'signup', 0)`, id[:]); err != nil {
		t.Fatal(err)
	}
	if err := recordAlert(t.Context(), db, id, "link", "k", "test linked from news.example.org", "6 people came.", time.UnixMilli(base)); err != nil {
		t.Fatal(err)
	}
	return s, id, base
}

func testSiteOn(t *testing.T, db *sql.DB, id uuid.UUID) (*site, uuid.UUID, int64) {
	t.Helper()
	verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(verify.Close)
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := web.NewRenderer(suite.With(templates), suite.FuncsWith(templateFuncs), layoutTemplates, pageTemplates)
	if err != nil {
		t.Fatal(err)
	}
	return &site{db: db, renderer: renderer, auth: web.NewAuthenticatorAt(verify.URL)}, id, 0
}

func get(t *testing.T, s *site, id uuid.UUID, query string, signedIn bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/"+id.String()+"?"+query, nil)
	if signedIn {
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "x"})
	}
	w := httptest.NewRecorder()
	s.dashboard(w, r, id)
	return w
}

func TestDashboardRendersWithFilters(t *testing.T) {
	s, id, _ := testSite(t)
	w := get(t, s, id, "date_start=2026-11-01&date_end=2026-11-05&f_referrer=news.example.org&f_device=Mobile", true)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{
		`referrer news.example.org`, `device Mobile`, `name="f_referrer" value="news.example.org"`,
		`goal · signup`, `linked · news.example.org`, `data-pane="entry"`, `class="o-strip"`,
		`report=md`, `id="goal-form"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page is missing %q", want)
		}
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Error("a template value was refused as unsafe")
	}
}

// A public property shows the numbers and filters to anyone, and keeps the
// controls, the alerts and the site tag for the signed in.
func TestPublicDashboardHidesTheOperatorParts(t *testing.T) {
	s, id, _ := testSite(t)
	if _, err := s.db.Exec(`UPDATE properties SET is_public = 1 WHERE id = ?`, id[:]); err != nil {
		t.Fatal(err)
	}
	w := get(t, s, id, "date_start=2026-11-01&date_end=2026-11-05&f_country=US", false)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "country US") {
		t.Error("filters don't work signed out")
	}
	for _, hidden := range []string{"is-public-form", "Site tag", "goal-form", "linked from", "PDF report", "Delete"} {
		if strings.Contains(body, hidden) {
			t.Errorf("a signed out visitor sees %q", hidden)
		}
	}
	if w := get(t, s, id, "report=md", false); w.Code != http.StatusSeeOther {
		t.Errorf("a signed out report got %d", w.Code)
	}
}

func TestMarkdownReportCarriesTheFilters(t *testing.T) {
	s, id, _ := testSite(t)
	w := get(t, s, id, "date_start=2026-11-01&date_end=2026-11-05&f_referrer=news.example.org&report=md", true)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "referrer news.example.org") {
		t.Errorf("the report doesn't say what it's filtered to:\n%s", w.Body)
	}
}

func TestGoalsFormReplacesGoalsAndGoesBack(t *testing.T) {
	s, id, _ := testSite(t)
	r := httptest.NewRequest("POST", "/properties/"+id.String()+"/goals",
		strings.NewReader("goal=checkout&goal=signup&back=f_referrer%3Dx%26date_range%3D7"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", id.String())
	w := httptest.NewRecorder()
	s.propertyGoals(w, r)
	if loc := w.Header().Get("Location"); loc != "/"+id.String()+"?date_range=7&f_referrer=x" {
		t.Errorf("went back to %q", loc)
	}
	if got := strings.Join(goalEvents(t.Context(), s.db, id), " "); got != "signup checkout" {
		t.Errorf("goals %q", got)
	}
}

func TestPropertiesListRenders(t *testing.T) {
	s, _, _ := testSite(t)
	r := httptest.NewRequest("GET", "/properties", nil)
	r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "x"})
	w := httptest.NewRecorder()
	s.properties(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`class="o-strip six"`, `data-xs-when`, `visitors 7d`, `data-l="[`} {
		if !strings.Contains(body, want) {
			t.Errorf("the list is missing %q", want)
		}
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Error("a template value was refused as unsafe")
	}
}
