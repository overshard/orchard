package main

// Two authentication surfaces: a signed session cookie for the browser UI, in
// this file, and HTTP Basic carrying a token for the git wire, in wire.go and
// db.go. Basic exists on the wire because git's credential subsystem wants it.

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

	// The sleep is per goroutine, so N concurrent attempts all serve their
	// 500ms at once. loginBurst below is the real limit.
	failedLoginDelay = 500 * time.Millisecond

	// One global bucket rather than per-IP state; there is one legitimate user.
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

// sessionKey derives the cookie signing key from the password, so rotating the
// password invalidates every outstanding session.
func sessionKey(password string) []byte {
	sum := sha512.Sum512(append([]byte("repos-cookie:"), password...))
	return sum[:]
}

// issueSession writes the signed cookie, payload "1:<unix expiry>". SameSite=Strict
// stands in for CSRF tokens, and every state-changing route here is a POST.
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
		// Secure even though this process speaks plaintext on the bridge: the
		// browser only ever sees this cookie over the tunnel.
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

// validSession checks the signature before the expiry, since an expiry read off
// an unverified cookie is a number the client chose.
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

// checkPassword compares in constant time; rate limiting is the caller's job.
func checkPassword(given, want string) bool {
	return subtle.ConstantTimeCompare([]byte(given), []byte(want)) == 1
}

// userKey is the context key for the token label a push authenticated as.
type userKey struct{}

func withUser(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, userKey{}, label)
}

// userFrom reads the token label back out; empty means the request was unauthenticated.
func userFrom(ctx context.Context) string {
	label, _ := ctx.Value(userKey{}).(string)
	return label
}

// clientIPOf duplicates web.ClientIP so the wire can log without the middleware chain.
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
