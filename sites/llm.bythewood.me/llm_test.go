package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "llm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAKeyIsUsableOnceAndNeverRecoverable(t *testing.T) {
	s := testStore(t)
	secret, k, err := s.NewKey("chat")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, keyPrefix) {
		t.Errorf("secret = %q, want the %s prefix", secret, keyPrefix)
	}
	got, ok := s.Authenticate(secret)
	if !ok {
		t.Fatal("a fresh key did not authenticate")
	}
	if got.Name != "chat" || got.ID != k.ID {
		t.Errorf("resolved to %+v", got)
	}
	// The plaintext must not be anywhere the listing can reach.
	keys, err := s.Keys()
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range keys {
		if strings.Contains(secret, listed.Prefix) && len(listed.Prefix) >= len(secret) {
			t.Error("the whole secret is in the listing")
		}
	}
}

func TestAWrongKeyIsRefused(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.NewKey("chat"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "nonsense", keyPrefix + "wrong", "Bearer something"} {
		if _, ok := s.Authenticate(bad); ok {
			t.Errorf("%q authenticated", bad)
		}
	}
}

// Revoking has to take effect on the next request, which is the whole reason
// the key is checked every time rather than cached.
func TestRevokingStopsAKeyImmediately(t *testing.T) {
	s := testStore(t)
	secret, k, _ := s.NewKey("search")
	if _, ok := s.Authenticate(secret); !ok {
		t.Fatal("not usable before revoking")
	}
	if err := s.Revoke(k.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate(secret); ok {
		t.Error("a revoked key still authenticates")
	}
}

func TestAKeyNeedsAName(t *testing.T) {
	s := testStore(t)
	if _, _, err := s.NewKey("   "); err == nil {
		t.Error("an unnamed key was accepted")
	}
}

func TestCallsAreLoggedAndRolledUp(t *testing.T) {
	s := testStore(t)
	_, k, _ := s.NewKey("chat")
	s.LogCall(Call{KeyID: k.ID, Caller: "chat", Model: "local",
		Messages: `[{"role":"user","content":"hello"}]`, Completion: "hi",
		PromptTok: 10, OutputTok: 4, DecodeTPS: 60, Status: 200})
	s.LogCall(Call{KeyID: k.ID, Caller: "chat", Status: 500, Err: "upstream refused"})

	calls, err := s.Calls("chat", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls", len(calls))
	}
	// The prompt and the completion are the point of the log, so assert they
	// survived rather than only that a row exists.
	if !strings.Contains(calls[1].Messages, "hello") || calls[1].Completion != "hi" {
		t.Errorf("the text was not kept: %+v", calls[1])
	}

	usage, err := s.Usage(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 {
		t.Fatalf("got %d callers", len(usage))
	}
	u := usage[0]
	if u.Calls != 2 || u.PromptTok != 10 || u.OutputTok != 4 || u.Errors != 1 {
		t.Errorf("usage = %+v", u)
	}
}

func TestPruneDropsOldCallsOnly(t *testing.T) {
	s := testStore(t)
	s.LogCall(Call{Caller: "chat", Completion: "recent"})
	if _, err := s.db.Exec(`INSERT INTO calls(caller, completion, at) VALUES(?,?,?)`,
		"chat", "ancient", time.Now().Add(-200*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	n, err := s.Prune(90 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned %d rows, want 1", n)
	}
	calls, _ := s.Calls("", 10)
	if len(calls) != 1 || calls[0].Completion != "recent" {
		t.Errorf("wrong row survived: %+v", calls)
	}
}

// The gateway is the security boundary, so an unkeyed request must not reach
// upstream at all rather than being refused after it.
func TestAnUnkeyedRequestNeverReachesUpstream(t *testing.T) {
	reached := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	defer up.Close()

	s := &site{store: testStore(t), upstream: up.URL, client: up.Client()}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"local"}`))
	s.requireKey(s.completions)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if reached {
		t.Error("an unkeyed request was forwarded upstream")
	}
}

func TestAKeyedCallIsForwardedAndWrittenDown(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"the answer"}}],
			"usage":{"prompt_tokens":31,"completion_tokens":7},
			"timings":{"predicted_per_second":58.5}}`)
	}))
	defer up.Close()

	store := testStore(t)
	secret, _, _ := store.NewKey("chat")
	s := &site{store: store, upstream: up.URL, client: up.Client()}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"local","messages":[{"role":"user","content":"ask"}]}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	s.requireKey(s.completions)(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "the answer") {
		t.Errorf("the caller did not get the upstream body: %s", rec.Body.String())
	}

	calls, _ := store.Calls("chat", 5)
	if len(calls) != 1 {
		t.Fatalf("logged %d calls", len(calls))
	}
	c := calls[0]
	if c.Completion != "the answer" {
		t.Errorf("completion = %q", c.Completion)
	}
	if c.PromptTok != 31 || c.OutputTok != 7 {
		t.Errorf("tokens = %d/%d, want 31/7", c.PromptTok, c.OutputTok)
	}
	if !strings.Contains(c.Messages, "ask") {
		t.Errorf("the prompt was not kept: %q", c.Messages)
	}
	if c.Status != 200 {
		t.Errorf("status = %d", c.Status)
	}
}

