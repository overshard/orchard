package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
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

func TestAlertRequestsArePinned(t *testing.T) {
	t.Setenv("NTFY_TOKEN", "tk_pin")
	logs := captureLog(t)
	srv, got := captureNtfy(t, http.StatusOK)

	n := pinNotifier(t, srv.URL)
	ctx := AlertContext{ID: "0b1c", Name: "example.com", URL: "https://example.com", CurrentStatus: 503, AvgResponseMS: 184}
	n.Fire("down", ctx)
	ctx.CurrentStatus = 200
	n.Send("recovery", ctx)
	n.Wait()
	n.Fire("sideways", ctx)

	want := []string{
		"POST /status\n" +
			"Accept-Encoding: gzip\n" +
			"Actions: view, Open status, https://status.bythewood.me/0b1c\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 104\n" +
			"Priority: high\n" +
			"Tags: rotating_light\n" +
			"Title: example.com is down\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"https://example.com\nTwo consecutive checks failed. Latest status: 503.\nRolling average response: 184 ms.",
		"POST /status\n" +
			"Accept-Encoding: gzip\n" +
			"Actions: view, Open status, https://status.bythewood.me/0b1c\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 84\n" +
			"Priority: default\n" +
			"Tags: white_check_mark\n" +
			"Title: example.com is back up\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"https://example.com\nThe latest check returned 200.\nRolling average response: 184 ms.",
	}
	if len(*got) != len(want) {
		t.Fatalf("ntfy got %d requests, want %d:\n%s", len(*got), len(want), strings.Join(*got, "\n----\n"))
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Errorf("request %d is\n%s\nwant\n%s", i, (*got)[i], want[i])
		}
	}

	wantLog := `level=INFO msg="alert: published down for https://example.com"
level=INFO msg="alert: published recovery for https://example.com"
level=INFO msg="alert: unknown kind \"sideways\" for https://example.com"
`
	if logs.String() != wantLog {
		t.Errorf("log is\n%s\nwant\n%s", logs.String(), wantLog)
	}
}

func TestAlertFailureAndMissingTokenArePinned(t *testing.T) {
	t.Setenv("NTFY_TOKEN", "")
	logs := captureLog(t)
	srv, got := captureNtfy(t, http.StatusForbidden)

	n := pinNotifier(t, srv.URL)
	n.Fire("down", AlertContext{ID: "0b1c", Name: "example.com", URL: "https://example.com", CurrentStatus: 503, AvgResponseMS: 184})

	if len(*got) != 1 || strings.Contains((*got)[0], "Authorization") {
		t.Errorf("a missing token should still publish, unauthenticated: %v", *got)
	}
	wantLog := `level=WARN msg="NTFY_TOKEN is unset; alerts will be rendered and refused, not delivered" component=alerts
level=ERROR msg="alert: publishing down for https://example.com failed: ntfy returned 403"
`
	if s := pinPort.ReplaceAllString(logs.String(), "HOST"); s != wantLog {
		t.Errorf("log is\n%s\nwant\n%s", s, wantLog)
	}
	if b := pinBase(); b != "http://orchard-ntfy:8000" {
		t.Errorf("publishes to %q", b)
	}
}
