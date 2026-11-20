package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"dns.bythewood.me/web"
)

// fakeUpstream stands in for unbound. It answers every A with 192.0.2.1, and
// cname.example.com with a CNAME to t.tracker.example first.
func fakeUpstream(t testing.TB) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		q := r.Question[0]
		name := q.Name
		if name == "cname.example.com." {
			m.Answer = append(m.Answer, &dns.CNAME{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60}, Target: "t.tracker.example."})
			name = "t.tracker.example."
		}
		if q.Qtype == dns.TypeA {
			m.Answer = append(m.Answer, &dns.A{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP("192.0.2.1")})
		}
		_ = w.WriteMsg(m)
	})}
	go func() { _ = srv.ActivateAndServe() }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().String()
}

func testSite(t testing.TB) (*site, *Device) {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "dns.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(web.SessionCookie); err == nil && c.Value == "good" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false}`))
	}))
	t.Cleanup(verify.Close)
	t.Setenv("DNS_UPSTREAM", fakeUpstream(t))
	s, err := newSite(store, dir, filepath.Join(dir, "tls"), web.NewAuthenticatorAt(verify.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "lists"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "lists", "pro.txt"), []byte("# Version: 1\ntracker.example\nads.example.net\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "lists", "tif.txt"), []byte("evil.example\nads.example.net\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "lists", "doh.txt"), []byte("bythewood.me\n"), 0o644)
	s.lists.load()
	d, err := store.AddDevice("phone", "phone")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.reload()
	go s.log.run(s.noteSeen)
	return s, d
}

func query(name string, qt uint16) *dns.Msg {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qt)
	m.SetEdns0(1232, false)
	return m
}

func TestTableMatchesParents(t *testing.T) {
	tab := build([][]string{{"tracker.example", "b.example"}, {"b.example", "c.example"}})
	cases := map[string]uint8{"tracker.example": 1, "x.y.tracker.example": 1, "b.example": 3, "a.b.example": 3, "c.example": 2, "example": 0, "nottracker.example": 0}
	for name, want := range cases {
		if _, got := tab.match(name); got != want {
			t.Errorf("%s: mask %b, want %b", name, got, want)
		}
	}
}

func TestDecideOrder(t *testing.T) {
	s, d := testSite(t)
	if v := s.res.decide(d, "a.tracker.example"); v.Status != "block" || v.Reason != "pro" || v.Domain != "tracker.example" {
		t.Errorf("listed name: %+v", v)
	}
	if v := s.res.decide(d, "ads.example.net"); v.Reason != "pro" {
		t.Errorf("credited to the first list: %+v", v)
	}
	if v := s.res.decide(d, "dns.bythewood.me"); v.Status != "pass" {
		t.Errorf("my own domain must never block: %+v", v)
	}
	_ = s.store.PutRule("tracker.example", "allow", "")
	_ = s.store.PutRule("fine.example", "block", "")
	_ = s.reload()
	if v := s.res.decide(d, "a.tracker.example"); v.Status != "allow" || v.Reason != "pro" {
		t.Errorf("my allow beats a list: %+v", v)
	}
	if v := s.res.decide(d, "x.fine.example"); v.Status != "deny" {
		t.Errorf("my block covers subdomains: %+v", v)
	}
	_ = s.store.UpdateDevice(d.ID, d.Name, d.Kind, []string{"pro"}, false)
	_ = s.reload()
	d2 := s.res.device(d.Token)
	if v := s.res.decide(d2, "ads.example.net"); v.Status != "block" || v.Reason != "tif" {
		t.Errorf("a device skipping pro still gets tif: %+v", v)
	}
}

func TestAnswerBlocksWithEDE(t *testing.T) {
	s, d := testSite(t)
	m := s.res.Answer(d, query("ads.example.net", dns.TypeA), "doh", "203.0.113.9")
	if len(m.Answer) != 1 || m.Answer[0].(*dns.A).A.String() != "0.0.0.0" {
		t.Fatalf("blocked A should be 0.0.0.0: %v", m.Answer)
	}
	var ede *dns.EDNS0_EDE
	for _, o := range m.IsEdns0().Option {
		if e, ok := o.(*dns.EDNS0_EDE); ok {
			ede = e
		}
	}
	if ede == nil || ede.InfoCode != dns.ExtendedErrorCodeBlocked || ede.ExtraText != "pro" {
		t.Errorf("want EDE 15 naming pro, got %+v", ede)
	}
	m = s.res.Answer(d, query("ok.example.org", dns.TypeA), "doh", "")
	if m.Rcode != dns.RcodeSuccess || len(m.Answer) != 1 || m.Answer[0].(*dns.A).A.String() != "192.0.2.1" {
		t.Errorf("a clean name should be forwarded: %v", m)
	}
	m = s.res.Answer(d, query("cname.example.com", dns.TypeA), "doh", "")
	if m.Answer[0].(*dns.A).A.String() != "0.0.0.0" {
		t.Errorf("a CNAME to a listed name should block: %v", m.Answer)
	}
	m = s.res.Answer(d, query(canary, dns.TypeA), "doh", "")
	if m.Rcode != dns.RcodeNameError {
		t.Errorf("the Firefox canary should be NXDOMAIN, got %d", m.Rcode)
	}
	waitLogged(t, s, 4)
	ls, _ := s.store.Log(Filter{})
	byName := map[string]Lookup{}
	for _, l := range ls {
		byName[l.Name] = l
	}
	if l := byName["cname.example.com"]; l.Status != "block" || l.Via != "t.tracker.example" {
		t.Errorf("CNAME block should log the via: %+v", l)
	}
	if l := byName["ads.example.net"]; l.Device != d.ID || l.IP != "203.0.113.9" || l.Proto != "doh" {
		t.Errorf("lookup should carry the device and where it came from: %+v", l)
	}
}

