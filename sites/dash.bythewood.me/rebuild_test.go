package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dash.bythewood.me/web"
)

func rebuildSite(t *testing.T, b *Briefer) http.Handler {
	s := &site{auth: verifier(t), briefer: b}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/rebuild", s.rebuildState)
	mux.HandleFunc("POST /api/rebuild", s.rebuildStart)
	return web.Chain(mux, web.Logged)
}

func rebuildReq(cookie string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/rebuild", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: cookie})
	}
	return r
}

func queued(b *Briefer) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.queued
}

// A signed out caller learns that and nothing else, not even whether a run is
// going, and the answer is never cached since the page it sits on can be.
func TestRebuildStateSaysWhetherSignedIn(t *testing.T) {
	h := rebuildSite(t, testBriefer(t, &fakeGateway{}, false))
	for cookie, want := range map[string]bool{"": false, "stale": false, "good": true} {
		r := httptest.NewRequest(http.MethodGet, "/api/rebuild", nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: cookie})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["signed_in"] != want {
			t.Errorf("cookie %q: signed_in = %v", cookie, got["signed_in"])
		}
		if _, ok := got["running"]; ok != want {
			t.Errorf("cookie %q: running present %v, body %s", cookie, ok, w.Body.String())
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("cookie %q: Cache-Control = %q", cookie, cc)
		}
	}
}

func TestRebuildRefusesWithoutASession(t *testing.T) {
	b := testBriefer(t, &fakeGateway{}, false)
	h := rebuildSite(t, b)
	for _, cookie := range []string{"", "stale"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, rebuildReq(cookie))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("cookie %q: status %d, want 401", cookie, w.Code)
		}
	}
	if queued(b) {
		t.Error("a signed out caller queued a run")
	}
}

func TestRebuildIsSameOriginOnly(t *testing.T) {
	for fetchSite, want := range map[string]int{
		"cross-site":  http.StatusForbidden,
		"same-site":   http.StatusForbidden, // another bythewood.me host carries the cookie too
		"none":        http.StatusForbidden,
		"same-origin": http.StatusAccepted,
		"":            http.StatusAccepted, // not a browser, and the session is still required
	} {
		b := testBriefer(t, &fakeGateway{}, false)
		r := rebuildReq("good")
		if fetchSite != "" {
			r.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		w := httptest.NewRecorder()
		rebuildSite(t, b).ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Sec-Fetch-Site %q: status %d, want %d", fetchSite, w.Code, want)
		}
		if queued(b) != (want == http.StatusAccepted) {
			t.Errorf("Sec-Fetch-Site %q: queued %v", fetchSite, queued(b))
		}
	}

	// A form posts text/plain across origins without a preflight.
	b := testBriefer(t, &fakeGateway{}, false)
	r := rebuildReq("good")
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	rebuildSite(t, b).ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType || queued(b) {
		t.Errorf("text/plain: status %d, queued %v", w.Code, queued(b))
	}
}

// One run at a time, and none again inside the cooldown after a forced one.
func TestRebuildRefusesWhileRunningAndCoolingDown(t *testing.T) {
	f := &fakeGateway{}
	b := testBriefer(t, f, false)
	h := rebuildSite(t, b)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, rebuildReq("good"))
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"running":true`) {
		t.Fatalf("first press: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, rebuildReq("good"))
	if w.Code != http.StatusConflict {
		t.Errorf("second press while queued: %d", w.Code)
	}

	<-b.force
	b.rebuild(context.Background(), time.Now())
	w = httptest.NewRecorder()
	h.ServeHTTP(w, rebuildReq("good"))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Errorf("press inside the cooldown: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	var got rebuildReply
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.RebuildStatus == nil {
		t.Fatalf("body %s: %v", w.Body.String(), err)
	}
	if got.Running || got.Finished == "" || got.ReadyIn < 590 || got.ReadyIn > 600 {
		t.Errorf("status after a run: %+v", *got.RebuildStatus)
	}
	if err := b.Force(time.Now().Add(rebuildCooldown + time.Second)); err != nil {
		t.Errorf("after the cooldown: %v", err)
	}
	if len(f.compiled) != 4 {
		t.Errorf("compiled %v, want one run of every desk", f.compiled)
	}

	off := NewBriefer(NewStore(NewHub()), nil, nil, t.TempDir())
	w = httptest.NewRecorder()
	rebuildSite(t, off).ServeHTTP(w, rebuildReq("good"))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("with no model key: %d", w.Code)
	}
}

// He's asking for it, so a busy card doesn't stop it, and the weights it put
// on still come off.
func TestForcedRunSkipsTheGateAndUnloads(t *testing.T) {
	f := &fakeGateway{gpu: GPUState{Busy: true, Reason: "card at 95%"}}
	b := testBriefer(t, f, false)
	now := inNY(t, "2026-10-10 14:20")
	b.rebuild(context.Background(), now)
	if strings.Join(f.compiled, ",") != "markets,news,feeds,glance" || f.unloads != 1 {
		t.Fatalf("compiled %v, unloaded %d", f.compiled, f.unloads)
	}
	if st := b.store.Snapshot().Briefs; st.News.Slot != now.Unix() || st.News.Title != "MIDDAY" {
		t.Errorf("news slot %d title %q", st.News.Slot, st.News.Title)
	}

	// Every desk now answers a moment after the 11am slot, so it isn't owed.
	b.tick(context.Background(), now.Add(time.Minute))
	if f.asked != 1 || len(f.compiled) != 4 {
		t.Errorf("a tick after a forced run asked %d and compiled %v", f.asked, f.compiled)
	}

	f2 := &fakeGateway{gpu: GPUState{Loaded: true}}
	b2 := testBriefer(t, f2, false)
	b2.rebuild(context.Background(), now)
	if f2.unloads != 0 {
		t.Error("a forced run pulled somebody's model off the card")
	}
}

// A slot that comes due while a forced run is queued leaves it to that run.
func TestTickGivesWayToAQueuedRun(t *testing.T) {
	f := &fakeGateway{}
	b := testBriefer(t, f, false)
	if err := b.Force(time.Now()); err != nil {
		t.Fatal(err)
	}
	b.tick(context.Background(), inNY(t, "2026-10-07 11:00"))
	if f.asked != 0 || len(f.compiled) != 0 || !queued(b) {
		t.Errorf("asked %d, compiled %v", f.asked, f.compiled)
	}
}

// Before 7am the latest slot is yesterday's close, and a run then is a morning.
func TestForcedSlot(t *testing.T) {
	for now, want := range map[string]string{
		"2026-10-07 05:30": "morning",
		"2026-10-07 09:45": "preopen",
		"2026-10-07 21:00": "close",
	} {
		if got := forcedSlot(inNY(t, now)).kind; got != want {
			t.Errorf("%s: %s, want %s", now, got, want)
		}
	}
}
