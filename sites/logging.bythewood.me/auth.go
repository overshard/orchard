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

// Single-operator authentication: one password, one signed cookie, no user
// table.

const (
	sessionCookie = "session"
	sessionTTL    = 30 * 24 * time.Hour
	// A flat delay on every failed attempt. It is a speed bump, not a rate
	// limit: the sleep is per goroutine, so N concurrent attempts all serve
	// their 500ms at once. Measured before the bucket below existed, 200
	// concurrent wrong passwords completed in 509ms, which is 392 guesses a
	// second, and each attempt also parked a goroutine for half a second on a
	// one-CPU container.
	failedLoginDelay = 500 * time.Millisecond

	// The actual limit. One global bucket rather than per-IP state, which
	// would be a map that grows with every prober and would in any case be
	// keyed on a header Cloudflare sets. There is exactly one legitimate user,
	// so a global ceiling costs him nothing and bounds everyone.
	loginBurst  = 5
	loginRefill = time.Second
)

// loginBucket is the global token bucket guarding POST /login.
var loginBucket = &tokenBucket{tokens: loginBurst, last: time.Now()}

type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// take refills by elapsed time and consumes one token, reporting whether there
// was one to consume.
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

// sessionKey derives the cookie signing key from the password, rather than
// taking a second configured secret. Rotating the password then invalidates
// every outstanding session, which is what rotating a password is usually
// meant to do.
func sessionKey(password string) []byte {
	sum := sha512.Sum512(append([]byte("logging-cookie:"), password...))
	return sum[:]
}

// issueSession writes the signed cookie. The payload is "1:<unix expiry>",
// versioned so the format can change later.
//
// SameSite=Strict stands in for CSRF tokens. Every state-changing route here
// is a POST, and Strict means the browser will not attach this cookie to a
// request another site originated, so a forged POST arrives unauthenticated.
// A reader looking for CSRF tokens will not find any.
//
// Secure is safe in dev because browsers exempt localhost, and behind the
// tunnel every real request is HTTPS at the edge.
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
// session. The signature is checked before the expiry is parsed, so a forged
// cookie never reaches the parsing code.
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

// passwordMatches compares in constant time. Both sides are hashed first so
// the comparison runs over two fixed 64-byte digests; comparing the raw
// strings would leak the password's length through timing even under a
// constant-time compare.
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

	// Before the password is even looked at, so a refused attempt costs
	// nothing and cannot be used to time anything.
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

// safeNext keeps the post-login redirect on this site. A bare "starts with /"
// test is not enough: "//evil.example" also starts with a slash and is a
// protocol-relative URL browsers follow off-site, as is "/\evil.example" in
// some browsers.
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
