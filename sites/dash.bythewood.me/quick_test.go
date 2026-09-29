package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dash.bythewood.me/web"
)

// verifier stands in for auth.bythewood.me, where only the cookie "good" is a
// live session.
func verifier(t *testing.T) *web.Authenticator {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(web.SessionCookie); err == nil && c.Value == "good" {
			fmt.Fprint(w, `{"ok":true,"username":"isaac"}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"ok":false}`)
	}))
	t.Cleanup(srv.Close)
	return web.NewAuthenticatorAt(srv.URL)
}

// fakeChat counts what reaches it, so a refusal can be shown to have stopped
// at dash.
func fakeChat(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func quickSite(t *testing.T, chat string) http.Handler {
	s := &site{auth: verifier(t), chat: chat}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/quick", s.quick)
	mux.HandleFunc("POST /api/quick/send", s.quickSend)
	// Through the request logger, since a wrapper that hides the flusher is
	// how a stream here broke before.
	return web.Chain(mux, web.Logged)
}

func sendReq(cookie, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/quick/send", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: cookie})
	}
	return r
}

func TestQuickSaysWhetherSignedIn(t *testing.T) {
	h := quickSite(t, "http://127.0.0.1:1")
	for cookie, want := range map[string]bool{"": false, "stale": false, "good": true} {
		r := httptest.NewRequest(http.MethodGet, "/api/quick", nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: cookie})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		var got struct {
			SignedIn bool `json:"signed_in"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.SignedIn != want {
			t.Errorf("cookie %q: signed_in = %v", cookie, got.SignedIn)
		}
		// The page itself may be cached at the edge, so this answer must not be.
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("cookie %q: Cache-Control = %q", cookie, cc)
		}
	}
}

func TestQuickSendRefusesWithoutASession(t *testing.T) {
	chat, hits := fakeChat(t, func(w http.ResponseWriter, r *http.Request) {})
	h := quickSite(t, chat.URL)
	body := `{"message":"hello","run_id":"r1"}`

	for _, cookie := range []string{"", "stale"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, sendReq(cookie, body))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("cookie %q: status %d, want 401", cookie, w.Code)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("chat was asked %d times for a signed out caller", n)
	}
}

func TestQuickSendIsSameOriginOnly(t *testing.T) {
	chat, hits := fakeChat(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"kind\":\"done\"}\n\n")
	})
	h := quickSite(t, chat.URL)
	body := `{"message":"hello","run_id":"r1"}`

	for fetchSite, want := range map[string]int{
		"cross-site":  http.StatusForbidden,
		"same-site":   http.StatusForbidden, // another bythewood.me host carries the cookie too
		"none":        http.StatusForbidden,
		"same-origin": http.StatusOK,
		"":            http.StatusOK, // not a browser, and the session is still required
	} {
		r := sendReq("good", body)
		if fetchSite != "" {
			r.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Sec-Fetch-Site %q: status %d, want %d", fetchSite, w.Code, want)
		}
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("chat was asked %d times, want only the two allowed", n)
	}

	// A form posts text/plain across origins without a preflight.
	r := sendReq("good", body)
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: status %d, want 415", w.Code)
	}
}

// The relay streams, and a proxy that read chat to the end before answering
// would hold the whole turn back. The fake chat only sends its second event
// once the first has come out of dash.
func TestQuickSendStreamsChatThrough(t *testing.T) {
	gotFirst := make(chan struct{})
	chat, _ := fakeChat(t, func(w http.ResponseWriter, r *http.Request) {
		// Only the session goes to chat, nothing else of the caller's.
		if got := r.Header.Get("Cookie"); got != web.SessionCookie+"=good" {
			t.Errorf("chat got Cookie %q", got)
		}
		for _, h := range []string{"X-Forwarded-For", "Authorization", "Referer"} {
			if v := r.Header.Get(h); v != "" {
				t.Errorf("chat got %s %q", h, v)
			}
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Errorf("body: %v", err)
		}
		if in["message"] != "hello" || in["conversation_id"] != "c1" || in["run_id"] != "c1" || len(in) != 3 {
			t.Errorf("chat got %v", in)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"kind\":\"status\",\"text\":\"thinking\"}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-gotFirst:
		case <-time.After(5 * time.Second):
			t.Error("the first event never came out of dash")
		}
		fmt.Fprint(w, "data: {\"kind\":\"block\",\"html\":\"<p>hi</p>\"}\n\n")
		fmt.Fprint(w, "data: {\"kind\":\"done\",\"conversation_id\":\"c1\",\"html\":\"<p>hi</p>\"}\n\n")
	})
	dash := httptest.NewServer(quickSite(t, chat.URL))
	defer dash.Close()

	req, _ := http.NewRequest(http.MethodPost, dash.URL+"/api/quick/send",
		strings.NewReader(`{"message":" hello ","conversation_id":"c1","run_id":"c1","incognito":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("Authorization", "Bearer nope")
	req.Header.Set("Referer", "https://dash.bythewood.me/")
	req.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "good"})
	req.AddCookie(&http.Cookie{Name: "_ga", Value: "someone-elses"})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}

	lines := bufio.NewReader(resp.Body)
	first, err := lines.ReadString('\n')
	if err != nil || !strings.Contains(first, `"status"`) {
		t.Fatalf("first line %q, %v", first, err)
	}
	close(gotFirst)

	rest, _ := io.ReadAll(lines)
	for _, want := range []string{`"kind":"block"`, `"kind":"done","conversation_id":"c1"`} {
		if !strings.Contains(string(rest), want) {
			t.Errorf("stream is missing %s:\n%s", want, rest)
		}
	}
}

// chat checks the session again, and whatever it says is passed on as it is.
func TestQuickSendPassesChatRefusalsOn(t *testing.T) {
	chat, _ := fakeChat(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"not signed in"}`)
	})
	h := quickSite(t, chat.URL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, sendReq("good", `{"message":"hello","run_id":"r1"}`))
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "not signed in") {
		t.Errorf("status %d body %q", w.Code, w.Body.String())
	}

	// An empty question never leaves dash.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, sendReq("good", `{"message":"  ","run_id":"r1"}`))
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty message: status %d", w.Code)
	}
}
