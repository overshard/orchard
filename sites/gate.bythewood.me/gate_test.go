package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gate.bythewood.me/web"
)

func testSite(t testing.TB) *site {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "gate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	seed(store)
	verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(web.SessionCookie); err == nil && c.Value == "good" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if c, err := r.Cookie(web.SessionCookie); err == nil && c.Value == "agent" {
			_, _ = w.Write([]byte(`{"ok":true,"method":"connector"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false}`))
	}))
	t.Cleanup(verify.Close)
	auth := web.NewAuthenticatorAt(verify.URL)
	s, err := newSite(store, OpenGeo(t.TempDir()), auth)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// ask is Caddy's forward_auth call for one request.
func ask(s *site, method, host, uri, ip string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "http://orchard-gate:8000/check", nil)
	r.Host = host
	r.Header.Set("X-Forwarded-Method", method)
	r.Header.Set("X-Forwarded-Uri", uri)
	r.Header.Set("X-Gate-Ip", ip)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	return w
}

const stranger = "198.51.100.7"

// The seeded rules carry over what Caddy did, including the clauses that look
// wrong and aren't.
func TestSeededRules(t *testing.T) {
	s := testSite(t)
	cases := []struct {
		host, method, uri string
		want              int
		hdr               []string
	}{
		{"blog.bythewood.me", "GET", "/", 204, nil},
		{"blog.bythewood.me", "GET", "/wp-login.php", 404, nil},
		{"blog.bythewood.me", "GET", "/blog/wp-json/wp/v2/users", 404, nil},
		{"blog.bythewood.me", "GET", "/app/.env", 404, nil},
		{"blog.bythewood.me", "GET", "/.git/config", 404, nil},
		{"blog.bythewood.me", "GET", "/media/images/cover.png", 204, nil},
		{"blog.bythewood.me", "GET", "/media/system/js/core.js", 404, nil},
		{"isaacbythewood.com", "GET", "/code", 204, nil},
		{"repos.bythewood.me", "GET", "/orchard.git/info/refs?service=git-upload-pack", 204, nil},
		{"repos.bythewood.me", "POST", "/orchard.git/git-receive-pack", 204, nil},
		{"repos.bythewood.me", "GET", "/orchard/blob/main/sites/analytics.bythewood.me/.env.example", 204, nil},
		{"repos.bythewood.me", "GET", "/dockerfiles", 204, nil},
		{"repos.bythewood.me", "GET", "/orchard/archive/main.tar.gz", 204, nil},
		{"repos.bythewood.me", "GET", "/.env", 404, nil},
		{"repos.bythewood.me", "GET", "/wp-admin/setup.php", 404, nil},
		{"mcp.bythewood.me", "GET", "/.well-known/oauth-protected-resource", 204, nil},
		{"mcp.bythewood.me", "GET", "/.well-known/security.txt", 404, nil},
		{"auth.bythewood.me", "GET", "/login", 204, []string{"Cf-Ipcountry", "US"}},
		{"auth.bythewood.me", "GET", "/login", 403, []string{"Cf-Ipcountry", "DE"}},
		{"auth.bythewood.me", "GET", "/sessions", 204, []string{"Cf-Ipcountry", "DE"}},
		{"auth.bythewood.me", "GET", "/login", 204, nil},
	}
	for _, c := range cases {
		w := ask(s, c.method, c.host, c.uri, stranger, c.hdr...)
		if w.Code != c.want {
			t.Errorf("%s %s%s = %d, want %d", c.method, c.host, c.uri, w.Code, c.want)
		}
	}
}

func TestOrchardIsNeverChecked(t *testing.T) {
	s := testSite(t)
	for _, ip := range []string{"172.18.0.5", "127.0.0.1", ""} {
		if w := ask(s, "GET", "blog.bythewood.me", "/wp-login.php", ip); w.Code != 204 {
			t.Errorf("%q got %d", ip, w.Code)
		}
	}
	_ = s.store.SetSetting("self", "203.0.113.10")
	_ = s.reload()
	if w := ask(s, "GET", "blog.bythewood.me", "/wp-login.php", "203.0.113.10"); w.Code != 204 {
		t.Errorf("the box's own address got %d", w.Code)
	}
}

