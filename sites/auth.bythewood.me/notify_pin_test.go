package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"auth.bythewood.me/ntfy"
)

// pinNotifier is the real constructor pointed at a test server, so the token,
// the topic and the timeout are the ones a deploy gets.
func pinNotifier(t *testing.T, base string) *Notifier {
	t.Helper()
	n := NewNotifier()
	n.client.Base = base
	return n
}

func pinBase() string { return NewNotifier().client.Base }

// captureNtfy records every request as text, headers sorted, so a test can
// compare what reached ntfy byte for byte.
func captureNtfy(t *testing.T, status int) (*httptest.Server, *[]string) {
	t.Helper()
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var lines []string
		for k, v := range r.Header {
			lines = append(lines, k+": "+strings.Join(v, ","))
		}
		sort.Strings(lines)
		got = append(got, fmt.Sprintf("%s %s\n%s\n\n%s", r.Method, r.RequestURI, strings.Join(lines, "\n"), body))
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// captureLog swaps the default logger for one this test can read, with the
// time dropped so the lines compare.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

var pinPort = regexp.MustCompile(`127\.0\.0\.1:\d+`)

var pinCode = regexp.MustCompile(`Login code \d{6}`)

var pinVisitor = reqContext{IP: "203.0.113.9", Country: "US", City: "Hickory", UA: "pin-agent/1.0"}

func TestNotificationRequestsArePinned(t *testing.T) {
	t.Setenv("AUTH_NTFY_TOKEN", "tk_pin")
	logs := captureLog(t)
	srv, got := captureNtfy(t, http.StatusOK)

	n := pinNotifier(t, srv.URL)
	if !n.configured() {
		t.Fatal("a notifier with a token reads as unconfigured")
	}
	for _, m := range []ntfy.Message{
		codeMessage("123456", pinVisitor),
		sessionMessage(pinVisitor, "a login code"),
		recoveryMessage(pinVisitor, 7),
		lockoutMessage(pinVisitor),
	} {
		if err := n.publish(t.Context(), m); err != nil {
			t.Fatal(err)
		}
	}
	n.notify(codeMessage("654321", reqContext{IP: "198.51.100.4"}))

	want := []string{
		"POST /auth\n" +
			"Accept-Encoding: gzip\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 106\n" +
			"Priority: low\n" +
			"Tags: closed_lock_with_key\n" +
			"Title: Login code 123456\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"Asked for from Hickory, US (203.0.113.9).\nIf that is not you, ignore this and the code expires on its own.",
		"POST /auth\n" +
			"Accept-Encoding: gzip\n" +
			"Actions: view, Sessions, https://auth.bythewood.me/sessions\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 73\n" +
			"Priority: high\n" +
			"Tags: key\n" +
			"Title: New login to bythewood.me\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"Signed in with a login code from Hickory, US (203.0.113.9).\npin-agent/1.0",
		"POST /auth\n" +
			"Accept-Encoding: gzip\n" +
			"Actions: view, Security, https://auth.bythewood.me/security\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 112\n" +
			"Priority: high\n" +
			"Tags: rotating_light\n" +
			"Title: A recovery code was used\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"Signed in from Hickory, US (203.0.113.9) without the phone.\n7 codes left, and regenerating replaces all of them.",
		"POST /auth\n" +
			"Accept-Encoding: gzip\n" +
			"Actions: view, Activity, https://auth.bythewood.me/activity\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 95\n" +
			"Priority: high\n" +
			"Tags: rotating_light\n" +
			"Title: Login codes locked for a day\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"10 wrong codes in 24 hours, the last from Hickory, US (203.0.113.9).\nRecovery codes still work.",
		"POST /auth\n" +
			"Accept-Encoding: gzip\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 108\n" +
			"Priority: low\n" +
			"Tags: closed_lock_with_key\n" +
			"Title: Login code 654321\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"Asked for from 198.51.100.4 (198.51.100.4).\nIf that is not you, ignore this and the code expires on its own.",
	}
	if len(*got) != len(want) {
		t.Fatalf("ntfy got %d requests, want %d:\n%s", len(*got), len(want), strings.Join(*got, "\n----\n"))
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Errorf("request %d is\n%s\nwant\n%s", i, (*got)[i], want[i])
		}
	}
	if logs.String() != "" {
		t.Errorf("a clean publish logged %q", logs.String())
	}
}

