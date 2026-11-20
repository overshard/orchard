package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A rule that would refuse signing in has to be caught however it's written,
// and the rules orchard runs today must not be.
func TestLockout(t *testing.T) {
	caught := []struct{ when, action string }{
		{"host is auth.bythewood.me", actBlock},
		{"path prefix /login", actBlock},
		{"country is US", actChallenge},
		{"ua contains mozilla", actBlock},
		{"method is POST\nhost is auth.bythewood.me", actBlock},
		{"not ip is 192.0.2.1", actBlock},
		{"not asn is 7922", actBlock},
		{"host is mcp.bythewood.me\npath prefix /authorize /token", actBlock},
		{"path is /connect", actChallenge},
		{"host is auth.bythewood.me", actLimit + ":2"},
	}
	for _, c := range caught {
		r := Rule{Name: "x", When: c.when, Action: c.action, Status: 403}
		if a, rate, ok := strings.Cut(c.action, ":"); ok {
			r.Action, r.Rate = a, atoi(rate)
		}
		if l := lockout(&r); len(l) == 0 {
			t.Errorf("%q %s doesn't warn", c.when, c.action)
		}
	}
	safe := []struct{ when, action string }{
		{"host is auth.bythewood.me", actAllow},
		{"host is auth.bythewood.me\npath is /login\nnot country is US", actBlock},
		{"host is auth.bythewood.me\nmethod is POST\npath is /login /code /recovery", actLimit + ":10"},
		{"crawler is fake", actBlock},
		{"asn is 48090 211590", actBlock},
		{"host is repos.bythewood.me", actChallenge},
		{"header:X-Thing is 1", actBlock},
	}
	for _, c := range safe {
		r := Rule{Name: "x", When: c.when, Action: c.action, Status: 403}
		if a, rate, ok := strings.Cut(c.action, ":"); ok {
			r.Action, r.Rate = a, atoi(rate)
		}
		if l := lockout(&r); len(l) != 0 {
			t.Errorf("%q %s warns of %v", c.when, c.action, l)
		}
	}
	for _, r := range seedRules() {
		if l := lockout(&r); len(l) != 0 {
			t.Errorf("seeded %s warns of %v", r.Name, l)
		}
	}
	// A browser seen signed in counts as you, so a rule on its own network trips.
	r := Rule{Name: "x", When: "asn is 7922", Action: actBlock}
	me := identity{IP: mustAddr("73.1.2.3"), ASN: 7922, Country: "US", UA: "Mozilla/5.0 Firefox/140.0"}
	if len(lockout(&r)) != 0 || len(lockout(&r, me)) == 0 {
		t.Error("a rule on the signed in browser's network isn't caught")
	}
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// Turning on a rule that catches signing in takes a second tick, on every way
// a rule can be turned on.
func TestLockoutNeedsConfirm(t *testing.T) {
	s := testSite(t)
	h := s.signedInHandler()
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(w, r)
		return w
	}
	form := url.Values{"name": {"lock"}, "when": {"host is auth.bythewood.me"}, "action": {"block"}, "status": {"403"}}
	if w := post("/rules", form); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "signs you out") {
		t.Fatalf("live lockout saved without asking, %d", w.Code)
	}
	if _, err := s.store.RuleNamed("lock"); err == nil {
		t.Fatal("the rule was stored")
	}
	form.Set("watching", "on")
	if w := post("/rules", form); w.Code != http.StatusSeeOther {
		t.Fatalf("watching save = %d", w.Code)
	}
	r, _ := s.store.RuleNamed("lock")
	id := strconv.FormatInt(r.ID, 10)
	if w := post("/rules/"+id+"/on", nil); w.Header().Get("Location") != "/rules/"+id+"?confirm=1" {
		t.Fatalf("turn on didn't ask, went to %s", w.Header().Get("Location"))
	}
	if r, _ = s.store.RuleNamed("lock"); !r.Watching {
		t.Fatal("turned on without the tick")
	}
	if w := ask(s, "GET", "auth.bythewood.me", "/login", stranger, "Cf-Ipcountry", "US"); w.Code != 204 {
		t.Fatalf("sign in refused while only watching, %d", w.Code)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rules/"+id+"?confirm=1", nil))
	if !strings.Contains(w.Body.String(), `name="lockout_ok"`) {
		t.Fatal("the confirm page has no second tick")
	}
	post("/rules/"+id+"/on", url.Values{"lockout_ok": {"1"}})
	if r, _ = s.store.RuleNamed("lock"); r.Watching {
		t.Fatal("the confirmed turn on didn't take")
	}
}