func TestSignInRateLimit(t *testing.T) {
	s := testSite(t)
	for i := range 12 {
		w := ask(s, "POST", "auth.bythewood.me", "/login", stranger, "Cf-Ipcountry", "US")
		want := 204
		if i >= 10 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("post %d = %d, want %d", i+1, w.Code, want)
		}
	}
	if w := ask(s, "POST", "auth.bythewood.me", "/login", "198.51.100.8", "Cf-Ipcountry", "US"); w.Code != 204 {
		t.Errorf("another address shares the limit, got %d", w.Code)
	}
}

func TestWatchingCountsOnly(t *testing.T) {
	s := testSite(t)
	r := Rule{Name: "feed", When: "path prefix /feed", Action: actBlock, Status: 403, Watching: true, Pending: true, Author: "claude"}
	if err := s.store.AddRule(&r, "claude"); err != nil {
		t.Fatal(err)
	}
	_ = s.reload()
	if w := ask(s, "GET", "blog.bythewood.me", "/feed.xml", stranger); w.Code != 204 {
		t.Fatalf("a watching rule acted, %d", w.Code)
	}
	p := s.engine.stats.take()
	if c := p.hits[hitKey{r.ID, hourOf(time.Now())}]; c == nil || c.Watched != 1 || c.Hits != 0 {
		t.Fatalf("watch not counted: %+v", c)
	}
	r.Watching = false
	_ = s.store.UpdateRule(&r, "you")
	_ = s.reload()
	if w := ask(s, "GET", "blog.bythewood.me", "/feed.xml", stranger); w.Code != 403 {
		t.Fatalf("turned on, got %d", w.Code)
	}
}

func TestAllowWins(t *testing.T) {
	s := testSite(t)
	r := Rule{Name: "friend", When: "ip is 198.51.100.0/24", Action: actAllow}
	_ = s.store.AddRule(&r, "you")
	_ = s.reload()
	if w := ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger); w.Code != 204 {
		t.Fatalf("allow didn't win, %d", w.Code)
	}
}

func TestBanAfter(t *testing.T) {
	s := testSite(t)
	r, _ := s.store.RuleNamed("scanners")
	r.BanAfter, r.BanFor = 3, time.Hour
	_ = s.store.UpdateRule(&r, "you")
	_ = s.reload()
	for range 3 {
		ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger)
	}
	if w := ask(s, "GET", "pond.bythewood.me", "/", stranger); w.Code != 403 {
		t.Fatalf("banned address got %d on another host", w.Code)
	}
	if err := s.store.Flush(s.engine.stats.take()); err != nil {
		t.Fatal(err)
	}
	bans, _ := s.store.Bans()
	if len(bans) != 1 || bans[0].IP.String() != stranger {
		t.Fatalf("ban not kept: %+v", bans)
	}
}

func TestParseWhen(t *testing.T) {
	bad := []string{"", "colour is red", "path near /x", "path is", "ip is nope", "asn is cloud", "path regex ("}
	for _, w := range bad {
		if _, err := parseWhen(w); err == nil {
			t.Errorf("%q parsed", w)
		}
	}
	good := []string{"path is /x", "not host is a.b\nua contains Bot", "header:X-Thing prefix abc", "asn in AS14061 16509", "ip is 2001:db8::/32 203.0.113.4"}
	for _, w := range good {
		if _, err := parseWhen(w); err != nil {
			t.Errorf("%q: %v", w, err)
		}
	}
}

func TestUnknownCountryNeverTripsNot(t *testing.T) {
	conds, _ := parseWhen("not country is US")
	r := Rule{conds: conds}
	if r.match(&req{Header: func(string) string { return "" }}) {
		t.Fatal("an unknown country matched a not")
	}
}