// Incognito is the one case where a call runs and leaves nothing behind, so the
// test is that the answer still arrives and the log is still empty.
func TestAnIncognitoCallIsForwardedAndNotWrittenDown(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"the answer"}}]}`)
	}))
	defer up.Close()

	store := testStore(t)
	secret, _, _ := store.NewKey("chat")
	s := &site{store: store, upstream: up.URL, client: up.Client()}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"local","messages":[{"role":"user","content":"a secret"}]}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set(incognitoHeader, "1")
	s.requireKey(s.completions)(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "the answer") {
		t.Errorf("the caller did not get the upstream body: %s", rec.Body.String())
	}
	calls, _ := store.Calls("", 5)
	if len(calls) != 0 {
		t.Fatalf("an incognito call was logged: %+v", calls)
	}
}

// A streamed answer is the one worth recording and the one easiest to lose,
// since the text only exists as deltas passing through.
func TestAStreamedCallIsReassembledForTheLog(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []string{
			`{"choices":[{"delta":{"content":"one "}}]}`,
			`{"choices":[{"delta":{"content":"two"}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":2},"timings":{"predicted_per_second":61.0}}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer up.Close()

	store := testStore(t)
	secret, _, _ := store.NewKey("search")
	s := &site{store: store, upstream: up.URL, client: up.Client()}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"local","stream":true,"messages":[{"role":"user","content":"go"}]}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	s.requireKey(s.completions)(rec, req)

	// The caller gets the events unchanged.
	if !strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Errorf("the stream did not pass through:\n%s", rec.Body.String())
	}
	calls, _ := store.Calls("search", 5)
	if len(calls) != 1 {
		t.Fatalf("logged %d calls", len(calls))
	}
	if calls[0].Completion != "one two" {
		t.Errorf("completion = %q, want %q", calls[0].Completion, "one two")
	}
	if calls[0].OutputTok != 2 || calls[0].DecodeTPS != 61.0 {
		t.Errorf("stats lost: %d tokens, %.1f tok/s", calls[0].OutputTok, calls[0].DecodeTPS)
	}
}

// A caller whose key is refused upstream still gets a row, since a log with the
// failures missing is the one that cannot explain an outage.
func TestAnUpstreamFailureIsStillLogged(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no model loaded", http.StatusServiceUnavailable)
	}))
	defer up.Close()

	store := testStore(t)
	secret, _, _ := store.NewKey("chat")
	s := &site{store: store, upstream: up.URL, client: up.Client()}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"local"}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	s.requireKey(s.completions)(rec, req)

	calls, _ := store.Calls("chat", 5)
	if len(calls) != 1 {
		t.Fatalf("logged %d calls", len(calls))
	}
	if calls[0].Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", calls[0].Status)
	}
}

func TestTheApiKeyHeaderIsAcceptedToo(t *testing.T) {
	store := testStore(t)
	secret, _, _ := store.NewKey("aiagent")
	s := &site{store: store}
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r.Header.Set("X-Api-Key", secret)
	if _, ok := s.authenticate(r); !ok {
		t.Error("a key sent as X-Api-Key was refused")
	}
}

func TestShortRendersMessagesReadably(t *testing.T) {
	msgs, _ := json.Marshal([]map[string]string{
		{"role": "system", "content": "be brief"},
		{"role": "user", "content": "what is the time"},
	})
	got := short(string(msgs), 200)
	if !strings.Contains(got, "system: be brief") || !strings.Contains(got, "user: what is the time") {
		t.Errorf("got %q", got)
	}
	if len(short(string(msgs), 10)) > 13 {
		t.Errorf("not truncated: %q", short(string(msgs), 10))
	}
}
