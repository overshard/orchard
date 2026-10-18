package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJudge(t *testing.T) {
	cases := []struct {
		r    gpuReading
		busy bool
	}{
		// Readings off this desktop with nothing loaded, then the thresholds.
		{gpuReading{UtilAvg: 14, UsedMiB: 1556, TotalMiB: 8192}, false},
		{gpuReading{UtilAvg: 29, UsedMiB: 1582, TotalMiB: 8192}, false},
		{gpuReading{UtilAvg: 34, UtilMax: 40, UsedMiB: 1619, TotalMiB: 8192}, false},
		{gpuReading{UtilAvg: 54, UsedMiB: 2999, TotalMiB: 8192}, false},
		{gpuReading{UtilAvg: 55, UsedMiB: 1600, TotalMiB: 8192}, true},
		{gpuReading{UtilAvg: 5, UsedMiB: 3000, TotalMiB: 8192}, true},
		{gpuReading{UtilAvg: 97, UsedMiB: 6800, TotalMiB: 8192}, true},
	}
	for _, c := range cases {
		if got, why := judge(c.r); got != c.busy {
			t.Errorf("%+v: busy=%t (%s), want %t", c.r, got, why, c.busy)
		}
	}
}

// gpuSite fakes llama-swap and the probe in one server. A completion or an
// unload is counted, and anything else would be a model load nobody asked for.
type fakeCard struct {
	running   string
	reading   gpuReading
	probed    int
	unloaded  int
	completed int
}

func gpuSite(t *testing.T, running string, reading gpuReading) (*site, string, *fakeCard) {
	t.Helper()
	f := &fakeCard{running: running, reading: reading}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/running":
			fmt.Fprint(w, f.running)
		case "/gpu":
			f.probed++
			_ = json.NewEncoder(w).Encode(f.reading)
		case "/unload":
			f.unloaded++
			f.running = `{"running":[]}`
		case "/v1/chat/completions":
			f.completed++
			f.running = `{"running":[{"model":"local","state":"ready"}]}`
			fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
		default:
			t.Errorf("asked for %q, which would load a model", r.URL.Path)
		}
	}))
	t.Cleanup(up.Close)
	store := testStore(t)
	secret, _, _ := store.NewKey("dash")
	return &site{store: store, upstream: up.URL, gpuProbe: up.URL, client: up.Client()}, secret, f
}

func askGPU(t *testing.T, s *site, secret string) gpuAnswer {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/gpu", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	s.requireKey(s.gpu)(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var a gpuAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestGPUBusyWhenTheDesktopIsGaming(t *testing.T) {
	s, secret, f := gpuSite(t, `{"running":[]}`, gpuReading{UtilAvg: 92, UtilMax: 99, UsedMiB: 6100, TotalMiB: 8192})
	a := askGPU(t, s, secret)
	if !a.Busy || a.Loaded || f.probed != 1 {
		t.Errorf("got %+v after %d probes", a, f.probed)
	}
}

func TestGPUIdle(t *testing.T) {
	s, secret, _ := gpuSite(t, `{"running":[]}`, gpuReading{UtilAvg: 14, UsedMiB: 1556, TotalMiB: 8192})
	if a := askGPU(t, s, secret); a.Busy {
		t.Errorf("idle desktop read busy: %+v", a)
	}
}

// With our own model resident most of the memory is ours, so it reads idle
// until a game fills the card beside it.
func TestGPUWithAModelResident(t *testing.T) {
	s, secret, f := gpuSite(t, `{"running":[{"model":"local","state":"ready"}]}`, gpuReading{UtilAvg: 3, UsedMiB: 7300, TotalMiB: 8192})
	if a := askGPU(t, s, secret); a.Busy || !a.Loaded {
		t.Errorf("model and an idle desktop read %+v", a)
	}
	f.reading = gpuReading{UtilAvg: 100, UsedMiB: 7814, TotalMiB: 8192}
	s.card.readAt = time.Time{}
	if a := askGPU(t, s, secret); !a.Busy || !a.Loaded {
		t.Errorf("model and a game read %+v", a)
	}
}