func TestChallengeAndSolve(t *testing.T) {
	s := testSite(t)
	r := Rule{Name: "bots", When: "ua contains curl\ncrawler is none", Action: actChallenge}
	_ = s.store.AddRule(&r, "you")
	_ = s.reload()
	w := ask(s, "GET", "repos.bythewood.me", "/explore?page=3", stranger, "User-Agent", "curl/8")
	if w.Code != 403 || !strings.Contains(w.Body.String(), "/__gate/pow.js") {
		t.Fatalf("no challenge page, %d", w.Code)
	}
	body := w.Body.String()
	i := strings.Index(body, `name="seed" value="`) + len(`name="seed" value="`)
	seed := body[i : i+strings.Index(body[i:], `"`)]
	nonce := work(seed)
	form := url.Values{"seed": {seed}, "nonce": {nonce}, "back": {"/explore?page=3"}}
	sr := httptest.NewRequest(http.MethodPost, "http://repos.bythewood.me/__gate/solve", strings.NewReader(form.Encode()))
	sr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sr.Header.Set("User-Agent", "curl/8")
	sr.Header.Set("X-Gate-Ip", stranger)
	sw := httptest.NewRecorder()
	s.handler().ServeHTTP(sw, sr)
	if sw.Code != 303 || sw.Header().Get("Location") != "/explore?page=3" {
		t.Fatalf("solve = %d %s %s", sw.Code, sw.Header().Get("Location"), sw.Body.String())
	}
	cookie := sw.Result().Cookies()[0]
	w = ask(s, "GET", "repos.bythewood.me", "/explore?page=4", stranger, "User-Agent", "curl/8", "Cookie", cookie.Name+"="+cookie.Value)
	if w.Code != 204 {
		t.Fatalf("a solved pass was refused, %d", w.Code)
	}
	w = ask(s, "GET", "repos.bythewood.me", "/", "198.51.100.200", "User-Agent", "curl/8", "Cookie", cookie.Name+"="+cookie.Value)
	if w.Code != 204 {
		t.Fatalf("a pass didn't cover its own /24, %d", w.Code)
	}
	w = ask(s, "GET", "repos.bythewood.me", "/", "203.0.113.7", "User-Agent", "curl/8", "Cookie", cookie.Name+"="+cookie.Value)
	if w.Code != 403 {
		t.Fatalf("a pass worked from another network, %d", w.Code)
	}
	if cookie.Domain != "bythewood.me" {
		t.Fatalf("pass cookie domain %q", cookie.Domain)
	}
	w = ask(s, "GET", "blog.bythewood.me", "/", stranger, "User-Agent", "curl/8", "Cookie", cookie.Name+"="+cookie.Value)
	if w.Code != 204 {
		t.Fatalf("a pass from repos didn't cover blog, %d", w.Code)
	}
	w = ask(s, "GET", "isaacbythewood.com", "/", stranger, "User-Agent", "curl/8", "Cookie", cookie.Name+"="+cookie.Value)
	if w.Code != 403 {
		t.Fatalf("a pass for bythewood.me worked on isaacbythewood.com, %d", w.Code)
	}
	if s.engine.seedOK(seed, "isaacbythewood.com", time.Now()) {
		t.Fatal("a seed for bythewood.me checked out on isaacbythewood.com")
	}
}

