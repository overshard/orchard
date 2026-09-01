package main

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"auth.bythewood.me/web"
)

// newTestSite builds the real thing against a temporary database and a stub
// ntfy, so a template referencing a field that does not exist fails here rather
// than at execute time on the live site.
func newTestSite(t *testing.T) (*site, *stubNtfy) {
	t.Helper()

	db, err := openDB(t.TempDir() + "/db.sqlite3")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	dist := distFS()
	assets, err := web.LoadAssets(dist)
	if err != nil {
		t.Fatalf("load assets (run `make build SITE=auth.bythewood.me` first): %v", err)
	}

	templates, err := templateSub()
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	renderer, err := web.NewRenderer(templates, templateFuncs,
		[]string{"base.html", "partials.html"}, allPages)
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	stub := newStubNtfy(t)
	s := &site{
		renderer:   renderer,
		db:         db,
		dist:       dist,
		assets:     assets,
		notifier:   stub.notifier,
		baseScript: assets.Script("static_src/base/index.js"),
		baseStyles: assets.Styles("static_src/base/index.js"),
	}
	return s, stub
}

type stubNtfy struct {
	notifier *Notifier
	server   *httptest.Server
	bodies   []string
	titles   []string
}

func newStubNtfy(t *testing.T) *stubNtfy {
	t.Helper()
	s := &stubNtfy{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.bodies = append(s.bodies, string(body))
		s.titles = append(s.titles, r.Header.Get("Title"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.server.Close)

	s.notifier = &Notifier{
		client: s.server.Client(),
		base:   s.server.URL,
		topic:  ntfyTopic,
		token:  "tk_test",
	}
	return s
}

// lastCode pulls the six digits out of the stubbed notification title, which is
// the only place a test can see them, the same as a phone.
func (s *stubNtfy) lastCode(t *testing.T) string {
	t.Helper()
	if len(s.titles) == 0 {
		t.Fatal("no notification was published")
	}
	title := s.titles[len(s.titles)-1]
	fields := strings.Fields(title)
	return fields[len(fields)-1]
}

func TestEveryPageRenders(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Signed out first: the four public pages.
	for _, path := range []string{"/", "/login", "/recovery"} {
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s signed out: %d", path, rec.Code)
		}
	}

	cookie := signIn(t, s, stub)

	for _, path := range []string{"/account", "/sessions", "/security", "/activity"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s signed in: %d, body %s", path, rec.Code, rec.Body.String())
		}
	}

	// The pages with no route of their own.
	for name, render := range map[string]func(){
		"notfound": func() {
			rec := httptest.NewRecorder()
			s.notFound(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("notfound: %d", rec.Code)
			}
		},
		"uninitialized": func() {
			rec := httptest.NewRecorder()
			s.uninitialized(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("uninitialized: %d", rec.Code)
			}
		},
		"error": func() {
			rec := httptest.NewRecorder()
			s.fail(rec, httptest.NewRequest(http.MethodGet, "/", nil), "testing", sql.ErrNoRows)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("error: %d", rec.Code)
			}
		},
		"codes": func() {
			// Rendered by the rotate handler rather than a GET route, so
			// nothing else here would catch a bad field on it.
			rec := httptest.NewRecorder()
			data := s.page(httptest.NewRequest(http.MethodGet, "/security", nil),
				"Recovery codes", "")
			data.NewCodes = []string{"aaaa-bbbb-cccc", "dddd-eeee-ffff"}
			data.Remaining = 2
			s.renderer.Render(rec, http.StatusOK, "codes.html", data)
			if rec.Code != http.StatusOK || rec.Body.Len() < 500 {
				t.Fatalf("codes: %d, %d bytes", rec.Code, rec.Body.Len())
			}
		},
		"code": func() {
			rec := httptest.NewRecorder()
			s.codePage(rec, httptest.NewRequest(http.MethodGet, "/code", nil), "", "", http.StatusOK)
			if rec.Code != http.StatusOK {
				t.Fatalf("code: %d", rec.Code)
			}
		},
	} {
		t.Run(name, func(t *testing.T) { render() })
	}
}

