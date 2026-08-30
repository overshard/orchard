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

const (
	sessionCookie = "session"
	sessionTTL    = 30 * 24 * time.Hour
	// A flat delay on every failed attempt avoids tracking per-IP state.
	failedLoginDelay = 500 * time.Millisecond
)

// sessionKey derives the cookie signing key from the password, so rotating the
// password invalidates every outstanding session.
func sessionKey(password string) []byte {
	sum := sha512.Sum512(append([]byte("status-cookie:"), password...))
	return sum[:]
}

// issueSession writes the signed cookie, "1:<unix expiry>" plus HMAC-SHA256.
// SameSite=Strict stands in for CSRF tokens, so there are none to look for.
// Secure is safe in dev because browsers exempt localhost.
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
// session. The signature is checked before the expiry is parsed.
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

// passwordMatches compares in constant time. Both sides are hashed first so the
// comparison runs over fixed-length digests and cannot leak the length.
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

// safeNext keeps the post-login redirect on this site. "starts with /" is not
// enough: "//evil.example" is protocol-relative, and so is "/\evil.example".
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

// requireAuthJSON gates the endpoints the dashboard JavaScript calls; a
// redirect to the login page reaches a fetch() as a 200 full of HTML.
func (s *site) requireAuthJSON(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r, s.cookieKey) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		next(w, r)
	}
}
