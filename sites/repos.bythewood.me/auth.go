package main

// Two authentication surfaces, one operator.
//
//	the browser UI    a signed session cookie, from a password login form.
//	                  This file. Same shape as logging.bythewood.me/auth.go,
//	                  because it is the same problem and one behaviour across
//	                  the repo beats two.
//	the git wire      HTTP Basic, carrying a random token. wire.go and db.go.
//
// They are separate on purpose. Basic auth exists on the git side only because
// git's credential subsystem speaks nothing else, and a browser should never be
// asked for it. Tokens are minted from the logged-in UI, so the password is the
// root credential and a token is a derived one that can be revoked alone.

import (
	"context"
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

const (
	sessionCookie = "session"
	sessionTTL    = 30 * 24 * time.Hour

	// A flat delay on every failed attempt. It is a speed bump, not the
	// limit: the sleep is per goroutine, so N concurrent attempts all serve
	// their 500ms at once.
	failedLoginDelay = 500 * time.Millisecond

	// The actual limit. One global bucket rather than per-IP state, which
	// would be a map that grows with every prober and would in any case be
	// keyed on a header Cloudflare sets. There is exactly one legitimate
	// user, so a global ceiling costs him nothing and bounds everyone.
	loginBurst  = 5
	loginRefill = time.Second
)

var loginBucket = &tokenBucket{tokens: loginBurst, last: time.Now()}

type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

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

// sessionKey derives the cookie signing key from the password rather than
// taking a second configured secret. Rotating the password then invalidates
// every outstanding session, which is what rotating a password is usually meant
// to do.
func sessionKey(password string) []byte {
	sum := sha512.Sum512(append([]byte("repos-cookie:"), password...))
	return sum[:]
}

// issueSession writes the signed cookie. The payload is "1:<unix expiry>",
// versioned so the format can change later.
//
// SameSite=Strict stands in for CSRF tokens. Every state-changing route here is
// a POST, and Strict means the browser will not attach this cookie to a request
// another site caused.
func issueSession(w http.ResponseWriter, password string) {
	expiry := time.Now().Add(sessionTTL).Unix()
	payload := "1:" + strconv.FormatInt(expiry, 10)

	mac := hmac.New(sha256.New, sessionKey(password))
	mac.Write([]byte(payload))
	value := payload + ":" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	http.SetCookie(w, &http.Cookie{
		Name:  sessionCookie,
		Value: value,
		Path:  "/",
		// Secure even though this process speaks plaintext on a bridge:
		// the browser only ever sees this cookie over the tunnel, where the
		// connection is HTTPS, and the flag is about that connection.
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(sessionTTL),
		MaxAge:   int(sessionTTL / time.Second),
	})
}

func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// validSession verifies the cookie: signature first, then expiry. In that
// order, because an expiry read off an unverified cookie is a number the client
// chose.
func validSession(r *http.Request, password string) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}

	i := strings.LastIndex(c.Value, ":")
	if i < 0 {
		return false
	}
	payload, sig := c.Value[:i], c.Value[i+1:]

	mac := hmac.New(sha256.New, sessionKey(password))
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) != 1 {
		return false
	}

	version, expiry, ok := strings.Cut(payload, ":")
	if !ok || version != "1" {
		return false
	}
	unix, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < unix
}

// checkPassword is the login comparison. Constant time, and rate limited by the
// caller rather than here so the limit applies to the attempt rather than to
// the comparison.
func checkPassword(given, want string) bool {
	return subtle.ConstantTimeCompare([]byte(given), []byte(want)) == 1
}

// userKey is the context key for the token label a push authenticated as.
type userKey struct{}

func withUser(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, userKey{}, label)
}

// userFrom reads the token label back out. Empty means the request was not
// authenticated, which for the wire means it is a read.
func userFrom(ctx context.Context) string {
	label, _ := ctx.Value(userKey{}).(string)
	return label
}

// clientIPOf is the same resolution web.ClientIP does, kept here so wire.go can
// log a failed push without importing the whole middleware chain.
func clientIPOf(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	return r.RemoteAddr
}
