package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Single sign-on against auth.bythewood.me. The cookie is opaque, so every
// request asks auth and nothing caches the answer, since a cache would leave a
// revoked session working for a while. With auth down every gated site is too.
const (
	SessionCookie = "bw_session"

	authVerifyURL = "http://orchard-auth:8000/verify"
	authLoginURL  = "https://auth.bythewood.me/login"

	// A dashboard that hangs because auth is slow is worse than one that says
	// you are signed out.
	authTimeout = 3 * time.Second
)

// Authenticator answers whether a request carries a live session.
type Authenticator struct {
	client *http.Client
	verify string
}

func NewAuthenticator() *Authenticator { return NewAuthenticatorAt(authVerifyURL) }

// NewAuthenticatorAt points at a different verifier, so a test can stand one up
// without a running auth container.
func NewAuthenticatorAt(verify string) *Authenticator {
	return &Authenticator{
		client: &http.Client{
			Timeout: authTimeout,
			// A verify call must never follow a redirect, since the answer is
			// the status code and a 302 to somewhere else is not one.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		verify: verify,
	}
}

// Authenticated reports whether the caller is signed in. Every failure is a no,
// including auth being unreachable, because the alternative is failing open.
// A connector's session only reads, so it's signed out on anything but GET and HEAD.
func (a *Authenticator) Authenticated(r *http.Request) bool {
	ok, connector, _ := a.check(r)
	return ok && (!connector || r.Method == http.MethodGet || r.Method == http.MethodHead)
}

// check asks auth about the cookie, and says whether it's a connector's and
// whether it signed in recently enough for sudo.
func (a *Authenticator) check(r *http.Request) (ok, connector, sudo bool) {
	// A site built without one is signed out instead of a panic, since this is
	// called from the page data every template renders.
	if a == nil || a.client == nil {
		return false, false, false
	}

	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return false, false, false
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, a.verify, nil)
	if err != nil {
		return false, false, false
	}
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: c.Value})

	resp, err := a.client.Do(req)
	if err != nil {
		// A visitor closing the tab is not auth failing.
		if errors.Is(err, context.Canceled) {
			return false, false, false
		}
		slog.Error("verifying a session failed",
			slog.String("component", "auth"),
			slog.Any("err", err))
		return false, false, false
	}
	defer resp.Body.Close()

	var body struct {
		OK     bool   `json:"ok"`
		Method string `json:"method"`
		Sudo   bool   `json:"sudo"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body); err != nil {
		return false, false, false
	}
	ok = resp.StatusCode == http.StatusOK && body.OK
	return ok, body.Method == "connector", ok && body.Sudo
}

// RequireAuth gates a handler, sending anyone without a session to the login
// with a way back.
func (a *Authenticator) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Authenticated(r) {
			http.Redirect(w, r, LoginURL(r), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// RequireSudo gates a change that a stolen cookie shouldn't get to make, so it
// wants a sign in from the last few minutes. Auth's /login sends a live session
// straight back, so this answers 403 instead of redirecting into a loop.
func (a *Authenticator) RequireSudo(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, connector, sudo := a.check(r)
		if !ok || connector {
			http.Redirect(w, r, LoginURL(r), http.StatusSeeOther)
			return
		}
		if !sudo {
			http.Error(w, "This needs a sign in from the last ten minutes, so sign out and in again at "+LogoutURL()+" and come back.",
				http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// RequireAuthJSON gates the endpoints a dashboard's own JavaScript calls, where
// a redirect to an HTML login page would be parsed as data.
func (a *Authenticator) RequireAuthJSON(next http.HandlerFunc) http.HandlerFunc {
	return a.requireJSON(a.Authenticated, next)
}

// RequireAuthJSONConnector is RequireAuthJSON that also lets a connector's
// session write, for the few endpoints mcp's tools post to.
func (a *Authenticator) RequireAuthJSONConnector(next http.HandlerFunc) http.HandlerFunc {
	return a.requireJSON(func(r *http.Request) bool {
		ok, _, _ := a.check(r)
		return ok
	}, next)
}

func (a *Authenticator) requireJSON(signedIn func(*http.Request) bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"not signed in"}`))
			return
		}
		next(w, r)
	}
}

// LoginURL is where a signed out visitor goes, with an absolute return address
// since the login is on another host. It never returns to this site's /login,
// which redirects to auth and would loop, so it uses ?next= or the root instead.
func LoginURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") == "http" {
		scheme = "http"
	}

	target := r.URL.RequestURI()
	if r.URL.Path == "/login" {
		target = "/"
		if n := r.URL.Query().Get("next"); strings.HasPrefix(n, "/") &&
			!strings.HasPrefix(n, "//") && !strings.HasPrefix(n, "/\\") &&
			n != "/login" {
			target = n
		}
	}

	back := scheme + "://" + r.Host + target
	return authLoginURL + "?next=" + url.QueryEscape(back)
}

// LogoutURL ends the session for every site at once, since one cookie covers
// all of them.
func LogoutURL() string { return "https://auth.bythewood.me/" }
