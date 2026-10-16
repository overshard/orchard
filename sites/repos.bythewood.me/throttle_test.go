package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"repos.bythewood.me/web"
)

// Three archives an hour is enough to take a copy of what somebody came for and
// not enough to mirror the estate.
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
// still bound a crawler going flat out.
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
	verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer verify.Close()
	s := &site{auth: web.NewAuthenticatorAt(verify.URL)}
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

// A cookie that auth says is dead still counts against the limit.
func TestAMadeUpSessionStillCounts(t *testing.T) {
	verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false}`))
	}))
	defer verify.Close()
	s := &site{auth: web.NewAuthenticatorAt(verify.URL)}
	th := newThrottle(archiveLimit)
	h := s.limited(th, func(w http.ResponseWriter, r *http.Request) {})

	var last *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest(http.MethodGet, "/orchard/archive/main.tar.gz", nil)
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "x"})
		last = httptest.NewRecorder()
		h(last, r)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("a made up session answered %d on the fifth archive", last.Code)
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

func TestRefuseMetaLeavesEveryoneElse(t *testing.T) {
	h := refuseMeta(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for ip, want := range map[string]int{
		"2a03:2880:f806:1d::": http.StatusForbidden,
		"2a03:2881::1":        http.StatusOK,
		"71.71.122.88":        http.StatusOK,
		"":                    http.StatusOK,
	} {
		r := httptest.NewRequest("GET", "/orchard/log", nil)
		r.Header.Set("CF-Connecting-IP", ip)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%q = %d, want %d", ip, w.Code, want)
		}
		if want == http.StatusForbidden && w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("a refusal the edge could cache: %q", w.Header().Get("Cache-Control"))
		}
	}
}

func TestRefuseCrawlers(t *testing.T) {
	const (
		chrome    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36"
		firefox   = "Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0"
		googlebot = "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.7390.122 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
		oaiSearch = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36; compatible; OAI-SearchBot/1.3; +https://openai.com/searchbot"
		claudeBot = "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)"
		status    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/102.0.5005.115 Safari/537.36 Status/2.0.0"
	)
	h := refuseCrawlers(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, c := range []struct {
		path, ua, mode string
		want           int
	}{
		{"/orchard/blob/abc123/go.mod", chrome, "", http.StatusForbidden},
		{"/orchard/log/main/sites/status.bythewood.me/go.sum", chrome, "", http.StatusForbidden},
		{"/orchard/tree/main", firefox, "", http.StatusForbidden},
		{"/orchard/blob/abc123/go.mod", chrome, "navigate", http.StatusOK},
		{"/robots.txt", googlebot, "", http.StatusOK},
		{"/robots.txt", oaiSearch, "", http.StatusOK},
		{"/robots.txt", claudeBot, "", http.StatusOK},
		{"/orchard/blob/abc123/go.mod", googlebot, "", http.StatusForbidden},
		{"/orchard/blob/abc123/go.mod", oaiSearch, "", http.StatusForbidden},
		{"/", claudeBot, "", http.StatusForbidden},
		{"/orchard/atom.xml", "Feedly/1.0 (+http://www.feedly.com/fetcher.html)", "", http.StatusOK},
		{"/orchard/blob/abc123/go.mod", "git/2.47.3", "", http.StatusOK},
		{"/orchard/blob/abc123/go.mod", "curl/8.14.1", "", http.StatusOK},
		{"/", status, "", http.StatusOK},
		{"/orchard", chrome, "", http.StatusOK},
		{"/orchard/atom.xml", chrome, "", http.StatusOK},
		{"/orchard.git/info/refs", chrome, "", http.StatusOK},
	} {
		r := httptest.NewRequest("GET", c.path, nil)
		r.Header.Set("User-Agent", c.ua)
		if c.mode != "" {
			r.Header.Set("Sec-Fetch-Mode", c.mode)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s as %.40q = %d, want %d", c.path, c.ua, w.Code, c.want)
		}
		if c.want == http.StatusForbidden && w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("a refusal the edge could cache: %q", w.Header().Get("Cache-Control"))
		}
	}
}
