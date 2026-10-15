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
	"time"
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
	n.Fire("silence", AlertContext{Source: "blog", Silent: 7 * time.Minute})
	n.Fire("resumed", AlertContext{Source: "blog", Silent: 90 * time.Minute})
	n.Fire("restart", AlertContext{Source: "blog", Detail: "Previous record seen 12 minutes ago."})
	n.Fire("catastrophe", AlertContext{Source: "blog"})

	want := []string{
		"POST /logging\n" +
			"Accept-Encoding: gzip\n" +
			"Actions: view, Open logs, https://logging.bythewood.me/sources/blog\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 157\n" +
			"Priority: high\n" +
			"Tags: mute\n" +
			"Title: blog has stopped logging\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"No records for 7 minutes.\nThe container may be down, wedged, or unable to reach ingest. Its own stdout is still the source of truth: docker logs orchard-blog",
		"POST /logging\n" +
			"Accept-Encoding: gzip\n" +
			"Actions: view, Open logs, https://logging.bythewood.me/sources/blog\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 44\n" +
			"Priority: default\n" +
			"Tags: white_check_mark\n" +
			"Title: blog is logging again\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"Records are arriving after 1h30m of silence.",
		"POST /logging\n" +
			"Accept-Encoding: gzip\n" +
			"Actions: view, Open logs, https://logging.bythewood.me/sources/blog\n" +
			"Authorization: Bearer tk_pin\n" +
			"Content-Length: 147\n" +
			"Priority: default\n" +
			"Tags: warning\n" +
			"Title: blog restarted without a clean shutdown\n" +
			"User-Agent: Go-http-client/1.1\n\n" +
			"It started listening with no shutting-down record before it, so the previous process did not exit on a signal.\nPrevious record seen 12 minutes ago.",
	}
	if len(*got) != len(want) {
		t.Fatalf("ntfy got %d requests, want %d:\n%s", len(*got), len(want), strings.Join(*got, "\n----\n"))
	}
	for i := range want {
		if (*got)[i] != want[i] {
			t.Errorf("request %d is\n%s\nwant\n%s", i, (*got)[i], want[i])
		}
	}

	wantLog := `level=INFO msg="alert: published silence for blog" component=alerts
level=INFO msg="alert: published resumed for blog" component=alerts
level=INFO msg="alert: published restart for blog" component=alerts
level=INFO msg="alert: unknown kind \"catastrophe\" for blog" component=alerts
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
	n.Fire("silence", AlertContext{Source: "blog", Silent: 7 * time.Minute})

	if len(*got) != 1 || strings.Contains((*got)[0], "Authorization") {
		t.Errorf("a missing token should still publish, unauthenticated: %v", *got)
	}
	wantLog := `level=WARN msg="NTFY_TOKEN is unset; alerts will be rendered and refused, not delivered" component=alerts
level=ERROR msg="alert: publishing silence for blog failed: ntfy returned 403" component=alerts
`
	if s := pinPort.ReplaceAllString(logs.String(), "HOST"); s != wantLog {
		t.Errorf("log is\n%s\nwant\n%s", s, wantLog)
	}
	if b := pinBase(); b != "http://orchard-ntfy:8000" {
		t.Errorf("publishes to %q", b)
	}
}
