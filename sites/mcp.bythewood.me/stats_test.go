package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcp.bythewood.me/web"
)

func seeded(t *testing.T) *site {
	t.Helper()
	s, _ := testSite(t)
	now := time.Now()
	g, _ := s.store.AddGrant(knownClients[0], "Claude", "sess-1", resourceURL)
	add := func(ago time.Duration, c Call) {
		c.At, c.Grant, c.Client = now.Add(-ago), g, "Claude"
		if c.Method == "" {
			c.Method = "tools/call"
		}
		s.store.AddCall(c)
	}
	add(time.Hour, Call{Method: "initialize", OK: true, RPCID: "0"})
	add(50*time.Minute, Call{Tool: "status_report", Args: `{"property":"shop.example.com"}`, MS: 10012, Err: "status didn't answer: context deadline exceeded (Client.Timeout exceeded)", RPCID: "4"})
	add(40*time.Minute, Call{Tool: "status_report", Args: `{"property":"shop.example.com"}`, MS: 300, OK: true, Chars: 900, Preview: strings.Repeat("x", 600) + "…", RPCID: "5"})
	add(30*time.Minute, Call{Tool: "logs_search", Args: `{"query":"timeout","source":"shop"}`, MS: 410, OK: true, Chars: 40, Preview: "171 lines", RPCID: `"a"`})
	add(3*24*time.Hour, Call{Tool: "orchard_brief", MS: 620, OK: true, Chars: 20, Preview: "fine", RPCID: "2"})
	add(20*24*time.Hour, Call{Tool: "cron_jobs", MS: 60, OK: true, RPCID: "1"})
	add(10*time.Minute, Call{Tool: "nope", Args: `{}`, Err: "no such tool", Code: -32602, RPCID: "9"})
	return s
}

func TestToolRowsPutTroubleFirstAndListEveryTool(t *testing.T) {
	s := seeded(t)
	rows := s.store.toolRows(time.Now())
	if len(rows) != len(tools) {
		t.Fatalf("got %d rows for %d tools", len(rows), len(tools))
	}
	if rows[0].Name != "status_report" || rows[0].Failed != 1 || rows[0].Calls != 2 || !strings.Contains(rows[0].LastErr, "deadline") {
		t.Fatalf("first row %+v", rows[0])
	}
	if rows[0].Days[statDays-1].Kind != "b" || len(rows[0].Week) != 7 {
		t.Fatalf("days %+v week %v", rows[0].Days, rows[0].Week)
	}
	last := rows[len(rows)-1]
	if !last.LastUsed.IsZero() || last.Calls != 0 {
		t.Fatalf("last row should be a never called tool, got %+v", last)
	}
	for _, r := range rows {
		if r.Name == "cron_jobs" && (r.Calls != 0 || r.LastUsed.IsZero()) {
			t.Fatalf("cron_jobs was called 20 days ago, got %+v", r)
		}
	}
}

func TestCauseFoldsErrors(t *testing.T) {
	for err, want := range map[string]string{
		"status didn't answer: context deadline exceeded (Client.Timeout exceeded)": "timeout",
		"logs didn't answer: dial tcp: no such host":                                "tool unreachable",
		"property is required":                                       "bad argument",
		"days must be a whole number":                                "bad argument",
		`no property matches "x". The properties are: a, b`:          "not found",
		`"b" matches more than one property, pick one of: blog, bar`: "ambiguous name",
		"no such tool":                     "unknown tool",
		"session revoked":                  "signed out",
		"gate refused it: bad when":        "refused by gate",
		"status answered 502":              "tool errored",
		"something nobody has seen before": "other",
	} {
		if got := cause(err); got != want {
			t.Errorf("%q: got %q, want %q", err, got, want)
		}
	}
}

