package main

import (
	"database/sql"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestThePartsAddUpToTheCost(t *testing.T) {
	for _, u := range []apiUsage{
		{InputTokens: 333, CacheReadInputTokens: 7777, CacheCreationInputTokens: 1234, OutputTokens: 999},
		{InputTokens: 150_000, OutputTokens: 3},
		{InputTokens: 1, CacheCreationInputTokens: 3},
	} {
		u.ServerToolUse.WebSearchRequests = 2
		p := priceParts(u)
		if p.micros() != costMicros(u) {
			t.Errorf("%+v: parts %d, cost %d", u, p.micros(), costMicros(u))
		}
	}
}

func TestACallsSpendPointsAtItsRow(t *testing.T) {
	s, secret, _ := fakeClaude(t, "application/json", `{"id":"m","type":"message","role":"assistant","model":"x",
		"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],`+usage+`}`)
	if rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	calls, _, err := s.store.FindCalls(callFilter{})
	if err != nil || len(calls) != 1 {
		t.Fatalf("%v %+v", err, calls)
	}
	c := calls[0]
	want := costMicros(apiUsage{InputTokens: 10, CacheReadInputTokens: 90, OutputTokens: 5})
	if c.Cost != want || c.Estimated {
		t.Errorf("cost %d estimated %v, want %d from the ledger", c.Cost, c.Estimated, want)
	}
	if c.InTok != 10 || c.CacheRead != 90 || c.PromptTok != 100 || c.OutputTok != 5 {
		t.Errorf("split %+v", c)
	}
	var callID, cr int64
	_ = s.store.db.QueryRow(`SELECT call_id, cache_read FROM spend`).Scan(&callID, &cr)
	if callID != c.ID || cr != 90 {
		t.Errorf("spend row has call %d and %d cache read, want %d and 90", callID, cr, c.ID)
	}
}

func TestAnOldCallIsPricedFromItsTokens(t *testing.T) {
	s := testStore(t)
	s.LogCall(Call{Caller: "dash", PromptTok: 2000, OutputTok: 100, Status: 200})
	calls, _ := s.Calls("dash", 5)
	want := costMicros(apiUsage{InputTokens: 2000, OutputTokens: 100})
	if len(calls) != 1 || !calls[0].Estimated || calls[0].Cost != want {
		t.Errorf("got %+v, want an estimate of %d", calls, want)
	}
}

func TestFiltersRunBeforeTheLimit(t *testing.T) {
	s := testStore(t)
	s.LogCall(Call{Caller: "cron", Status: 529, Err: "overloaded", Messages: `[{"role":"user","content":"price of 100% cotton"}]`})
	for range 30 {
		s.LogCall(Call{Caller: "dash", Status: 200, Messages: `[{"role":"user","content":"brief"}]`})
	}
	got, total, _ := s.FindCalls(callFilter{Failed: true, Limit: 10})
	if len(got) != 1 || total != 1 || got[0].Caller != "cron" {
		t.Errorf("failed only found %d of %d", len(got), total)
	}
	if got, total, _ := s.FindCalls(callFilter{Caller: "dash", Limit: 5}); len(got) != 5 || total != 30 {
		t.Errorf("dash found %d of %d", len(got), total)
	}
	if got, _, _ := s.FindCalls(callFilter{Q: "100%"}); len(got) != 1 {
		t.Errorf("a literal %% matched %d", len(got))
	}
	if got, _, _ := s.FindCalls(callFilter{Q: "1_0"}); len(got) != 0 {
		t.Errorf("an underscore matched as a wildcard")
	}
	today := time.Now().In(budgetZone)
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, budgetZone)
	if _, total, _ := s.FindCalls(callFilter{Day: midnight}); total != 31 {
		t.Errorf("today found %d", total)
	}
	if _, total, _ := s.FindCalls(callFilter{Day: midnight.AddDate(0, 0, -1)}); total != 0 {
		t.Errorf("yesterday found %d", total)
	}
	if _, total, _ := s.FindCalls(callFilter{Since: time.Now().Add(time.Minute)}); total != 0 {
		t.Errorf("a range in the future found %d", total)
	}
}

func TestTheCallsQueryRoundTrips(t *testing.T) {
	q, _ := url.ParseQuery("caller=cron&failed=1&range=7d&day=2026-11-03&q=price&limit=50&format=json")
	f := readCallsQuery(q, time.Now())
	if f.Caller != "cron" || !f.Failed || f.Range != "7d" || f.Day.IsZero() || f.Q != "price" || f.Limit != 50 {
		t.Fatalf("read %+v", f)
	}
	if got := f.url("failed", "", "caller", "dash"); got != "/calls?caller=dash&day=2026-11-03&limit=50&q=price&range=7d" {
		t.Errorf("url = %s", got)
	}
	if got := readCallsQuery(url.Values{"range": {"forever"}, "day": {"soon"}}, time.Now()); !got.Since.IsZero() || !got.Day.IsZero() {
		t.Errorf("junk was read as a filter: %+v", got)
	}
}

