package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJudge(t *testing.T) {
	cases := []struct {
		r    gpuReading
		busy bool
	}{
		// Readings taken on this desktop with nothing loaded.
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

func gpuSite(t *testing.T, running string, reading gpuReading) (*site, string, *int) {
	t.Helper()
	probed := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/running":
			fmt.Fprint(w, running)
		case "/gpu":
			probed++
			_ = json.NewEncoder(w).Encode(reading)
		default:
			t.Errorf("asked for %q, which would load a model", r.URL.Path)
		}
	}))
	t.Cleanup(up.Close)
	store := testStore(t)
	secret, _, _ := store.NewKey("dash")
	return &site{store: store, upstream: up.URL, gpuProbe: up.URL, client: up.Client()}, secret, &probed
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
	s, secret, probed := gpuSite(t, `{"running":[]}`, gpuReading{UtilAvg: 92, UtilMax: 99, UsedMiB: 6100, TotalMiB: 8192})
	a := askGPU(t, s, secret)
	if !a.Busy || a.Loaded || *probed != 1 {
		t.Errorf("got %+v after %d probes", a, *probed)
	}
}

func TestGPUIdle(t *testing.T) {
	s, secret, _ := gpuSite(t, `{"running":[]}`, gpuReading{UtilAvg: 14, UsedMiB: 1556, TotalMiB: 8192})
	if a := askGPU(t, s, secret); a.Busy {
		t.Errorf("idle desktop read busy: %+v", a)
	}
}

// With our own model resident the numbers are ours, so the probe is skipped and
// the caller is told it can go ahead and use what is already loaded.
func TestGPUWithAModelResidentSkipsTheProbe(t *testing.T) {
	s, secret, probed := gpuSite(t, `{"running":[{"model":"local","state":"ready"}]}`, gpuReading{UtilAvg: 99, UsedMiB: 7300})
	a := askGPU(t, s, secret)
	if a.Busy || !a.Loaded || *probed != 0 {
		t.Errorf("got %+v after %d probes", a, *probed)
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