func TestModeEnds(t *testing.T) {
	s := testSite(t)
	h := s.signedInHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader("mode=off&for=1h"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(w, r)
	if s.engine.Mode() != modeOff {
		t.Fatalf("mode %s after saving off", s.engine.Mode())
	}
	if w := ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger); w.Code != 204 {
		t.Fatalf("off still refused, %d", w.Code)
	}
	until := s.engine.until.Load()
	if d := time.Until(time.Unix(until, 0)); d < 59*time.Minute || d > 61*time.Minute {
		t.Fatalf("an hour ends in %v", d)
	}
	// The engine reads the clock itself, so the moment it passes it enforces.
	s.engine.until.Store(time.Now().Add(-time.Second).Unix())
	if s.engine.Mode() != modeOn {
		t.Fatal("an ended mode still runs")
	}
	if w := ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger); w.Code != 404 {
		t.Fatalf("ended off still lets scanners in, %d", w.Code)
	}
	_ = s.store.SetSetting("mode_until", strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10))
	_ = s.reload()
	if s.store.Setting("mode") != modeOn || s.engine.until.Load() != 0 {
		t.Fatalf("reload didn't put enforcing back, %s", s.store.Setting("mode"))
	}
	if c, _, _ := s.store.Changes(1); len(c) == 0 || !strings.Contains(c[0].What, "ran out") {
		t.Fatal("running out isn't in the change log")
	}
	// A watch with no end gets one rather than running forever.
	_ = s.store.SetSetting("mode", modeWatch)
	_ = s.store.SetSetting("mode_until", "0")
	_ = s.reload()
	if s.engine.Mode() != modeWatch || s.engine.until.Load() == 0 {
		t.Fatal("an endless watch kept no end")
	}
}

func TestRingAndTester(t *testing.T) {
	s := testSite(t)
	for i := range 5 {
		ask(s, "GET", "blog.bythewood.me", "/feed/"+strconv.Itoa(i), stranger, "User-Agent", "python-requests/2")
	}
	ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger, "User-Agent", "python-requests/2")
	ask(s, "GET", "auth.bythewood.me", "/login", "198.51.100.9", "Cookie", "bw_session=good", "Cf-Ipcountry", "US")
	ask(s, "GET", "blog.bythewood.me", "/feed/x", "172.18.0.4")
	hour := s.engine.stats.hour(time.Now())
	if len(hour) != 7 {
		t.Fatalf("ring holds %d, want 7 with orchard left out", len(hour))
	}
	r := Rule{Name: "feeds", When: "path prefix /feed", Action: actBlock}
	res := testRule(&r, hour, *s.engine.rules.Load(), time.Now())
	if res.Matched != 5 || res.Addresses != 1 || res.Hosts[0].Key != "blog.bythewood.me" || len(res.Lockout) != 0 {
		t.Fatalf("test = %+v", res)
	}
	r = Rule{Name: "wp", When: "path contains wp-", Action: actBlock}
	if res = testRule(&r, hour, *s.engine.rules.Load(), time.Now()); res.AlreadyN != 1 || res.Already[0].Key != "scanners" {
		t.Fatalf("overlap = %+v", res)
	}
	r = Rule{Name: "auth", When: "host is auth.bythewood.me", Action: actBlock}
	if res = testRule(&r, hour, *s.engine.rules.Load(), time.Now(), signedIn(hour)...); res.Session != 1 || len(res.Lockout) == 0 {
		t.Fatalf("signed in not warned: %+v", res)
	}
	r = Rule{Name: "bad", When: "path regex (", Action: actBlock}
	if res = testRule(&r, hour, nil, time.Now()); res.Error == "" {
		t.Fatal("a broken regex tested")
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/rules/test", strings.NewReader("name=x&when=path+prefix+%2Ffeed&action=block"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.signedInHandler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"matched":5`) {
		t.Fatalf("test endpoint = %d %s", w.Code, w.Body.String())
	}
}

func TestRingWraps(t *testing.T) {
	g := newRing(3)
	now := time.Now()
	for i := range 5 {
		g.add(now, &req{Path: strconv.Itoa(i)}, verdict{kind: vPass})
	}
	got := g.since(0)
	if len(got) != 3 || got[0].Path != "2" || got[2].Path != "4" {
		t.Fatalf("ring = %+v", got)
	}
}

