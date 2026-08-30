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
	"sync"
	"time"
)

// Single-operator authentication: one password, one signed cookie, no user table.
const (
	sessionCookie = "session"
	sessionTTL    = 30 * 24 * time.Hour
	// A speed bump, not a rate limit: the sleep is per goroutine, so
	// concurrent attempts all serve it at once. The bucket below is the limit.
	failedLoginDelay = 500 * time.Millisecond

	// Global rather than per-IP: per-IP state grows with every prober, and
	// there is exactly one legitimate user.
	loginBurst  = 5
	loginRefill = time.Second
)

// loginBucket guards POST /login and is safe for concurrent use.
var loginBucket = &tokenBucket{tokens: loginBurst, last: time.Now()}

type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// take consumes a token, reporting whether there was one to consume.
func (b *tokenBucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() / loginRefill.Seconds()
	if b.tokens > loginBurst {
		b.tokens = loginBurst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sessionKey derives the cookie signing key from the password, so rotating the
// password invalidates every outstanding session.
func sessionKey(password string) []byte {
	sum := sha512.Sum512(append([]byte("logging-cookie:"), password...))
	return sum[:]
}

// issueSession writes the signed cookie, payload "1:<unix expiry>". SameSite
// Strict stands in for CSRF tokens: a forged POST arrives without the cookie.
// Secure is fine in dev because browsers exempt localhost.
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
		http.Redirect(w, r, "/overview", http.StatusSeeOther)
		return
	}
	data := s.page(r, "Log in", "Log in to the log dashboard.")
	data.Next = "/overview"
	s.renderer.Render(w, http.StatusOK, "login.html", data)
}

func (s *site) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Before the password is looked at, so a refused attempt times nothing.
	if !loginBucket.take() {
		w.Header().Set("Retry-After", "5")
		data := s.page(r, "Log in", "Log in to the log dashboard.")
		data.Next = "/overview"
		data.Error = "Too many attempts. Wait a few seconds."
		s.renderer.Render(w, http.StatusTooManyRequests, "login.html", data)
		return
	}

	if !passwordMatches(r.PostFormValue("password"), s.password) {
		time.Sleep(failedLoginDelay)
		data := s.page(r, "Log in", "Log in to the log dashboard.")
		data.Next = "/overview"
		data.Error = "Invalid password."
		s.renderer.Render(w, http.StatusUnauthorized, "login.html", data)
		return
	}

	issueSession(w, s.cookieKey)
	http.Redirect(w, r, safeNext(r.PostFormValue("next")), http.StatusSeeOther)
}

// safeNext keeps the post-login redirect on this site. "starts with /" alone is
// not enough: "//evil.example" and "/\evil.example" are followed off-site.
func safeNext(next string) string {
	if strings.HasPrefix(next, "/") &&
		!strings.HasPrefix(next, "//") &&
		!strings.HasPrefix(next, "/\\") {
		return next
	}
	return "/overview"
}

func (s *site) logout(w http.ResponseWriter, r *http.Request) {
	clearSession(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *site) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAuthenticated(r, s.cookieKey) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
