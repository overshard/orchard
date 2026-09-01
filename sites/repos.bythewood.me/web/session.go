package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// Single sign-on for every bythewood.me site, against auth.bythewood.me.
//
// The cookie is opaque, so a site cannot check it for itself and has to ask.
// That is the point: a signed cookie is valid until it expires no matter what
// the issuer says, and asking is what makes revoking a session take effect
// everywhere on the next request rather than whenever the signature ages out.
//
// The answer is not cached. A call to another container on the bridge is well
// under a millisecond and these are dashboards one person reads, so a cache
// would buy nothing and would put a window on revocation, which is the feature
// this exists for.
//
// The cost is real and worth stating: with orchard-auth down, every site behind
// this is unreachable. That is why auth.bythewood.me has recovery codes and why
// nothing public is behind it.
const (
	SessionCookie = "bw_session"

	authVerifyURL = "http://orchard-auth:8000/verify"
	authLoginURL  = "https://auth.bythewood.me/login"

	// Short. A dashboard that hangs because auth is slow is worse than one
	// that says you are signed out.
	authTimeout = 3 * time.Second
)

// Authenticator answers whether a request carries a live session.
type Authenticator struct {
	client *http.Client
	verify string
}

func NewAuthenticator() *Authenticator { return NewAuthenticatorAt(authVerifyURL) }

// NewAuthenticatorAt points at a different verifier, which is what the tests
// use to stand one up without a running auth container.
func NewAuthenticatorAt(verify string) *Authenticator {
	return &Authenticator{
		client: &http.Client{
			Timeout: authTimeout,
			// A verify call must never follow a redirect: the answer is the
			// status code, and a 302 to somewhere else is not an answer.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		verify: verify,
	}
}

// Authenticated reports whether the caller is signed in. Every failure is a no,
// including auth being unreachable, because the alternative is failing open.
func (a *Authenticator) Authenticated(r *http.Request) bool {
	// A site built without one is signed out rather than a panic, which matters
	// because this is called from the page data every template renders.
	if a == nil || a.client == nil {
		return false
	}

	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return false
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, a.verify, nil)
	if err != nil {
		return false
	}
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: c.Value})

	resp, err := a.client.Do(req)
	if err != nil {
		slog.Error("verifying a session failed",
			slog.String("component", "auth"),
			slog.Any("err", err))
		return false
	}
	defer resp.Body.Close()

	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body); err != nil {
		return false
	}
	return resp.StatusCode == http.StatusOK && body.OK
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

// RequireAuthJSON gates the endpoints a dashboard's own JavaScript calls, where
// a redirect to an HTML login page would be parsed as data.
func (a *Authenticator) RequireAuthJSON(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Authenticated(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"not signed in"}`))
			return
		}
		next(w, r)
	}
}

// LoginURL is where a signed out visitor goes, carrying an absolute return
// address because the login is on another host.
func LoginURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") == "http" {
		scheme = "http"
	}
	back := scheme + "://" + r.Host + r.URL.RequestURI()
	return authLoginURL + "?next=" + url.QueryEscape(back)
}

// LogoutURL ends the session for every site at once, which is the only sensible
// meaning of signing out when one cookie covers all of them.
func LogoutURL() string { return "https://auth.bythewood.me/" }