// Every page renders against a database with something in it, since a
// template naming a field that doesn't exist only fails when it runs.
func TestPagesRender(t *testing.T) {
	s := testSite(t)
	r := Rule{Name: "feed-hammer", When: "ua contains python-requests\npath prefix /feed", Action: actBlock, Status: 403, Watching: true, Pending: true, Author: "claude", Note: "from a test"}
	_ = s.store.AddRule(&r, "claude")
	_ = s.reload()
	for _, uri := range []string{"/wp-login.php", "/feed", "/.env"} {
		ask(s, "GET", "blog.bythewood.me", uri, stranger, "User-Agent", "python-requests/2")
	}
	ip, _ := netip.ParseAddr("203.0.113.9")
	s.engine.addBan(Ban{IP: ip, Until: time.Now().Add(time.Hour), Rule: "by hand", Created: time.Now()})
	_ = s.store.AddBan(Ban{IP: ip, Until: time.Now().Add(time.Hour), Rule: "by hand", Created: time.Now()})
	if err := s.store.Flush(s.engine.stats.take()); err != nil {
		t.Fatal(err)
	}
	pages := []string{"/", "/rules", "/rules?range=7d", "/rules/new", "/rules/1", "/rules/" + strconv.FormatInt(r.ID, 10), "/traffic",
		"/traffic?asn=14061&host=blog.bythewood.me", "/traffic?ip=" + stranger, "/bans", "/settings", "/docs", "/nope"}
	for _, p := range pages {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		s.signedInHandler().ServeHTTP(w, req)
		if w.Code >= 500 || (w.Code != 200 && w.Code != 404) {
			t.Errorf("%s = %d", p, w.Code)
		}
		if strings.Contains(w.Body.String(), "ZgotmplZ") || strings.Contains(w.Body.String(), "<no value>") {
			t.Errorf("%s rendered a bad value", p)
		}
	}
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "checked first") {
		t.Errorf("landing = %d", w.Code)
	}
}

func TestProposeAPI(t *testing.T) {
	s := testSite(t)
	body := `{"name":"cloud-crawl","when":"asn is 14061\nhost is repos.bythewood.me","then":"challenge","note":"crawling explore"}`
	w := httptest.NewRecorder()
	s.signedInHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/propose", strings.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("propose = %d %s", w.Code, w.Body.String())
	}
	r, err := s.store.RuleNamed("cloud-crawl")
	if err != nil || !r.Watching || !r.Pending || r.Author != "claude" || r.Expires.IsZero() {
		t.Fatalf("proposal stored wrong: %+v %v", r, err)
	}
	w = httptest.NewRecorder()
	s.signedInHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/propose", strings.NewReader(`{"name":"x y","when":"path is /","then":"block"}`)))
	if w.Code != 400 {
		t.Fatalf("a bad name was taken, %d", w.Code)
	}
	for _, p := range []string{"/api/report", "/api/report?range=7d", "/api/traffic?asn=14061", "/api/traffic?rule=scanners"} {
		w = httptest.NewRecorder()
		s.signedInHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "# gate") {
			t.Errorf("%s = %d", p, w.Code)
		}
	}
}

func (s *site) signedInHandler() http.Handler {
	h := s.handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "good"})
		h.ServeHTTP(w, r)
	})
}

// A connector's session reads and proposes, and nothing else it posts lands.
func TestConnectorWritesOnlyThroughTheAPI(t *testing.T) {
	s := testSite(t)
	send := func(method, path, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "agent"})
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, r)
		return w.Code
	}
	if c := send(http.MethodGet, "/api/report", ""); c != 200 {
		t.Errorf("connector GET /api/report = %d", c)
	}
	if c := send(http.MethodPost, "/api/propose", `{"name":"by-agent","when":"asn is 14061","then":"challenge"}`); c != 200 {
		t.Errorf("connector propose = %d", c)
	}
	if c := send(http.MethodPost, "/settings", "mode=off"); c != http.StatusSeeOther {
		t.Errorf("connector POST /settings = %d, want a redirect to sign in", c)
	}
	if c := send(http.MethodPost, "/bans", "ip=203.0.113.9"); c != http.StatusSeeOther {
		t.Errorf("connector POST /bans = %d, want a redirect to sign in", c)
	}
}