func TestShadowed(t *testing.T) {
	s := testSite(t)
	r := Rule{Name: "wp-too", When: "path contains wp-login", Action: actBlock, Status: 403}
	_ = s.store.AddRule(&r, "you")
	_ = s.reload()
	ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger)
	sh := shadowed(*s.engine.rules.Load(), s.engine.stats.hour(time.Now()), time.Now())
	if sh[r.ID].By != "scanners" {
		t.Fatalf("shadows = %+v", sh)
	}
	scan, _ := s.store.RuleNamed("scanners")
	if _, ok := sh[scan.ID]; ok {
		t.Fatal("the rule doing the work is marked shadowed")
	}
}

func TestEventWhy(t *testing.T) {
	s := testSite(t)
	ask(s, "GET", "auth.bythewood.me", "/login", stranger, "Cf-Ipcountry", "DE")
	p := s.engine.stats.take()
	if len(p.events) != 1 {
		t.Fatalf("%d events", len(p.events))
	}
	e := p.events[0]
	if e.Rule != "sign-in-us" || e.Line != 2 || e.Matched != "/login" {
		t.Fatalf("why = %+v", e)
	}
	if err := s.store.Flush(p); err != nil {
		t.Fatal(err)
	}
	rows := s.eventRows(s.store.Events(TrafficFilter{Window: time.Hour}, "", 5, time.Now()), time.Now())
	if len(rows) != 1 || rows[0].Summary() != "line 2 · /login" || len(rows[0].Lines) != 3 || rows[0].Ticks[23] != "bad" {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestSolveCounts(t *testing.T) {
	s := testSite(t)
	r := Rule{Name: "bots", When: "ua contains curl", Action: actChallenge}
	_ = s.store.AddRule(&r, "you")
	_ = s.reload()
	var body string
	for range 3 {
		body = ask(s, "GET", "repos.bythewood.me", "/explore", stranger, "User-Agent", "curl/8").Body.String()
	}
	i := strings.Index(body, `name="seed" value="`) + len(`name="seed" value="`)
	seed := body[i : i+strings.Index(body[i:], `"`)]
	if seedRule(seed) != r.ID {
		t.Fatalf("seed carries rule %d, want %d", seedRule(seed), r.ID)
	}
	solve := func(nonce string) {
		form := url.Values{"seed": {seed}, "nonce": {nonce}}
		sr := httptest.NewRequest(http.MethodPost, "http://repos.bythewood.me/__gate/solve", strings.NewReader(form.Encode()))
		sr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		s.handler().ServeHTTP(httptest.NewRecorder(), sr)
	}
	solve("nope")
	solve(work(seed))
	if err := s.store.Flush(s.engine.stats.take()); err != nil {
		t.Fatal(err)
	}
	sv := s.store.Solves(24*time.Hour, time.Now())
	if sv.People != 1 || sv.Solved != 1 || sv.Failed != 1 || sv.ByRule[r.ID] != [3]int{1, 1, 1} {
		t.Fatalf("solves = %+v", sv)
	}
}

func TestRestore(t *testing.T) {
	s := testSite(t)
	r, _ := s.store.RuleNamed("dotfiles")
	before := r.When
	r.When = "path prefix /.git"
	_ = s.store.UpdateRule(&r, "you")
	changes, _, _ := s.store.Changes(1)
	d := changes[0].Diff()
	if len(d) != 3 || d[0].Op != "-" || d[2] != (DiffLine{"+", "path prefix /.git"}) {
		t.Fatalf("diff = %+v", d)
	}
	w := httptest.NewRecorder()
	s.signedInHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/changes/"+strconv.FormatInt(changes[0].ID, 10)+"/restore", nil))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("restore = %d", w.Code)
	}
	if r, _ = s.store.RuleNamed("dotfiles"); r.When != before {
		t.Fatalf("restored to %q", r.When)
	}
	// A version that catches signing in comes back watching.
	lock := Rule{Name: "lock", When: "host is auth.bythewood.me", Action: actBlock, Status: 403}
	_ = s.store.AddRule(&lock, "you")
	lock.When = "host is blog.bythewood.me"
	_ = s.store.UpdateRule(&lock, "you")
	changes, _, _ = s.store.Changes(1)
	s.signedInHandler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/changes/"+strconv.FormatInt(changes[0].ID, 10)+"/restore", nil))
	if lock, _ = s.store.RuleNamed("lock"); !lock.Watching || lock.When != "host is auth.bythewood.me" {
		t.Fatalf("lockout restored live: %+v", lock)
	}
}
