package main

import (
	"context"
	"database/sql"
	"encoding/base32"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pass.bythewood.me/ntfy"
	"pass.bythewood.me/web"
)

// The columns this site reads, as Vaultwarden's migrations make them.
const schema = `
CREATE TABLE users (uuid TEXT PRIMARY KEY, enabled BOOLEAN NOT NULL DEFAULT 1, email TEXT, created_at DATETIME, totp_recover TEXT);
CREATE TABLE twofactor (uuid TEXT PRIMARY KEY, user_uuid TEXT, atype INTEGER, enabled BOOLEAN, data TEXT, last_used BIGINT, UNIQUE (user_uuid, atype));
CREATE TABLE ciphers (uuid TEXT PRIMARY KEY, created_at DATETIME, updated_at DATETIME, user_uuid TEXT, atype INTEGER, name TEXT, data TEXT, deleted_at DATETIME);
CREATE TABLE folders (uuid TEXT PRIMARY KEY, name TEXT);
CREATE TABLE sends (uuid TEXT PRIMARY KEY, name TEXT);
CREATE TABLE attachments (id TEXT PRIMARY KEY, cipher_uuid TEXT, file_name TEXT, file_size BIGINT);
CREATE TABLE devices (uuid TEXT, created_at DATETIME, updated_at DATETIME, user_uuid TEXT, name TEXT, atype INTEGER,
	push_uuid TEXT, push_token TEXT, refresh_token TEXT, twofactor_remember TEXT, PRIMARY KEY (uuid, user_uuid));
CREATE TABLE event (uuid TEXT PRIMARY KEY, event_type INTEGER, user_uuid TEXT, org_uuid TEXT, act_user_uuid TEXT, device_type INTEGER,
	ip_address TEXT, event_date DATETIME);
`

type fixture struct {
	db   *sql.DB
	path string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(wal)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return &fixture{db: db, path: path}
}

func (f *fixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) event(t *testing.T, id string, kind int, ip string, at time.Time, org any) {
	f.exec(t, `INSERT INTO event (uuid, event_type, user_uuid, org_uuid, device_type, ip_address, event_date) VALUES (?, ?, 'u1', ?, 2, ?, ?)`,
		id, kind, org, ip, at.UTC().Format("2006-01-02 15:04:05.000000"))
}

func (f *fixture) seed(t *testing.T, now time.Time) {
	f.exec(t, `INSERT INTO users (uuid, enabled, email, created_at) VALUES ('u1', 1, 'me@example.com', ?)`, vwArg(now))
	f.exec(t, `INSERT INTO twofactor VALUES ('t1', 'u1', 3, 1, '', 0)`)
	f.exec(t, `INSERT INTO ciphers VALUES ('c1', ?, ?, 'u1', 1, '2.x', '{"username":"2.a","totp":"2.b"}', NULL)`, vwArg(now), vwArg(now))
	f.exec(t, `INSERT INTO ciphers VALUES ('c2', ?, ?, 'u1', 1, '2.x', '{"username":"2.a","totp":null}', NULL)`, vwArg(now), vwArg(now))
	f.exec(t, `INSERT INTO ciphers VALUES ('c3', ?, ?, 'u1', 2, '2.x', '{}', NULL)`, vwArg(now), vwArg(now))
	f.exec(t, `INSERT INTO ciphers VALUES ('c4', ?, ?, 'u1', 1, '2.x', '{}', ?)`, vwArg(now), vwArg(now), vwArg(now))
	f.exec(t, `INSERT INTO attachments VALUES ('a1', 'c1', '2.f', 2048)`)
	f.exec(t, `INSERT INTO devices VALUES ('d1', ?, ?, 'u1', 'chrome', 2, NULL, NULL, 'r', NULL)`, vwArg(now.Add(-40*24*time.Hour)), vwArg(now.Add(-time.Hour)))
	f.exec(t, `INSERT INTO devices VALUES ('d2', ?, ?, 'u1', 'Pixel 9', 0, 'p', 'tok', 'r', NULL)`, vwArg(now.Add(-2*time.Hour)), vwArg(now.Add(-2*time.Hour)))
	f.event(t, "e1", evLogin, "198.51.100.7", now.Add(-3*time.Hour), nil)
	f.event(t, "e1o", evLogin, "198.51.100.7", now.Add(-3*time.Hour), "org1")
	f.event(t, "e2", evFailedLogin, "203.0.113.48", now.Add(-2*time.Hour), nil)
	f.event(t, "e3", evFailed2FA, "198.51.100.7", now.Add(-5*24*time.Hour), nil)
}