// signIn walks the real two step flow and returns the session cookie.
func signIn(t *testing.T, s *site, stub *stubNtfy) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, postForm("/login", url.Values{"username": {seedUsername}}))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /login: %d, body %s", rec.Code, rec.Body.String())
	}
	pending := cookieNamed(rec.Result().Cookies(), pendingCookie)
	if pending == nil {
		t.Fatal("no pending cookie was set")
	}

	req := postForm("/code", url.Values{"code": {stub.lastCode(t)}})
	req.AddCookie(pending)
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /code: %d, body %s", rec.Code, rec.Body.String())
	}

	session := cookieNamed(rec.Result().Cookies(), sessionCookie)
	if session == nil {
		t.Fatal("no session cookie was set")
	}
	return session
}

func postForm(path string, values url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func cookieNamed(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name && c.Value != "" {
			return c
		}
	}
	return nil
}

// A code pushed to the phone has to be useless in a browser that did not ask
// for it, or somebody who can see the notification can sign in with it.
func TestCodeIsBoundToTheBrowserThatAskedForIt(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, postForm("/login", url.Values{"username": {seedUsername}}))
	code := stub.lastCode(t)

	// The right code, in a browser carrying no pending cookie.
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, postForm("/code", url.Values{"code": {code}}))
	if rec.Code == http.StatusSeeOther {
		t.Fatal("a code was accepted from a browser that never asked for one")
	}

	// And in a browser carrying somebody else's.
	req := postForm("/code", url.Values{"code": {code}})
	req.AddCookie(&http.Cookie{Name: pendingCookie, Value: "someone-elses-token"})
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusSeeOther {
		t.Fatal("a code was accepted with the wrong pending cookie")
	}
}

// The rule that turns a flood of requests into one notification.
func TestOnlyOneCodeIsOutstandingAtATime(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 4; i++ {
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, postForm("/login", url.Values{"username": {seedUsername}}))
	}
	if len(stub.titles) != 1 {
		t.Fatalf("four login requests published %d notifications, want 1", len(stub.titles))
	}
}

// The ceiling counts published notifications for the account and ignores where
// the request came from, which is what still holds when every request arrives
// from a different address.
func TestSendCeilingIgnoresTheSourceAddress(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < sendCeiling+3; i++ {
		// A fresh address every time, and each code consumed so the
		// one-outstanding rule is not what stops it.
		req := postForm("/login", url.Values{"username": {seedUsername}})
		req.Header.Set("CF-Connecting-IP", "203.0.113."+itoa(i+1))
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)

		_, _ = s.db.Exec(`UPDATE pending_logins SET consumed = 1`)
		loginBucket.tokens = loginBucket.burst
	}

	if len(stub.titles) > sendCeiling {
		t.Fatalf("published %d notifications from %d addresses, ceiling is %d",
			len(stub.titles), sendCeiling+3, sendCeiling)
	}
}

func TestWrongCodesAreBurnedAfterFiveTries(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, postForm("/login", url.Values{"username": {seedUsername}}))
	pending := cookieNamed(rec.Result().Cookies(), pendingCookie)
	real := stub.lastCode(t)

	wrong := "000000"
	if real == wrong {
		wrong = "111111"
	}
	for i := 0; i < maxAttempts; i++ {
		req := postForm("/code", url.Values{"code": {wrong}})
		req.AddCookie(pending)
		rec = httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		codeBucket.tokens = codeBucket.burst
	}

	// The real code must be dead too, or five guesses buys a sixth.
	req := postForm("/code", url.Values{"code": {real}})
	req.AddCookie(pending)
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusSeeOther {
		t.Fatal("the code still worked after five wrong guesses")
	}
}

