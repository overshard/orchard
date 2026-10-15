package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// pinRewind moves one endpoint's clock back, so a test can step past a pace,
// a window or an open breaker without waiting it out.
func pinRewind(g *Guard, name string, d time.Duration) {
	g.file.mu.Lock()
	defer g.file.mu.Unlock()
	e := g.file.entry(name)
	e.LastCall = e.LastCall.Add(-d)
	e.OpenUntil = e.OpenUntil.Add(-d)
	e.WindowStart = e.WindowStart.Add(-d)
}

type pinReply struct {
	status int
	header map[string]string
	body   string
}

// pinUpstream answers with each reply in turn and counts what reached it.
func pinUpstream(t *testing.T, replies ...pinReply) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if hits >= len(replies) {
			t.Errorf("an unscripted request reached the upstream")
			w.WriteHeader(http.StatusTeapot)
			return
		}
		rep := replies[hits]
		hits++
		for k, v := range rep.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(rep.status)
		fmt.Fprint(w, rep.body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

var (
	pinDuration = regexp.MustCompile(`\b(\d+h)?(\d+m)?\d+(\.\d+)?(s|ms|µs)\b`)
	pinHostPort = regexp.MustCompile(`127\.0\.0\.1:\d+`)
)

// pinNormal rounds every duration in s, to 10ms for a short one and ten
// seconds for the rest, since a breaker's time left has moved on by the time it is read.
func pinNormal(s string) string {
	s = pinHostPort.ReplaceAllString(s, "HOST")
	return pinDuration.ReplaceAllStringFunc(s, func(m string) string {
		d, err := time.ParseDuration(m)
		if err != nil {
			return m
		}
		if d < 10*time.Second {
			return d.Round(10 * time.Millisecond).String()
		}
		return d.Round(10 * time.Second).String()
	})
}

func pinLog(t *testing.T) *bytes.Buffer {
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

func pinCompare(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") == strings.Join(want, "\n") {
		return
	}
	t.Errorf("the sequence changed.\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
}

// The breaker through the real fetch path, against an upstream that fails every
// way it can: errors, 429 and 503 with Retry-After in both forms, a decode
// failure and a dead address.
func TestGuardBreakerSequenceIsPinned(t *testing.T) {
	logs := pinLog(t)
	g := NewGuard(t.TempDir())
	date := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(http.TimeFormat) }
	srv, hits := pinUpstream(t,
		pinReply{status: 200, body: "{}"},
		pinReply{status: 500},
		pinReply{status: 404, header: map[string]string{"Retry-After": "900"}},
		pinReply{status: 200, body: "not json"},
		pinReply{status: 500},
		pinReply{status: 200, body: "{}"},
		pinReply{status: 429, header: map[string]string{"Retry-After": "120"}},
		pinReply{status: 429, header: map[string]string{"Retry-After": date(5 * time.Minute)}},
		pinReply{status: 503},
		pinReply{status: 503},
		pinReply{status: 503},
		pinReply{status: 503},
		pinReply{status: 503},
		pinReply{status: 503},
		pinReply{status: 502, header: map[string]string{"Retry-After": "3600"}},
		pinReply{status: 200, body: "{}"},
		pinReply{status: 503, header: map[string]string{"Retry-After": "-5"}},
		pinReply{status: 429, header: map[string]string{"Retry-After": "  600 "}},
	)
	dead := "http://127.0.0.1:1/"

	var got []string
	call := func(url string) {
		before := *hits
		var out map[string]any
		err := getJSON(t.Context(), g, "logging", url, &out)
		line := fmt.Sprintf("sent=%v ", *hits > before)
		if err != nil {
			line += "err=" + err.Error()
		} else {
			line += "ok"
		}
		got = append(got, pinNormal(line))
	}
	feed := func() {
		for _, f := range g.Feeds(time.Now()) {
			if f.Name == "LOGGING" {
				got = append(got, fmt.Sprintf("feed state=%s used=%d budget=%d load=%d status=%v", f.State, f.Used, f.Budget, f.Load, g.Status()))
			}
		}
	}

	call(srv.URL)
	feed()
	call(srv.URL)
	call(dead)
	call(srv.URL)
	feed()
	call(srv.URL)
	call(srv.URL)
	feed()
	pinRewind(g, "logging", 30*time.Second)
	feed()
	call(srv.URL)
	call(srv.URL)
	pinRewind(g, "logging", time.Minute)
	call(srv.URL)
	feed()
	call(srv.URL)
	call(srv.URL)
	pinRewind(g, "logging", 2*time.Minute)
	call(srv.URL)
	call(srv.URL)
	for _, d := range []time.Duration{5 * time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute} {
		pinRewind(g, "logging", d)
		call(srv.URL)
		call(srv.URL)
	}
	pinRewind(g, "logging", 30*time.Minute)
	call(srv.URL)
	call(srv.URL)
	pinRewind(g, "logging", time.Hour)
	call(srv.URL)
	call(srv.URL)
	call(srv.URL)
	pinRewind(g, "logging", 30*time.Second)
	call(dead)
	call(dead)
	feed()
	call(dead)
	call(dead)
	feed()
	pinRewind(g, "logging", time.Minute)
	call(srv.URL)
	call(srv.URL)

	pinCompare(t, got, []string{
		"sent=true ok",
		"feed state=ok used=1 budget=900 load=0 status=[]",
		"sent=true err=logging: http 500",
		`sent=false err=Get "http://HOST/": dial tcp HOST: connect: connection refused`,
		"sent=true err=logging: http 404",
		"feed state=degraded used=4 budget=900 load=0 status=[]",
		"sent=true err=logging: invalid character 'o' in literal null (expecting 'u')",
		"sent=false err=logging: circuit open for another 30s",
		"feed state=open used=5 budget=900 load=0 status=[logging]",
		"feed state=degraded used=5 budget=900 load=0 status=[]",
		"sent=true err=logging: http 500",
		"sent=false err=logging: circuit open for another 1m0s",
		"sent=true ok",
		"feed state=ok used=7 budget=900 load=0 status=[]",
		"sent=true err=logging: http 429",
		"sent=false err=logging: circuit open for another 2m0s",
		"sent=true err=logging: http 429",
		"sent=false err=logging: circuit open for another 5m0s",
		"sent=true err=logging: http 503",
		"sent=false err=logging: circuit open for another 2m0s",
		"sent=true err=logging: http 503",
		"sent=false err=logging: circuit open for another 4m0s",
		"sent=true err=logging: http 503",
		"sent=false err=logging: circuit open for another 8m0s",
		"sent=true err=logging: http 503",
		"sent=false err=logging: circuit open for another 16m0s",
		"sent=true err=logging: http 503",
		"sent=false err=logging: circuit open for another 30m0s",
		"sent=true err=logging: http 503",
		"sent=false err=logging: circuit open for another 30m0s",
		"sent=true err=logging: http 502",
		"sent=false err=logging: circuit open for another 1h0m0s",
		"sent=true ok",
		"sent=true err=logging: http 503",
		"sent=false err=logging: circuit open for another 30s",
		`sent=false err=Get "http://HOST/": dial tcp HOST: connect: connection refused`,
		`sent=false err=Get "http://HOST/": dial tcp HOST: connect: connection refused`,
		"feed state=degraded used=4 budget=900 load=0 status=[]",
		`sent=false err=Get "http://HOST/": dial tcp HOST: connect: connection refused`,
		"sent=false err=logging: circuit open for another 1m0s",
		"feed state=open used=5 budget=900 load=0 status=[logging]",
		"sent=true err=logging: http 429",
		"sent=false err=logging: circuit open for another 10m0s",
	})

	wantLog := []string{
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=200 for=30s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=500 for=1m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=429 for=2m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=429 for=5m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=503 for=2m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=503 for=4m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=503 for=8m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=503 for=16m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=503 for=30m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=503 for=30m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=502 for=1h0m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=503 for=30s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=0 for=1m0s`,
		`level=WARN msg="upstream breaker opened" component=guard endpoint=logging status=429 for=10m0s`,
	}
	pinCompare(t, strings.Split(pinNormal(strings.TrimSpace(logs.String())), "\n"), wantLog)
}

// A failure that lands while the breaker is already open, from a call that was
// in flight when another opened it, sets a new time instead of keeping the
// longer one.
func TestGuardFailWhileOpenIsPinned(t *testing.T) {
	pinLog(t)
	g := NewGuard(t.TempDir())
	g.Fail("logging", 429, time.Hour)
	g.Fail("logging", 503, 0)
	err := g.Allow("logging")
	if got := pinNormal(fmt.Sprint(err)); got != "logging: circuit open for another 1m0s" {
		t.Errorf("got %q", got)
	}
}

// Pace and budget, through Allow, which refuses where Reserve would wait.
func TestGuardPaceAndBudgetSequenceIsPinned(t *testing.T) {
	g := NewGuard(t.TempDir())
	var got []string
	allow := func(name string) {
		got = append(got, pinNormal(fmt.Sprintf("%s %v", name, g.Allow(name))))
	}

	allow("yahoo")
	allow("yahoo")
	pinRewind(g, "yahoo", 1500*time.Millisecond)
	allow("yahoo")
	pinRewind(g, "yahoo", 500*time.Millisecond)
	allow("yahoo")
	for i := 0; i < 10; i++ {
		allow("bls")
		pinRewind(g, "bls", 2*time.Second)
	}
	allow("bls")
	pinRewind(g, "bls", time.Hour-30*time.Second)
	allow("bls")
	pinRewind(g, "bls", 10*time.Second)
	allow("bls")
	allow("bls")
	allow("hnitem")
	allow("hnitem")
	pinRewind(g, "hnitem", 250*time.Millisecond)
	allow("hnitem")
	allow("uptime")
	allow("uptime")
	allow("nobody")

	pinCompare(t, got, []string{
		"yahoo <nil>",
		"yahoo yahoo: paced, 2s early",
		"yahoo yahoo: paced, 500ms early",
		"yahoo <nil>",
		"bls <nil>",
		"bls <nil>",
		"bls <nil>",
		"bls <nil>",
		"bls <nil>",
		"bls <nil>",
		"bls <nil>",
		"bls <nil>",
		"bls <nil>",
		"bls <nil>",
		"bls bls: hourly budget of 10 spent",
		"bls bls: hourly budget of 10 spent",
		"bls <nil>",
		"bls bls: paced, 2s early",
		"hnitem <nil>",
		"hnitem hnitem: paced, 250ms early",
		"hnitem <nil>",
		"uptime <nil>",
		"uptime <nil>",
		"nobody nobody: no budget defined",
	})
}

// Every upstream's budget and pace, and the breaker's numbers, so changing any
// of them means changing this test too.
func TestGuardConfigurationIsPinned(t *testing.T) {
	want := map[string]string{
		"yahoo": "900/2s", "algolia": "120/2s", "lobsters": "120/2s", "hnitem": "300/250ms",
		"articles": "150/300ms", "openmeteo": "120/2s", "npr": "120/2s", "bbc": "60/2s",
		"googlenews": "30/2s", "dowjones": "30/2s", "thehill": "30/2s", "newsnation": "30/2s",
		"pbs": "30/2s", "cbs": "30/2s", "fox": "30/2s", "examiner": "30/2s", "cnbc": "30/2s",
		"bls": "10/2s", "bea": "10/2s", "nasdaq": "120/2s", "nws": "120/2s", "pollen": "60/2s",
		"steam": "240/1s", "justwatch": "60/2s", "youtube": "90/2s", "twitch": "90/2s",
		"twitchvods": "30/2s", "uptime": "900/0s", "logging": "900/0s",
	}
	if len(budgets) != len(want) {
		t.Errorf("%d budgets, want %d", len(budgets), len(want))
	}
	for name, w := range want {
		b, ok := budgets[name]
		if got := fmt.Sprintf("%d/%s", b.perHour, b.pace); !ok || got != w {
			t.Errorf("%s is %s, want %s", name, got, w)
		}
	}
	if failsToTrip != 4 || minBackoff != 30*time.Second || maxBackoff != 30*time.Minute {
		t.Errorf("breaker is %d, %s, %s", failsToTrip, minBackoff, maxBackoff)
	}
}

// The file on disk outlives a deploy, so the new code has to read what the
// old code wrote, and write it back the same shape.
func TestGuardFileFormatIsPinned(t *testing.T) {
	pinLog(t)
	dir := t.TempDir()
	now := time.Now().UTC()
	old := map[string]map[string]any{
		"yahoo": {
			"window_start": now.Add(-10 * time.Minute), "count": 7, "fails": 4,
			"last_call": now.Add(-time.Minute), "open_until": now.Add(3 * time.Minute),
			"backoff": int64(2 * time.Minute),
		},
		"bls": {
			"window_start": now.Add(-10 * time.Minute), "count": 10, "fails": 0,
			"last_call": now.Add(-time.Minute), "open_until": time.Time{}, "backoff": 0,
		},
	}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(dir, "guard.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	g := NewGuard(dir)
	var got []string
	got = append(got, pinNormal(fmt.Sprint(g.Allow("yahoo"))))
	got = append(got, pinNormal(fmt.Sprint(g.Allow("bls"))))
	pinRewind(g, "yahoo", 3*time.Minute)
	g.Fail("yahoo", 0, 0)
	got = append(got, pinNormal(fmt.Sprint(g.Allow("yahoo"))))
	g.Flush()

	pinCompare(t, got, []string{
		"yahoo: circuit open for another 3m0s",
		"bls: hourly budget of 10 spent",
		"yahoo: circuit open for another 4m0s",
	})

	raw, err := os.ReadFile(filepath.Join(dir, "guard.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	var fields []string
	for k, v := range back["yahoo"] {
		switch k {
		case "count", "fails", "backoff":
			fields = append(fields, fmt.Sprintf("%s=%v", k, v))
		default:
			fields = append(fields, k)
		}
	}
	sort.Strings(fields)
	if got := strings.Join(fields, " "); got != "backoff=2.4e+11 count=7 fails=5 last_call open_until window_start" {
		t.Errorf("yahoo was written back as %s", got)
	}
	if len(back) != 2 {
		t.Errorf("the file holds %d endpoints, want 2", len(back))
	}
}