type fakePush struct {
	mu   sync.Mutex
	sent []ntfy.Message
}

func (f *fakePush) Deliver(_ context.Context, m ntfy.Message) error {
	f.mu.Lock()
	f.sent = append(f.sent, m)
	f.mu.Unlock()
	return nil
}

func (f *fakePush) titles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.sent {
		out = append(out, m.Title)
	}
	return out
}

func testSite(t *testing.T, f *fixture) (*site, *fakePush) {
	t.Helper()
	verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(web.SessionCookie); err == nil && c.Value == "good" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false}`))
	}))
	t.Cleanup(verify.Close)
	v, err := OpenVault(f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	push := &fakePush{}
	s, err := newSite(v, web.NewAuthenticatorAt(verify.URL), push)
	if err != nil {
		t.Fatal(err)
	}
	return s, push
}

func get(t *testing.T, h http.Handler, path string, signedIn bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if signedIn {
		req.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "good"})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPages(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC()
	f.seed(t, now)
	s, _ := testSite(t, f)
	h := s.handler()
	cases := []struct {
		path   string
		in     bool
		status int
		body   string
	}{
		{"/", false, 200, "Every password"},
		{"/", true, 200, "Two devices"},
		{"/logins", true, 200, "203.0.113.48"},
		{"/logins?show=failed&range=7d", true, 200, "wrong two step code"},
		{"/devices", true, 200, "Pixel 9"},
		{"/account", true, 200, "data:image/png;base64,"},
		{"/healthz", false, 200, "ok"},
		{"/robots.txt", false, 200, "Disallow"},
		{"/favicon.svg", false, 200, "<svg"},
		{"/nothing-here", false, 404, "No such page"},
	}
	for _, c := range cases {
		rec := get(t, h, c.path, c.in)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.status)
		}
		if !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: body does not contain %q", c.path, c.body)
		}
	}
	if rec := get(t, h, "/logins", false); rec.Code == 200 {
		t.Error("/logins answered signed out")
	}
}

// Vaultwarden hasn't made its database on a fresh volume, and the pages
// still have to answer.
func TestPagesWithNoDatabase(t *testing.T) {
	v, err := OpenVault(filepath.Join(t.TempDir(), "missing.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	s, err := newSite(v, web.NewAuthenticatorAt("http://127.0.0.1:1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec := get(t, s.handler(), "/", false); rec.Code != 200 {
		t.Errorf("home answered %d", rec.Code)
	}
}

func TestHoldings(t *testing.T) {
	f := newFixture(t)
	f.seed(t, time.Now())
	v, _ := OpenVault(f.path)
	defer v.Close()
	h, err := v.Holdings()
	if err != nil {
		t.Fatal(err)
	}
	if h.Logins != 2 || h.Notes != 1 || h.Trash != 1 || h.TOTP != 1 || h.Users != 1 || h.TwoFactor != 1 || h.FileBytes != 2048 {
		t.Errorf("holdings %+v", h)
	}
}

func TestEventsSkipOrgCopies(t *testing.T) {
	f := newFixture(t)
	f.seed(t, time.Now())
	v, _ := OpenVault(f.path)
	defer v.Close()
	evs, err := v.Events(time.Now().Add(-24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	if evs[0].Type != evFailedLogin || evs[0].IP != "203.0.113.48" {
		t.Errorf("newest first, got %+v", evs[0])
	}
}

func TestAddrsPutFailuresFirst(t *testing.T) {
	now := time.Now()
	as := addrs([]Event{
		{Type: evLogin, IP: "a", At: now}, {Type: evLogin, IP: "a", At: now},
		{Type: evFailedLogin, IP: "b", At: now},
		{Type: evChangedPass, IP: "c", At: now},
	})
	if len(as) != 2 || as[0].IP != "b" || as[1].OK != 2 {
		t.Errorf("addrs %+v", as)
	}
}

// A restart must not push what happened before it, and a scan must push each
// failure and new device once.
func TestWatcherPushesOnce(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC()
	f.seed(t, now)
	v, _ := OpenVault(f.path)
	defer v.Close()
	push := &fakePush{}
	w := newWatcher(v, push)
	w.scan(context.Background(), now)
	f.event(t, "e9", evFailedLogin, "203.0.113.9", time.Now().Add(time.Second), nil)
	f.exec(t, `INSERT INTO devices VALUES ('d3', ?, ?, 'u1', 'firefox', 3, NULL, NULL, 'r', NULL)`, vwArg(now), vwArg(now))
	w.scan(context.Background(), time.Now().Add(2*time.Second))
	w.scan(context.Background(), time.Now().Add(3*time.Second))
	deadline := time.Now().Add(2 * time.Second)
	for len(push.titles()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	got := push.titles()
	if len(got) != 2 {
		t.Fatalf("pushed %q, want a wrong password and a new device", got)
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "wrong password") || !strings.Contains(joined, "new device") {
		t.Errorf("pushed %q", got)
	}
}

func post(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	req.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "good"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTOTPMatchesTheRFC(t *testing.T) {
	// RFC 6238's SHA1 vector, the ASCII secret 12345678901234567890 at 59s.
	secret := base32.StdEncoding.EncodeToString([]byte("12345678901234567890"))
	if got, _ := totpAt(secret, 59/30); got != "287082" {
		t.Errorf("got %s, want 287082", got)
	}
	if _, ok := checkTOTP(secret, "287082", time.Unix(59+30, 0)); !ok {
		t.Error("a code one step old should pass")
	}
	if _, ok := checkTOTP(secret, "287082", time.Unix(59+90, 0)); ok {
		t.Error("a code three steps old should fail")
	}
}

// Turning it on writes what Vaultwarden's own API writes, and turning it off
// needs a current code and forgets remembered devices.
func TestTwoStepOnAndOff(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC()
	f.seed(t, now)
	f.exec(t, `UPDATE devices SET twofactor_remember = 'r'`)
	s, _ := testSite(t, f)
	h := s.handler()
	secret := newSecret()
	if rec := post(t, h, "/account/totp", url.Values{"secret": {secret}, "code": {"000000"}}); rec.Code != 400 || !strings.Contains(rec.Body.String(), "didn&#39;t match") {
		t.Fatalf("a wrong code answered %d", rec.Code)
	}
	code, _ := totpAt(secret, time.Now().Unix()/30)
	rec := post(t, h, "/account/totp", url.Values{"secret": {secret}, "code": {code}})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Recovery code") {
		t.Fatalf("enabling answered %d", rec.Code)
	}
	var data string
	var used int64
	if err := f.db.QueryRow(`SELECT data, last_used FROM twofactor WHERE user_uuid = 'u1' AND atype = 0 AND enabled`).Scan(&data, &used); err != nil {
		t.Fatal(err)
	}
	if data != secret || used == 0 {
		t.Errorf("saved %q at step %d", data, used)
	}
	var recover string
	_ = f.db.QueryRow(`SELECT totp_recover FROM users WHERE uuid = 'u1'`).Scan(&recover)
	if len(recover) != 32 {
		t.Errorf("recovery code %q", recover)
	}
	if rec := post(t, h, "/account/totp/off", url.Values{"code": {"000000"}}); rec.Code != 400 {
		t.Errorf("turning off with a wrong code answered %d", rec.Code)
	}
	code, _ = totpAt(secret, time.Now().Unix()/30)
	if rec := post(t, h, "/account/totp/off", url.Values{"code": {code}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("turning off answered %d", rec.Code)
	}
	var left, remembered int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM twofactor WHERE atype = 0`).Scan(&left)
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM devices WHERE twofactor_remember IS NOT NULL`).Scan(&remembered)
	if left != 0 || remembered != 0 {
		t.Errorf("%d authenticators and %d remembered devices left", left, remembered)
	}
	var events int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM event WHERE event_type IN (1002, 1003)`).Scan(&events)
	if events != 2 {
		t.Errorf("%d account events, want 2", events)
	}
}

func TestAdminSignups(t *testing.T) {
	var got []string
	vw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/admin" && r.FormValue("token") == "tok":
			http.SetCookie(w, &http.Cookie{Name: "VW_ADMIN", Value: "jwt", Secure: true})
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/admin":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			c, err := r.Cookie("VW_ADMIN")
			if err != nil || c.Value != "jwt" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			b, _ := io.ReadAll(r.Body)
			got = append(got, r.URL.Path+" "+string(b))
		}
	}))
	defer vw.Close()
	dir := t.TempDir()
	a := newAdmin(vw.URL, "tok", filepath.Join(dir, "db.sqlite3"))
	if a.Signups() {
		t.Error("no config file should read as off")
	}
	if err := a.SetSignups(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != `/admin/config {"signups_allowed":true}` {
		t.Errorf("sent %q", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"signups_allowed": true, "domain": "x"}`), 0o600)
	if !a.Signups() {
		t.Error("the saved config says on")
	}
	if err := newAdmin(vw.URL, "wrong", "x").SetSignups(context.Background(), true); err == nil {
		t.Error("a wrong token went through")
	}
}
