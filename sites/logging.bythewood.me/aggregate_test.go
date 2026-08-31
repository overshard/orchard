package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The dash strip is a public page, so this endpoint is the fence: it may hand
// over counts and an up flag, and never a message, a path, a status code or an
// address. A field added here is a field published to the internet.
func TestAggregatePublishesCountsAndNothingElse(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	now := time.Now().UTC().UnixMilli()
	seedRows(t, db, []row{
		{source: "blog", ts: now, level: "INFO", msg: "request", component: "http", method: "GET", path: "/secret-admin-path", host: "blog.bythewood.me", status: 200, ip: "203.0.113.7", cfRay: "abc123"},
		{source: "blog", ts: now, level: "ERROR", msg: "database is on fire", component: "db"},
		{source: "repos", ts: now, level: "INFO", msg: "request", component: "http", status: 500},
	})

	rec := httptest.NewRecorder()
	s.aggregate(rec, httptest.NewRequest(http.MethodGet, "/aggregate", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, leaked := range []string{"secret-admin-path", "database is on fire", "203.0.113.7", "abc123"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the response carries %q, which must never leave this site", leaked)
		}
	}

	var payload struct {
		WindowHours int              `json:"window_hours"`
		Sources     []map[string]any `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.WindowHours != 24 {
		t.Errorf("window_hours = %d, want 24", payload.WindowHours)
	}

	allowed := map[string]bool{
		"source": true, "records": true, "errors": true,
		"requests": true, "server_5xx": true, "up": true, "up_known": true,
		"baseline_daily": true,
	}
	for _, src := range payload.Sources {
		for k := range src {
			if !allowed[k] {
				t.Errorf("aggregate publishes %q, which was not reviewed as safe for a public page", k)
			}
		}
	}

	byName := map[string]map[string]any{}
	for _, src := range payload.Sources {
		byName[src["source"].(string)] = src
	}
	if got := byName["blog"]["errors"]; got != float64(1) {
		t.Errorf("blog errors = %v, want 1", got)
	}
	if got := byName["repos"]["server_5xx"]; got != float64(1) {
		t.Errorf("repos server_5xx = %v, want 1", got)
	}
}

// healthz is the binary probing itself every thirty seconds, and on a quiet
// site it is most of what gets logged.
func TestAggregateExcludesHealthChecks(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	now := time.Now().UTC().UnixMilli()
	seedRows(t, db, []row{
		{source: "blog", ts: now, level: "INFO", msg: "request", component: "healthz", status: 200, rollupOnly: true},
		{source: "blog", ts: now, level: "INFO", msg: "request", component: "http", status: 200},
	})

	rec := httptest.NewRecorder()
	s.aggregate(rec, httptest.NewRequest(http.MethodGet, "/aggregate", nil))

	var payload struct {
		Sources []struct {
			Source  string `json:"source"`
			Records int64  `json:"records"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sources) != 1 {
		t.Fatalf("%d sources, want 1", len(payload.Sources))
	}
	if payload.Sources[0].Records != 1 {
		t.Errorf("records = %d, want 1 with the probe excluded", payload.Sources[0].Records)
	}
}

// The window is hour-floored, so a row older than the window is out and a row
// inside it is in. An unfloored start drops the bucket holding the start.
func TestAggregateWindowIsHourFloored(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	now := time.Now().UTC()
	seedRows(t, db, []row{
		{source: "blog", ts: now.Add(-23 * time.Hour).UnixMilli(), level: "ERROR", msg: "recent", component: "db"},
		{source: "blog", ts: now.Add(-72 * time.Hour).UnixMilli(), level: "ERROR", msg: "ancient", component: "db"},
	})

	rec := httptest.NewRecorder()
	s.aggregate(rec, httptest.NewRequest(http.MethodGet, "/aggregate", nil))

	var payload struct {
		Sources []struct {
			Errors int64 `json:"errors"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sources) != 1 || payload.Sources[0].Errors != 1 {
		t.Errorf("errors = %v, want only the one inside the window", payload.Sources)
	}
}

func TestAggregateReadsTheWatchdogLifecycleState(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	now := time.Now().UTC()
	seedRows(t, db, []row{
		{source: "blog", ts: now.UnixMilli(), level: "INFO", msg: "request", component: "http", status: 200},
	})
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)`,
		lifecycleKey("blog"), "up|"+strconv.FormatInt(now.UnixMilli(), 10)); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.aggregate(rec, httptest.NewRequest(http.MethodGet, "/aggregate", nil))

	var payload struct {
		Sources []struct {
			Up      bool `json:"up"`
			UpKnown bool `json:"up_known"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Sources[0].UpKnown || !payload.Sources[0].Up {
		t.Errorf("up=%v known=%v, want both true", payload.Sources[0].Up, payload.Sources[0].UpKnown)
	}
}

// The baseline is the week before the window, not including it, or a busy day
// would raise the bar it is being measured against.
func TestAggregateBaselineExcludesTheWindow(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)

	now := time.Now().UTC()
	rows := []row{}
	// Seven quiet days before the window.
	for d := 2; d <= 8; d++ {
		rows = append(rows, row{
			source: "blog", ts: now.AddDate(0, 0, -d).UnixMilli(),
			level: "INFO", msg: "request", component: "http", status: 200,
		})
	}
	// A busy day inside it.
	for i := 0; i < 20; i++ {
		rows = append(rows, row{
			source: "blog", ts: now.Add(-time.Hour).UnixMilli(),
			level: "INFO", msg: "request", component: "http", status: 200,
		})
	}
	seedRows(t, db, rows)

	rec := httptest.NewRecorder()
	s.aggregate(rec, httptest.NewRequest(http.MethodGet, "/aggregate", nil))

	var payload struct {
		Sources []struct {
			Requests      int64   `json:"requests"`
			BaselineDaily float64 `json:"baseline_daily"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sources) != 1 {
		t.Fatalf("%d sources, want 1", len(payload.Sources))
	}

	got := payload.Sources[0]
	if got.Requests != 20 {
		t.Errorf("requests = %d, want the 20 inside the window", got.Requests)
	}
	// Seven requests over seven days, and the busy day must not be in it.
	if got.BaselineDaily <= 0 || got.BaselineDaily > 2 {
		t.Errorf("baseline = %.2f/day, want about 1 with the window excluded", got.BaselineDaily)
	}
}
