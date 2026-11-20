package main

import (
	"crypto/sha256"
	"encoding/hex"
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

func testSite(t *testing.T) *site {
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
	r := Rule{Name: "bots", When: "ua contains curl", Action: actChallenge}
	_ = s.store.AddRule(&r, "you")
	_ = s.reload()
	w := ask(s, "GET", "repos.bythewood.me", "/explore?page=3", stranger, "User-Agent", "curl/8")
	if w.Code != 403 || !strings.Contains(w.Body.String(), "/__gate/pow.js") {
		t.Fatalf("no challenge page, %d", w.Code)
	}
	body := w.Body.String()
	i := strings.Index(body, `name="seed" value="`) + len(`name="seed" value="`)
	seed := body[i : i+strings.Index(body[i:], `"`)]
	nonce := ""
	for n := 0; ; n++ {
		sum := sha256.Sum256([]byte(seed + strconv.Itoa(n)))
		if strings.HasPrefix(hex.EncodeToString(sum[:]), strings.Repeat("0", powZeros)) {
			nonce = strconv.Itoa(n)
			break
		}
	}
	form := url.Values{"seed": {seed}, "nonce": {nonce}, "back": {"/explore?page=3"}}
	sr := httptest.NewRequest(http.MethodPost, "http://repos.bythewood.me/__gate/solve", strings.NewReader(form.Encode()))
	sr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sr.Header.Set("User-Agent", "curl/8")
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
	w = ask(s, "GET", "blog.bythewood.me", "/", stranger, "User-Agent", "curl/8", "Cookie", cookie.Name+"="+cookie.Value)
	if w.Code != 403 {
		t.Fatalf("a pass for repos worked on blog, %d", w.Code)
	}
	if s.engine.seedOK(seed, "blog.bythewood.me", time.Now()) {
		t.Fatal("a seed for repos checked out on blog")
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

func TestSignedInSkipsRules(t *testing.T) {
	s := testSite(t)
	if w := ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger, "Cookie", web.SessionCookie+"=good"); w.Code != 204 {
		t.Fatalf("signed in got %d", w.Code)
	}
	if w := ask(s, "GET", "blog.bythewood.me", "/wp-login.php", stranger, "Cookie", web.SessionCookie+"=stale"); w.Code != 404 {
		t.Fatalf("a dead session got %d", w.Code)
	}
}