func TestCallFilterSearchesArgumentsAndRange(t *testing.T) {
	s := seeded(t)
	all := CallFilter{Since: time.Now().Add(-keepFor)}
	if n, failed := s.store.CountCalls(all); n != 7 || failed != 2 {
		t.Fatalf("got %d, %d failed", n, failed)
	}
	q := all
	q.Q = "shop"
	if n, _ := s.store.CountCalls(q); n != 3 {
		t.Fatalf("q=shop got %d", n)
	}
	q.Q = "%"
	if n, _ := s.store.CountCalls(q); n != 0 {
		t.Fatalf("a percent sign is a literal, got %d", n)
	}
	day := CallFilter{Since: time.Now().Add(-24 * time.Hour)}
	if n, _ := s.store.CountCalls(day); n != 5 {
		t.Fatalf("24h got %d", n)
	}
}

func TestExchangeRebuildsTheJSONRPC(t *testing.T) {
	s := seeded(t)
	calls, _ := s.store.Calls(CallFilter{Since: time.Now().Add(-keepFor)})
	by := map[string]Call{}
	for _, c := range calls {
		by[c.RPCID] = c
	}
	ok := by["5"].Exchange()
	if !strings.Contains(ok.Request, `"name": "status_report"`) || !strings.Contains(ok.Request, `"property": "shop.example.com"`) ||
		!strings.Contains(ok.Response, `"isError": false`) || !strings.Contains(ok.Note, "first 600 of 900") {
		t.Fatalf("got %+v", ok)
	}
	bad := by["9"].Exchange()
	if !strings.Contains(bad.Response, `"code": -32602`) {
		t.Fatalf("got %+v", bad)
	}
	if str := by[`"a"`].Exchange(); !strings.Contains(str.Request, `"id": "a"`) {
		t.Fatalf("got %+v", str)
	}
	h := string(jsonHTML(ok.Request))
	if !strings.Contains(h, `<span class="k">&#34;jsonrpc&#34;</span>:`) || !strings.Contains(h, `<span class="n">5</span>`) {
		t.Fatalf("got %s", h)
	}
}

func TestOpenStoreAddsColumnsToAnOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE calls (id INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, grant_id INTEGER NOT NULL, client TEXT NOT NULL, method TEXT NOT NULL, tool TEXT NOT NULL DEFAULT '', args TEXT NOT NULL DEFAULT '', ms INTEGER NOT NULL, ok INTEGER NOT NULL, err TEXT NOT NULL DEFAULT '', chars INTEGER NOT NULL DEFAULT 0, preview TEXT NOT NULL DEFAULT '');
		INSERT INTO calls (at, grant_id, client, method, ms, ok) VALUES (1, 1, 'Claude', 'ping', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 {
		st, err := OpenStore(path)
		if err != nil {
			t.Fatal(err)
		}
		calls, err := st.Calls(CallFilter{})
		if err != nil || len(calls) != 1 || calls[0].RPCID != "" {
			t.Fatalf("got %v %v", calls, err)
		}
		st.Close()
	}
}

func TestSignedInPages(t *testing.T) {
	s := seeded(t)
	verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) }))
	t.Cleanup(verify.Close)
	s.auth = web.NewAuthenticatorAt(verify.URL)
	h := s.handler()
	for path, want := range map[string][]string{
		"/":                         {"Failures by cause", "ends in 89d", "timeout"},
		"/tools":                    {"status_report", "repo_readme", "never called", "o-ds", "Failures by cause", "property=shop.example.com"},
		"/calls":                    {"7 calls", "view-raw", "the first 600 kept"},
		"/calls?q=shop&range=24h":   {"3 calls", "logs_search"},
		"/calls?failed=1&range=24h": {"2 calls"},
		"/calls?id=1":               {"cron_jobs"},
		"/docs":                     {"last handshake", "not connected", "read only", "proposes"},
	} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "x"})
		rec := do(t, h, r)
		if rec.Code != 200 {
			t.Fatalf("%s: got %d", path, rec.Code)
		}
		for _, w := range want {
			if !strings.Contains(rec.Body.String(), w) {
				t.Errorf("%s: no %q", path, w)
			}
		}
	}
}
