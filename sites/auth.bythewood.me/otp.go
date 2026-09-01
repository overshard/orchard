package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"
)

const (
	pendingCookie = "bw_pending"

	codeTTL = 10 * time.Minute
	// Wrong codes against one outstanding six digit value. Five tries out of a
	// million, inside ten minutes, with the row destroyed at the fifth.
	maxAttempts = 5

	// The ceiling that actually bounds the damage, because it counts published
	// notifications for the account and ignores where the request came from.
	// Per-IP limiting is bypassed by sending one request from each of a
	// thousand proxies; this is what still holds when that happens.
	sendCeiling = 5
	sendWindow  = time.Hour

	// A speed bump on top, not the limit: the sleep is per goroutine, so
	// concurrent attempts all serve it at once.
	failedDelay = 500 * time.Millisecond
)

// One global bucket rather than per-IP state, which grows with every prober,
// and there is exactly one legitimate user.
var (
	loginBucket = &tokenBucket{tokens: 5, burst: 5, refill: 2 * time.Second, last: time.Now()}
	codeBucket  = &tokenBucket{tokens: 10, burst: 10, refill: time.Second, last: time.Now()}
)

type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	burst  float64
	refill time.Duration
	last   time.Time
}

func (b *tokenBucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() / b.refill.Seconds()
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

var (
	errCeiling     = errors.New("too many codes have been sent recently")
	errOutstanding = errors.New("a code is already outstanding")
	errNoPending   = errors.New("that code has expired, start again")
	errBadCode     = errors.New("that code is not right")
)

// pendingBrowser reads the cookie that binds an outstanding code to the browser
// that asked for it.
func pendingBrowser(r *http.Request) string {
	c, err := r.Cookie(pendingCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

func writePendingCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:  pendingCookie,
		Value: value,
		Path:  "/",
		// Host-only. This one never leaves auth.bythewood.me, so it takes no
		// Domain and is not sent anywhere else.
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
		Secure:   true,
		MaxAge:   int(codeTTL / time.Second),
	})
}

func clearPendingCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     pendingCookie,
		Value:    "",
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
		Secure:   true,
		MaxAge:   -1,
	})
}

// livePending returns the one unconsumed, unexpired row, if there is one.
func livePending(db *sql.DB) (id int64, browser []byte, ok bool, err error) {
	err = db.QueryRow(`
        SELECT id, browser_hash FROM pending_logins
        WHERE consumed = 0 AND expires > ?
        ORDER BY id DESC LIMIT 1`, time.Now().Unix()).Scan(&id, &browser)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, err
	}
	return id, browser, true, nil
}

// sendsInWindow counts what has actually been published lately.
func sendsInWindow(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM sends WHERE ts > ?`,
		time.Now().Add(-sendWindow).Unix()).Scan(&n)
	return n, err
}

// startLogin mints a code and stores it, returning the code to publish and the
// browser token to set as a cookie.
//
// It publishes nothing itself. The caller does that, and only records the send
// once ntfy accepted it, so a failed publish does not spend the ceiling.
func startLogin(db *sql.DB, r *http.Request) (code, browser string, err error) {
	// One outstanding at a time, for the account rather than per browser. A
	// repeat request inside the window publishes nothing, which is what
	// collapses a flood of requests into one notification per window.
	if _, _, ok, err := livePending(db); err != nil {
		return "", "", err
	} else if ok {
		return "", "", errOutstanding
	}

	n, err := sendsInWindow(db)
	if err != nil {
		return "", "", err
	}
	if n >= sendCeiling {
		return "", "", errCeiling
	}

	if code, err = newCode(); err != nil {
		return "", "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	browser = base64.RawURLEncoding.EncodeToString(raw)

	hash, salt, err := hashSecret(code)
	if err != nil {
		return "", "", err
	}

	now := time.Now()
	c := requestContext(r)
	_, err = db.Exec(`
        INSERT INTO pending_logins
            (code_hash, code_salt, browser_hash, created, expires, ip, country, city, ua)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hash, salt, sessionHash(browser), now.Unix(), now.Add(codeTTL).Unix(),
		c.IP, c.Country, c.City, c.UA)
	if err != nil {
		return "", "", err
	}
	return code, browser, nil
}

// recordSend spends one of the hour's notifications. Called after ntfy accepted
// the publish, never before.
func recordSend(db *sql.DB) error {
	_, err := db.Exec(`INSERT INTO sends (ts) VALUES (?)`, time.Now().Unix())
	return err
}

// finishLogin checks a code against the outstanding row for this browser. A
// code pushed to the phone is worthless in a browser that did not ask for it,
// which is what stops somebody who can see the notification from using it.
func finishLogin(db *sql.DB, r *http.Request, code string) error {
	browser := pendingBrowser(r)
	if browser == "" {
		return errNoPending
	}

	var (
		id         int64
		hash, salt []byte
		attempts   int
	)
	err := db.QueryRow(`
        SELECT id, code_hash, code_salt, attempts FROM pending_logins
        WHERE consumed = 0 AND expires > ? AND browser_hash = ?
        ORDER BY id DESC LIMIT 1`,
		time.Now().Unix(), sessionHash(browser)).Scan(&id, &hash, &salt, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return errNoPending
	}
	if err != nil {
		return err
	}

	if attempts+1 >= maxAttempts {
		// Burn the row on the last attempt whether or not this one is right, so
		// a wrong fifth guess cannot be followed by a sixth.
		defer func() { _, _ = db.Exec(`UPDATE pending_logins SET consumed = 1 WHERE id = ?`, id) }()
	}
	if _, err := db.Exec(`UPDATE pending_logins SET attempts = attempts + 1 WHERE id = ?`, id); err != nil {
		return err
	}

	if !secretMatches(code, hash, salt) {
		return errBadCode
	}

	_, err = db.Exec(`UPDATE pending_logins SET consumed = 1 WHERE id = ?`, id)
	return err
}

// sweepPending drops spent and expired rows, and the send counters that have
// aged past the window they are counted in.
func sweepPending(db *sql.DB) error {
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	if _, err := db.Exec(`DELETE FROM pending_logins WHERE expires < ?`, cutoff); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM sends WHERE ts < ?`, cutoff)
	return err
}
