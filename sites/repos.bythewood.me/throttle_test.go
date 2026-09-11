package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"repos.bythewood.me/web"
)

// Three archives an hour is enough to take a copy of what somebody came for and
// not enough to mirror the estate. The crawler took 36 in two and a half hours.
func TestArchiveLimitStopsAMirror(t *testing.T) {
	th := newThrottle(archiveLimit)
	now := time.Now()
	for i := 0; i < 3; i++ {
		if ok, _ := th.allow("1.2.3.4", now); !ok {
			t.Fatalf("archive %d was refused inside the burst", i+1)
		}
	}
	ok, wait := th.allow("1.2.3.4", now)
	if ok {
		t.Fatal("the fourth archive in a row was allowed")
	}
	if wait <= 0 || wait > time.Hour {
		t.Errorf("Retry-After would be %v", wait)
	}

	// An hour later the allowance is back, and not more than the burst.
	for i := 0; i < 3; i++ {
		if ok, _ := th.allow("1.2.3.4", now.Add(time.Hour)); !ok {
			t.Fatalf("archive %d after an hour was refused", i+1)
		}
	}
	if ok, _ := th.allow("1.2.3.4", now.Add(time.Hour)); ok {
		t.Error("the bucket refilled past its burst")
	}
}

// One address running out must not touch anybody else's allowance.
func TestThrottleIsPerAddress(t *testing.T) {
	th := newThrottle(archiveLimit)
	now := time.Now()
	for i := 0; i < 4; i++ {
		th.allow("185.213.175.37", now)
	}
	if ok, _ := th.allow("71.71.122.88", now); !ok {
		t.Error("one crawler spent somebody else's allowance")
	}
}

// The page bucket has to be generous to a person clicking through history and
// still bound a crawler that averaged 29 requests a minute.
func TestPageLimitLeavesAReaderAlone(t *testing.T) {
	th := newThrottle(pageLimit)
	now := time.Now()
	for i := 0; i < 20; i++ {
		if ok, _ := th.allow("1.2.3.4", now.Add(time.Duration(i)*3*time.Second)); !ok {
			t.Fatalf("a reader was refused on click %d", i+1)
		}
	}

	// Flat out from a standing start, the burst is the whole of it.
	fast := newThrottle(pageLimit)
	start := time.Now()
	allowed := 0
	for i := 0; i < 200; i++ {
		if ok, _ := fast.allow("9.9.9.9", start); ok {
			allowed++
		}
	}
	if allowed != int(pageLimit.burst) {
		t.Errorf("a flat out crawler got %d requests, want the burst of %v", allowed, pageLimit.burst)
	}
}

// A signed in request is Isaac using his own site and never counts.
func TestSignedInSkipsTheLimit(t *testing.T) {
	s := &site{}
	th := newThrottle(archiveLimit)
	hit := 0
	h := s.limited(th, func(w http.ResponseWriter, r *http.Request) { hit++ })

	for i := 0; i < 10; i++ {
		r := httptest.NewRequest(http.MethodGet, "/orchard/archive/main.tar.gz", nil)
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "a-live-session"})
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("a signed in archive answered %d", w.Code)
		}
	}
	if hit != 10 {
		t.Errorf("the handler ran %d times, want 10", hit)
	}
}

// A refusal says how long to wait and must never be cached, or the edge hands
// the same 429 to everybody else asking for that path.
func TestRefusalIsUncacheableAndSaysWhen(t *testing.T) {
	s := &site{}
	th := newThrottle(archiveLimit)
	h := s.limited(th, func(w http.ResponseWriter, r *http.Request) {})

	var last *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest(http.MethodGet, "/orchard/archive/main.tar.gz", nil)
		r.Header.Set("CF-Connecting-IP", "185.213.175.37")
		last = httptest.NewRecorder()
		h(last, r)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("the fifth archive answered %d", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After on the refusal")
	}
	if last.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", last.Header().Get("Cache-Control"))
	}
}