func TestAKeyOverItsCapIsRefused(t *testing.T) {
	s, secret, sent := fakeClaude(t, "application/json", okReply)
	k, _ := s.store.Authenticate(secret)
	_ = s.store.SetCap(k.ID, 5_000_000)
	s.store.LogSpend(k.ID, 5_000_000)
	rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`)
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"type":"insufficient_quota"`) || !strings.Contains(rec.Body.String(), "$5.00") {
		t.Errorf("status %d body %s", rec.Code, rec.Body)
	}
	srec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/search", strings.NewReader(`{"query":"q"}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	s.requireKey(s.search)(srec, req)
	if srec.Code != 422 {
		t.Errorf("a search over the cap got %d", srec.Code)
	}
	if len(*sent) != 0 {
		t.Errorf("%d calls reached the api", len(*sent))
	}

	// Another key's spend and last month's don't count against it.
	_, other, _ := s.store.NewKey("pond")
	s.store.LogSpend(other.ID, 1)
	_, _ = s.store.db.Exec(`UPDATE spend SET at = at - 40*86400 WHERE key_id = ?`, k.ID)
	if rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`); rec.Code != 200 {
		t.Errorf("under the cap got %d", rec.Code)
	}
}

func TestNoCapMeansNoLimit(t *testing.T) {
	s, secret, _ := fakeClaude(t, "application/json", okReply)
	k, _ := s.store.Authenticate(secret)
	s.store.LogSpend(k.ID, 50_000_000)
	if rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`); rec.Code != 200 {
		t.Errorf("a key with no cap got %d", rec.Code)
	}
}

func TestTheCapFormTakesDollars(t *testing.T) {
	s := &site{store: testStore(t)}
	_, k, _ := s.store.NewKey("cron")
	for _, c := range []struct {
		in   string
		code int
		want int64
	}{{"$12.50", 303, 12_500_000}, {"", 303, 0}, {"-3", 400, 0}, {"lots", 400, 0}} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/keys/1/cap", strings.NewReader(url.Values{"cap": {c.in}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("id", "1")
		s.keyCap(rec, req)
		keys, _ := s.store.Keys()
		if rec.Code != c.code || (c.code == 303 && keys[0].CapMicros != c.want) {
			t.Errorf("%q: %d and cap %d", c.in, rec.Code, keys[0].CapMicros)
		}
	}
	_ = k
}

func TestAnOlderDatabaseIsMigrated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE keys (id INTEGER PRIMARY KEY, name TEXT NOT NULL, hash TEXT NOT NULL UNIQUE, prefix TEXT NOT NULL,
			created_at INTEGER NOT NULL, last_used INTEGER NOT NULL DEFAULT 0, revoked_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE calls (id INTEGER PRIMARY KEY, key_id INTEGER NOT NULL DEFAULT 0, caller TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '', messages TEXT NOT NULL DEFAULT '[]', completion TEXT NOT NULL DEFAULT '',
			tools TEXT NOT NULL DEFAULT '', prompt_tok INTEGER NOT NULL DEFAULT 0, output_tok INTEGER NOT NULL DEFAULT 0,
			decode_tps REAL NOT NULL DEFAULT 0, ms INTEGER NOT NULL DEFAULT 0, status INTEGER NOT NULL DEFAULT 0,
			err TEXT NOT NULL DEFAULT '', at INTEGER NOT NULL)`,
		`CREATE TABLE spend (id INTEGER PRIMARY KEY, key_id INTEGER NOT NULL DEFAULT 0, micros INTEGER NOT NULL, at INTEGER NOT NULL)`,
		`INSERT INTO calls (caller, prompt_tok, output_tok, at) VALUES ('dash', 100, 10, 1)`,
		`INSERT INTO spend (key_id, micros, at) VALUES (1, 70, 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	for range 2 {
		s, err := OpenStore(path)
		if err != nil {
			t.Fatal(err)
		}
		calls, _ := s.Calls("dash", 5)
		if len(calls) != 1 || !calls[0].Estimated {
			t.Errorf("the old call reads as %+v", calls)
		}
		s.Close()
	}
}

func TestTheOverviewShowsTheNewPanels(t *testing.T) {
	s, secret, _ := fakeClaude(t, "application/json", `{"id":"m","type":"message","role":"assistant","model":"x",
		"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],`+usage+`}`)
	s.retain, s.budget = 90*24*time.Hour, 100
	if err := s.loadTemplates(); err != nil {
		t.Fatal(err)
	}
	call(t, s, secret, `{"messages":[{"role":"user","content":"what is up"}]}`)
	k, _ := s.store.Authenticate(secret)
	_ = s.store.SetCap(k.ID, 10_000_000)
	rec := httptest.NewRecorder()
	s.overview(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	for _, want := range []string{"p95 &middot; 24h", "Token mix", "cache read", "monthly cap $10.00", "No alert has gone out", "80%<b class=\"\">not yet", "/calls?id=1"} {
		if !strings.Contains(body, want) {
			t.Errorf("the overview is missing %q", want)
		}
	}
	rec = httptest.NewRecorder()
	s.callsPage(rec, httptest.NewRequest("GET", "/calls?q=what", nil))
	for _, want := range []string{"o-drawer", "what is up", "90 cache read", "not streamed"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the calls page is missing %q", want)
		}
	}
}