func TestRecoveryCodesWorkOnceEach(t *testing.T) {
	s, _ := newTestSite(t)
	codes, err := regenerateRecoveryCodes(s.db)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != recoveryCount {
		t.Fatalf("generated %d codes, want %d", len(codes), recoveryCount)
	}

	remaining, err := useRecoveryCode(s.db, codes[0])
	if err != nil {
		t.Fatalf("first use: %v", err)
	}
	if remaining != recoveryCount-1 {
		t.Fatalf("remaining %d, want %d", remaining, recoveryCount-1)
	}

	if _, err := useRecoveryCode(s.db, codes[0]); err == nil {
		t.Fatal("the same recovery code worked twice")
	}
	if _, err := useRecoveryCode(s.db, "not-a-real-code"); err == nil {
		t.Fatal("a made up recovery code was accepted")
	}

	// Regenerating has to kill the old set, or an old slip of paper still works.
	if _, err := regenerateRecoveryCodes(s.db); err != nil {
		t.Fatal(err)
	}
	if _, err := useRecoveryCode(s.db, codes[1]); err == nil {
		t.Fatal("an old code survived a regenerate")
	}
}

// Revocation is the reason sessions are opaque rows rather than signed cookies,
// so it has to actually take effect on the next request.
func TestRevokingASessionEndsIt(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}
	cookie := signIn(t, s, stub)

	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("account before revoke: %d", rec.Code)
	}

	l, err := lookupSession(s.db, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := revokeSession(s.db, l.ID); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("account after revoke: %d, want a redirect to the login", rec.Code)
	}
}

func TestVerifyAnswersForTheOtherSites(t *testing.T) {
	s, stub := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/verify", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/verify with no cookie: %d", rec.Code)
	}

	cookie := signIn(t, s, stub)
	req := httptest.NewRequest(http.MethodGet, "/verify", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/verify with a live cookie: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"username":"`+seedUsername+`"`) {
		t.Fatalf("/verify said %s", rec.Body.String())
	}
}

// A login must never be able to bounce somebody off the platform.
func TestSafeNextStaysOnThePlatform(t *testing.T) {
	for next, want := range map[string]string{
		"":                                    defaultNext,
		"/sessions":                           "/sessions",
		"//evil.example":                      defaultNext,
		"/\\evil.example":                     defaultNext,
		"https://evil.example/":               defaultNext,
		"http://analytics.bythewood.me/":      defaultNext,
		"https://analytics.bythewood.me/x":    "https://analytics.bythewood.me/x",
		"https://bythewood.me.evil.example/":  defaultNext,
		"https://logging.bythewood.me/search": "https://logging.bythewood.me/search",
	} {
		if got := safeNext(next); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", next, got, want)
		}
	}
}

func TestInitIsIdempotent(t *testing.T) {
	s, _ := newTestSite(t)
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}
	codes, err := countRecoveryCodes(s.db)
	if err != nil || codes != recoveryCount {
		t.Fatalf("after init: %d codes, %v", codes, err)
	}

	if _, err := useRecoveryCode(s.db, "aaaa-bbbb-cccc"); err == nil {
		t.Fatal("a made up code was accepted")
	}

	// A second init must not replace the codes somebody already wrote down.
	if err := runInit(s.db); err != nil {
		t.Fatal(err)
	}
	again, _ := countRecoveryCodes(s.db)
	if again != recoveryCount {
		t.Fatalf("a second init changed the code count to %d", again)
	}
}

func TestSudoExpires(t *testing.T) {
	l := live{SudoAt: time.Now()}
	if !l.inSudo() {
		t.Fatal("a fresh login is not in sudo")
	}
	l.SudoAt = time.Now().Add(-sudoWindow - time.Minute)
	if l.inSudo() {
		t.Fatal("sudo outlived its window")
	}
}

// A site's own /login is a redirect to auth, so handing it back as the return
// address is the infinite loop it caused on logging.bythewood.me: auth sends you
// there, the stub sends you to auth, auth sees a session and sends you there.
func TestLoginURLNeverReturnsToALoginStub(t *testing.T) {
	for path, want := range map[string]string{
		"/login":                "https://logging.bythewood.me/",
		"/login?next=/overview": "https://logging.bythewood.me/overview",
		"/login?next=//evil":    "https://logging.bythewood.me/",
		"/login?next=/login":    "https://logging.bythewood.me/",
		"/overview":             "https://logging.bythewood.me/overview",
	} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Host = "logging.bythewood.me"
		got := web.LoginURL(r)
		if !strings.HasSuffix(got, url.QueryEscape(want)) {
			t.Errorf("LoginURL(%q) = %q, want it to return to %q", path, got, want)
		}
	}
}
