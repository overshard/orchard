package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Single-operator authentication: one password, one signed cookie, no user
// table. There is exactly one person who logs into this and it is the person
// who deployed it.

const (
	sessionCookie = "session"
	sessionTTL    = 30 * 24 * time.Hour
	// A flat delay on every failed attempt. It keeps online brute force
	// impractical for a single-password app without tracking per-IP state,
	// which would mean either a table or a map that grows with every prober.
	failedLoginDelay = 500 * time.Millisecond
)

// sessionKey derives the cookie signing key from the password.
//
// Deriving rather than configuring is a deliberate subtraction: the Rust
// version also accepted ANALYTICS_COOKIE_SECRET, documented as optional, which
// is exactly the config-or-credential gray zone decisions/0008 set out to
// delete. Deriving has a real property too. Rotating the password invalidates
// every outstanding session, which is what someone rotating a password wants
// and what a separate secret would quietly fail to do.
func sessionKey(password string) []byte {
	sum := sha512.Sum512(append([]byte("analytics-cookie:"), password...))
	return sum[:]
}

// issueSession writes the signed cookie.
//
// The payload is "1:<unix expiry>". The version prefix is the Rust format,
// kept so a session issued by the Rust process is still valid after the
// cutover; the signature will not match across the swap anyway, but the format
// costs nothing and documents that the payload is versioned.
//
// SameSite=Strict rather than CSRF tokens. Every state-changing route here is
// a POST, and Strict means the browser will not attach this cookie to a
// request another site originated at all, so a forged POST arrives
// unauthenticated. That is the whole protection and it is worth stating,
// because a reader looking for CSRF tokens will not find any.
//
// Secure is new; the Rust version left it off. It is safe in dev because
// browsers exempt localhost from the requirement, and behind the tunnel every
// real request is HTTPS at the edge.
func issueSession(w http.ResponseWriter, key []byte) {
	exp := time.Now().Add(sessionTTL).Unix()
	payload := "1:" + strconv.FormatInt(exp, 10)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    payload + "." + sign(key, payload),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   true,
		MaxAge:   int(sessionTTL / time.Second),
	})
}

func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   true,
		MaxAge:   -1,
	})
}

func sign(key []byte, payload string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// isAuthenticated reports whether the request carries a valid, unexpired
// session.
//
// The signature is checked before the expiry is parsed, so a forged cookie
// never reaches the parsing code at all.
func isAuthenticated(r *http.Request, key []byte) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	payload, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	if !hmac.Equal([]byte(sig), []byte(sign(key, payload))) {
		return false
	}

	version, expStr, ok := strings.Cut(payload, ":")
	if !ok || version != "1" {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < exp
}

// passwordMatches compares in constant time.
//
// Both sides are hashed first so the comparison runs over two fixed 64-byte
// digests. Comparing the raw strings would leak the password's length through
// timing even under a constant-time compare, because the compare itself has to
// look at how much there is to compare.
func passwordMatches(supplied, actual string) bool {
	a := sha512.Sum512([]byte(supplied))
	b := sha512.Sum512([]byte(actual))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

func (s *site) loginForm(w http.ResponseWriter, r *http.Request) {
	if isAuthenticated(r, s.cookieKey) {
		http.Redirect(w, r, "/properties", http.StatusSeeOther)
		return
	}
	data := s.page(r, "Log in", "Log in to your dashboard.")
	data.Next = "/properties"
	s.renderer.Render(w, http.StatusOK, "login.html", data)
}

func (s *site) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if !passwordMatches(r.PostFormValue("password"), s.password) {
		time.Sleep(failedLoginDelay)
		data := s.page(r, "Log in", "Log in to your dashboard.")
		data.Next = "/properties"
		data.Error = "Invalid password."
		s.renderer.Render(w, http.StatusUnauthorized, "login.html", data)
		return
	}

	issueSession(w, s.cookieKey)
	http.Redirect(w, r, safeNext(r.PostFormValue("next")), http.StatusSeeOther)
}

// safeNext keeps the post-login redirect on this site.
//
// A bare "starts with /" test is not enough: "//evil.example" also starts with
// a slash and is a protocol-relative URL that browsers follow off-site. So is
// "/\evil.example" in some browsers, which is why the backslash is checked
// too.
func safeNext(next string) string {
	if strings.HasPrefix(next, "/") &&
		!strings.HasPrefix(next, "//") &&
		!strings.HasPrefix(next, "/\\") {
		return next
	}
	return "/properties"
}

func (s *site) logout(w http.ResponseWriter, r *http.Request) {
	clearSession(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// requireAuth wraps a handler that only the operator may reach.
func (s *site) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r, s.cookieKey) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