func waitLogged(t *testing.T, s *site, n int) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if total, _ := s.store.Count(Filter{}); total >= n {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("lookups never reached the database")
}

func TestDoH(t *testing.T) {
	s, d := testSite(t)
	h := s.handler()
	raw, _ := query("ads.example.net", dns.TypeA).Pack()

	r := httptest.NewRequest(http.MethodGet, "/q/"+d.Token+"?dns="+base64.RawURLEncoding.EncodeToString(raw), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/dns-message" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET: %d %v", w.Code, w.Header())
	}
	m := new(dns.Msg)
	if err := m.Unpack(w.Body.Bytes()); err != nil || m.Answer[0].(*dns.A).A.String() != "0.0.0.0" {
		t.Errorf("GET answer: %v %v", err, m)
	}

	r = httptest.NewRequest(http.MethodPost, "/q/"+d.Token, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/dns-message")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("POST: %d", w.Code)
	}

	r = httptest.NewRequest(http.MethodGet, "/q/aaaaaaaaaaaaaaaa?dns="+base64.RawURLEncoding.EncodeToString(raw), nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("an unknown token must get nothing, got %d", w.Code)
	}
}

func TestInternalQuery(t *testing.T) {
	s, d := testSite(t)
	raw, _ := query("ok.example.org", dns.TypeA).Pack()
	r := httptest.NewRequest(http.MethodPost, "/internal/query", bytes.NewReader(raw))
	r.Header.Set("X-Dns-Token", d.Token)
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("internal query: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "/internal/query", bytes.NewReader(raw))
	r.Header.Set("X-Dns-Token", "nope")
	w = httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("unknown token: %d", w.Code)
	}
}

func TestTokenFrom(t *testing.T) {
	tok := newToken()
	if !validToken(tok) {
		t.Fatalf("new token %q isn't valid", tok)
	}
	if got, ok := tokenFrom(strings.ToUpper(tok) + ".DNS.bythewood.me"); !ok || got != tok {
		t.Errorf("tokenFrom upper: %q %v", got, ok)
	}
	for _, bad := range []string{"", "dns.bythewood.me", tok + ".evil.example", "x." + tok + ".dns.bythewood.me", "short.dns.bythewood.me"} {
		if _, ok := tokenFrom(bad); ok {
			t.Errorf("%q should not give a token", bad)
		}
	}
}

func TestParseSkipsComments(t *testing.T) {
	ds, v, err := parse(strings.NewReader("# Version: 2026.1\n! x\n\nads.example.com\nnot a domain\n*.wild.example\n"))
	if err != nil || v != "2026.1" || len(ds) != 2 || ds[1] != "wild.example" {
		t.Errorf("parse: %v %q %v", ds, v, err)
	}
}

func TestPagesRender(t *testing.T) {
	s, d := testSite(t)
	s.res.Answer(d, query("ads.example.net", dns.TypeA), "doh", "")
	s.res.Answer(d, query("ok.example.org", dns.TypeA), "dot", "")
	waitLogged(t, s, 2)
	_ = s.store.PutRule("fine.example", "block", "testing")
	_ = s.store.PutRuleFor("nas.example", "rewrite", "", d.ID, "192.168.1.20")
	pc, _ := s.store.AddDevice("desk", "computer")
	_ = s.reload()
	h := s.handler()
	for _, path := range []string{"/", "/log", "/log?status=blocked&range=15m&device=" + itoa(d.ID), "/log/rows?after=1", "/devices", "/devices/" + itoa(d.ID), "/devices/" + itoa(pc.ID),
		"/lists", "/lists?q=fine", "/check?domain=a.ads.example.net", "/check?domain=ok.example.org", "/check?domain=fine.example", "/docs", "/nope", "/api/report"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "good"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 200
		if path == "/nope" {
			want = 404
		}
		body, _ := io.ReadAll(w.Body)
		page := path != "/api/report" && !strings.HasPrefix(path, "/log/rows")
		if w.Code != want || bytes.Contains(body, []byte("<no value>")) || (page && !bytes.Contains(body, []byte("</html>"))) {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/", "/docs"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 {
			t.Errorf("signed out %s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/log", nil))
	if w.Code != http.StatusSeeOther && w.Code != http.StatusFound {
		t.Errorf("the log must need signing in, got %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/devices/"+itoa(d.ID)+"/profile", nil)
	r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "good"})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/q/"+d.Token) || !strings.Contains(w.Body.String(), "com.apple.dnsSettings.managed") {
		t.Errorf("profile: %d %s", w.Code, w.Body.String())
	}
}