func TestNotificationFailureAndMissingTokenArePinned(t *testing.T) {
	t.Setenv("AUTH_NTFY_TOKEN", "")
	logs := captureLog(t)
	srv, got := captureNtfy(t, http.StatusTooManyRequests)

	n := pinNotifier(t, srv.URL)
	if n.configured() {
		t.Error("a notifier with no token reads as configured")
	}
	err := n.publish(t.Context(), lockoutMessage(pinVisitor))
	if err == nil || err.Error() != "ntfy returned 429" {
		t.Errorf("publish against a 429 returned %v", err)
	}
	n.notify(lockoutMessage(pinVisitor))

	if len(*got) != 2 || strings.Contains((*got)[0], "Authorization") {
		t.Errorf("a missing token should still publish, unauthenticated: %v", *got)
	}
	wantLog := `level=WARN msg="AUTH_NTFY_TOKEN is unset; login codes cannot be delivered, so only recovery codes will work" component=auth
level=ERROR msg="publishing an alert failed" component=auth err="ntfy returned 429"
`
	if s := pinPort.ReplaceAllString(logs.String(), "HOST"); s != wantLog {
		t.Errorf("log is\n%s\nwant\n%s", s, wantLog)
	}
	if b := pinBase(); b != "http://orchard-ntfy:8000" {
		t.Errorf("publishes to %q", b)
	}
}

// The login flow end to end, so what the handlers hand the notifier is pinned
// along with what the notifier sends.
func TestLoginNotificationsArePinned(t *testing.T) {
	t.Setenv("AUTH_NTFY_TOKEN", "tk_pin")
	s, _ := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}
	srv, got := captureNtfy(t, http.StatusOK)
	s.notifier = pinNotifier(t, srv.URL)

	req := postForm("/login", url.Values{"username": {seedUsername}})
	req.Header.Set("CF-IPCountry", "US")
	req.Header.Set("User-Agent", "pin-agent/1.0")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if len(*got) != 1 {
		t.Fatalf("POST /login published %d notifications", len(*got))
	}
	code := strings.TrimPrefix(pinCode.FindString((*got)[0]), "Login code ")
	want := "POST /auth\n" +
		"Accept-Encoding: gzip\n" +
		"Authorization: Bearer tk_pin\n" +
		"Content-Length: 95\n" +
		"Priority: low\n" +
		"Tags: closed_lock_with_key\n" +
		"Title: Login code CODE\n" +
		"User-Agent: Go-http-client/1.1\n\n" +
		"Asked for from US (192.0.2.1).\nIf that is not you, ignore this and the code expires on its own."
	if g := strings.Replace((*got)[0], code, "CODE", 1); g != want {
		t.Errorf("the login code request is\n%s\nwant\n%s", g, want)
	}

	req = postForm("/code", url.Values{"code": {code}})
	req.Header.Set("CF-IPCountry", "US")
	req.Header.Set("User-Agent", "pin-agent/1.0")
	req.AddCookie(cookieNamed(rec.Result().Cookies(), pendingCookie))
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	for i := 0; i < 100 && len(*got) < 2; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if len(*got) != 2 {
		t.Fatalf("signing in published %d notifications, want 2", len(*got))
	}
	want = "POST /auth\n" +
		"Accept-Encoding: gzip\n" +
		"Actions: view, Sessions, https://auth.bythewood.me/sessions\n" +
		"Authorization: Bearer tk_pin\n" +
		"Content-Length: 56\n" +
		"Priority: high\n" +
		"Tags: key\n" +
		"Title: New login to bythewood.me\n" +
		"User-Agent: Go-http-client/1.1\n\n" +
		"Signed in with a code from US (192.0.2.1).\npin-agent/1.0"
	if (*got)[1] != want {
		t.Errorf("the session request is\n%s\nwant\n%s", (*got)[1], want)
	}
}