func TestJudgeLoaded(t *testing.T) {
	cases := []struct {
		r    gpuReading
		ours bool
		busy bool
	}{
		{gpuReading{UtilAvg: 3, UsedMiB: 7300, TotalMiB: 8192}, false, false},
		{gpuReading{UtilAvg: 96, UsedMiB: 7300, TotalMiB: 8192}, true, false},
		{gpuReading{UtilAvg: 96, UsedMiB: 7300, TotalMiB: 8192}, false, true},
		{gpuReading{UtilAvg: 100, UsedMiB: 7814, TotalMiB: 8192}, true, true},
	}
	for _, c := range cases {
		if got, why := judgeLoaded(c.r, c.ours); got != c.busy {
			t.Errorf("%+v ours=%t: busy=%t (%s), want %t", c.r, c.ours, got, why, c.busy)
		}
	}
}

func complete(t *testing.T, s *site, secret string, background bool) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"local","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	if background {
		req.Header.Set(backgroundHeader, "1")
	}
	s.requireKey(s.completions)(rec, req)
	return rec
}

// A game starting partway through a brief stops it at the next call, and the
// model the brief loaded comes off instead of sitting out its idle timer.
func TestABackgroundCallIsTurnedAwayAndItsModelUnloaded(t *testing.T) {
	s, secret, f := gpuSite(t, `{"running":[]}`, gpuReading{UtilAvg: 10, UsedMiB: 1600, TotalMiB: 8192})
	if rec := complete(t, s, secret, true); rec.Code != 200 {
		t.Fatalf("idle card: status %d", rec.Code)
	}
	f.reading = gpuReading{UtilAvg: 100, UsedMiB: 7814, TotalMiB: 8192}
	s.card.readAt = time.Time{}
	rec := complete(t, s, secret, true)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "3600" {
		t.Fatalf("game started: status %d, retry %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if f.completed != 1 || f.unloaded != 1 {
		t.Errorf("completed %d, unloaded %d", f.completed, f.unloaded)
	}
}

// Somebody chatting gets the model whatever the card is doing, and a model
// they loaded isn't pulled out from under them by a refused brief.
func TestChatIsNeverGated(t *testing.T) {
	s, secret, f := gpuSite(t, `{"running":[]}`, gpuReading{UtilAvg: 95, UsedMiB: 6100, TotalMiB: 8192})
	if rec := complete(t, s, secret, false); rec.Code != 200 {
		t.Fatalf("chat while gaming: status %d", rec.Code)
	}
	s.card.readAt = time.Time{}
	if rec := complete(t, s, secret, true); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("brief while gaming: status %d", rec.Code)
	}
	if f.unloaded != 0 || f.probed != 1 {
		t.Errorf("unloaded %d, probed %d", f.unloaded, f.probed)
	}
}

// A burst of background calls shares one reading.
func TestTheProbeIsCached(t *testing.T) {
	s, secret, f := gpuSite(t, `{"running":[]}`, gpuReading{UtilAvg: 10, UsedMiB: 1600, TotalMiB: 8192})
	for range 5 {
		complete(t, s, secret, true)
	}
	if f.probed != 1 || f.completed != 5 {
		t.Errorf("probed %d, completed %d", f.probed, f.completed)
	}
}

// After a reboot the containers are back before the game is, so background
// calls wait out the grace without a probe.
func TestStartupGrace(t *testing.T) {
	s, secret, f := gpuSite(t, `{"running":[]}`, gpuReading{UtilAvg: 10, UsedMiB: 1600, TotalMiB: 8192})
	s.card.started = time.Now().Add(-5 * time.Minute)
	a := askGPU(t, s, secret)
	if !a.Busy || f.probed != 0 {
		t.Fatalf("got %+v after %d probes", a, f.probed)
	}
	if left := time.Until(time.Unix(a.RetryAt, 0)); left < 9*time.Minute || left > 11*time.Minute {
		t.Errorf("retry in %s, want about ten minutes", left)
	}
	if rec := complete(t, s, secret, false); rec.Code != 200 {
		t.Errorf("chat inside the grace: status %d", rec.Code)
	}
}

func TestGPUNeedsAKey(t *testing.T) {
	s, _, _ := gpuSite(t, `{"running":[]}`, gpuReading{})
	rec := httptest.NewRecorder()
	s.requireKey(s.gpu)(rec, httptest.NewRequest("GET", "/v1/gpu", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d without a key", rec.Code)
	}
}