func TestSignedInSkipsRules(t *testing.T) {
	s := testSite(t)
	if w := ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger, "Cookie", web.SessionCookie+"=good"); w.Code != 204 {
		t.Fatalf("signed in got %d", w.Code)
	}
	if w := ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger, "Cookie", web.SessionCookie+"=stale"); w.Code != 404 {
		t.Fatalf("a dead session got %d", w.Code)
	}
}

func TestCrawlers(t *testing.T) {
	c := NewCrawlers()
	c.ranges["google"] = []netip.Prefix{netip.MustParsePrefix("66.249.64.0/27")}
	ctx := context.Background()
	cases := []struct {
		ip, ua string
		asn    int
		want   string
	}{
		{"66.249.64.5", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", 15169, "google"},
		{"10.9.9.9", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", 396982, "fake"},
		{"31.13.24.5", "meta-externalagent/1.1", 32934, "meta"},
		{"203.0.113.4", "facebookexternalhit/1.1", 64500, "fake"},
		{"203.0.113.4", "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", 64500, "preview"},
		{"203.0.113.4", "Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0", 64500, "none"},
	}
	for _, x := range cases {
		if got := c.Classify(ctx, netip.MustParseAddr(x.ip), x.ua, x.asn); got != x.want {
			t.Errorf("%s %q = %s, want %s", x.ip, x.ua, got, x.want)
		}
	}
	if m := prefixField.FindAllStringSubmatch(`{"prefixes":[{"ipv4Prefix":"1.2.3.0/24"},{"ipv6Prefix":"2001:db8::/32"}]} ipv4Prefix\": \"3.81.245.78\"`, -1); len(m) != 3 || m[2][1] != "3.81.245.78" {
		t.Errorf("prefixes parsed as %v", m)
	}
}

func TestPassCoversTheSite(t *testing.T) {
	if siteOf("repos.bythewood.me") != "bythewood.me" || siteOf("www.isaacbythewood.com") != "isaacbythewood.com" || siteOf("bythewood.me") != "bythewood.me" {
		t.Fatal("siteOf")
	}
}

func work(seed string) string {
	for n := 0; ; n++ {
		sum := sha256.Sum256([]byte(seed + strconv.Itoa(n)))
		if strings.HasPrefix(hex.EncodeToString(sum[:]), strings.Repeat("0", powZeros)) {
			return strconv.Itoa(n)
		}
	}
}

func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }

func TestAPIEdit(t *testing.T) {
	s := testSite(t)
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.signedInHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/edit", strings.NewReader(body)))
		return w
	}
	if w := post(`{"name":"scanners","why":"repeat probers","ban_after":20}`); w.Code != 200 {
		t.Fatalf("edit = %d %s", w.Code, w.Body.String())
	}
	r, _ := s.store.RuleNamed("scanners")
	if r.BanAfter != 20 || r.BanFor != 24*time.Hour || r.Watching {
		t.Fatalf("edit stored wrong: %+v", r)
	}
	if c, _, _ := s.store.Changes(1); len(c) != 1 || c[0].Who != "claude" || !strings.Contains(c[0].What, "why: repeat probers") || c[0].Before == "" {
		t.Fatalf("change not logged: %+v", c)
	}
	for body, code := range map[string]int{
		`{"name":"scanners","ban_after":5}`:                           400,
		`{"name":"nope","why":"x","ban_after":5}`:                     404,
		`{"name":"claude-connector","why":"x","watching":true}`:       403,
		`{"name":"scanners","why":"x","then":"allow"}`:                403,
		`{"name":"sign-in-rate","why":"x","then":"block"}`:            403,
		`{"name":"scanners","why":"x","when":"bad line here at all"}`: 400,
	} {
		if w := post(body); w.Code != code {
			t.Errorf("%s = %d, want %d %s", body, w.Code, code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	s.signedInHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/propose", strings.NewReader(`{"name":"cloud-crawl","when":"asn is 14061","then":"challenge","note":"x"}`)))
	if w := post(`{"name":"cloud-crawl","why":"x","watching":false}`); w.Code != 403 {
		t.Errorf("a proposal was turned on, %d", w.Code)
	}
	if w := post(`{"name":"cloud-crawl","why":"x","when":"asn is 14061\nnot path prefix /feed"}`); w.Code != 200 {
		t.Errorf("a proposal's condition couldn't be narrowed, %d %s", w.Code, w.Body.String())
	}
	if w := post(`{"name":"cloud-crawl","why":"x","when":"asn is 14061 16509"}`); w.Code != 403 {
		t.Errorf("a proposal's condition was widened, %d %s", w.Code, w.Body.String())
	}
}

// An agent's edit goes live at once, so it can only make a rule refuse less.
func TestAPIEditOnlyNarrows(t *testing.T) {
	s := testSite(t)
	post := func(v map[string]any) *httptest.ResponseRecorder {
		v["why"] = "x"
		body, _ := json.Marshal(v)
		w := httptest.NewRecorder()
		s.signedInHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/edit", bytes.NewReader(body)))
		return w
	}
	scanners, _ := s.store.RuleNamed("scanners")
	for _, c := range []struct {
		why  string
		edit map[string]any
		code int
	}{
		{"swap the condition", map[string]any{"name": "scanners", "when": "host is isaacbythewood.com"}, 403},
		{"drop a line", map[string]any{"name": "dotfiles", "when": "path prefix /."}, 403},
		{"change a value", map[string]any{"name": "sign-in-us", "when": "host is auth.bythewood.me\npath is /login"}, 403},
		{"enforce", map[string]any{"name": "scanners", "watching": false}, 403},
		{"change the action", map[string]any{"name": "sign-in-rate", "then": "block"}, 403},
		{"lower a rate", map[string]any{"name": "sign-in-rate", "rate": 5}, 403},
		{"ban after one hit", map[string]any{"name": "scanners", "ban_after": 1}, 403},
		{"ban for a year", map[string]any{"name": "scanners", "ban_after": 5, "ban_hours": 8760}, 403},
		{"the whole attack", map[string]any{"name": "scanners", "when": "host is isaacbythewood.com", "watching": false, "ban_after": 1, "ban_hours": 8760}, 403},
		{"add a line", map[string]any{"name": "scanners", "when": scanners.When + "\nnot host is repos.bythewood.me"}, 200},
		{"raise a rate", map[string]any{"name": "sign-in-rate", "rate": 20}, 200},
		{"ban after three", map[string]any{"name": "scanners", "ban_after": 3, "ban_hours": 24}, 200},
		{"ban later", map[string]any{"name": "scanners", "ban_after": 50}, 200},
		{"stop banning", map[string]any{"name": "scanners", "ban_after": 0}, 200},
		{"watch", map[string]any{"name": "scanners", "watching": true}, 200},
	} {
		if w := post(c.edit); w.Code != c.code {
			t.Errorf("%s = %d, want %d %s", c.why, w.Code, c.code, w.Body.String())
		}
	}
	r, _ := s.store.RuleNamed("scanners")
	if !r.Watching || r.BanAfter != 0 || !strings.HasPrefix(r.When, scanners.When) {
		t.Fatalf("scanners ended up wrong: %+v", r)
	}

	// A ban Isaac set longer than a day stays his, and an agent can't ban
	// sooner under it.
	r.BanAfter, r.BanFor = 10, 8760*time.Hour
	if err := s.store.UpdateRuleWhy(&r, "isaac", "test"); err != nil {
		t.Fatal(err)
	}
	if w := post(map[string]any{"name": "scanners", "ban_after": 5}); w.Code != 403 {
		t.Errorf("banned sooner under a year long ban, %d", w.Code)
	}
	if w := post(map[string]any{"name": "scanners", "note": "still a year"}); w.Code != 200 {
		t.Errorf("a note edit was refused, %d %s", w.Code, w.Body.String())
	}
}
