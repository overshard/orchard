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

	// Published notifications for the account, whatever address asked, since
	// per-IP limits are bypassed by one request from each of a thousand proxies.
	sendCeiling = 5
	sendWindow  = time.Hour

	// A speed bump on top, not the limit: the sleep is per goroutine, so
	// concurrent attempts all serve it at once.
	failedDelay = 500 * time.Millisecond

	// Wrong codes across every row before codes stop working for a day. Five
	// a code and five codes an hour otherwise adds up to a real chance over a
	// year of quiet guessing. Recovery codes still work while this is tripped.
	dailyFailures = 10
	failureWindow = 24 * time.Hour
)

// One global bucket instead of per-IP state, which grows with every prober,
// and there's only one legitimate user.
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
	errLockedOut   = errors.New("too many wrong codes today")
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

// startLogin mints and stores a code, returning it and the browser token for the
// cookie. It publishes nothing, and the caller records the send only once ntfy
// accepts it, so a failed publish doesn't spend the ceiling.
func startLogin(db *sql.DB, r *http.Request) (code, browser string, err error) {
	// No point pushing a code that cannot be typed.
	if locked, err := lockedOut(db); err != nil {
		return "", "", err
	} else if locked {
		return "", "", errLockedOut
	}

	// One outstanding code for the account, not per browser, so a flood of
	// requests collapses into one notification per window.
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

// finishLogin checks a code against the outstanding row for this browser, so
// somebody who can see the notification can't use it from a browser that didn't ask.
func finishLogin(db *sql.DB, r *http.Request, code string) error {
	browser := pendingBrowser(r)
	if browser == "" {
		return errNoPending
	}

	if locked, err := lockedOut(db); err != nil {
		return err
	} else if locked {
		return errLockedOut
	}

	var (
		id         int64
		hash, salt []byte
	)
	err := db.QueryRow(`
        SELECT id, code_hash, code_salt FROM pending_logins
        WHERE consumed = 0 AND expires > ? AND browser_hash = ?
        ORDER BY id DESC LIMIT 1`,
		time.Now().Unix(), sessionHash(browser)).Scan(&id, &hash, &salt)
	if errors.Is(err, sql.ErrNoRows) {
		return errNoPending
	}
	if err != nil {
		return err
	}

	// Claim the attempt in the same statement that checks the limit, or
	// concurrent guesses all read the same count and all get checked.
	var attempts int
	err = db.QueryRow(`
        UPDATE pending_logins SET attempts = attempts + 1
        WHERE id = ? AND consumed = 0 AND attempts < ?
        RETURNING attempts`, id, maxAttempts).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return errNoPending
	}
	if err != nil {
		return err
	}

	if attempts >= maxAttempts {
		// Burn the row on the last attempt whether or not this one is right, so
		// a wrong fifth guess cannot be followed by a sixth.
		defer func() { _, _ = db.Exec(`UPDATE pending_logins SET consumed = 1 WHERE id = ?`, id) }()
	}

	if !secretMatches(code, hash, salt) {
		_, _ = db.Exec(`INSERT INTO failures (ts) VALUES (?)`, time.Now().Unix())
		return errBadCode
	}

	_, err = db.Exec(`UPDATE pending_logins SET consumed = 1 WHERE id = ?`, id)
	return err
}

// failuresInWindow counts wrong codes across every row in the last day.
func failuresInWindow(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM failures WHERE ts > ?`,
		time.Now().Add(-failureWindow).Unix()).Scan(&n)
	return n, err
}

func lockedOut(db *sql.DB) (bool, error) {
	n, err := failuresInWindow(db)
	return n >= dailyFailures, err
}

// sweepPending drops login rows, sends and failures a day after they stop mattering.
func sweepPending(db *sql.DB) error {
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	if _, err := db.Exec(`DELETE FROM pending_logins WHERE expires < ?`, cutoff); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM sends WHERE ts < ?`, cutoff); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM failures WHERE ts < ?`, cutoff)
	return err
}
