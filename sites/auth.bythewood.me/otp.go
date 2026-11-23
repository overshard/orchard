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

	// Wrong codes before codes stop working for a day. Five a code and five
	// codes an hour otherwise adds up to a real chance over a year of quiet
	// guessing. Recovery codes still work while this is tripped.
	dailyFailures = 10
	failureWindow = 24 * time.Hour
)

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

// outstanding reports whether a code is already out. A known browser is only
// held up by its own, so a stranger's code can't stand in the way of it.
func outstanding(db *sql.DB, r *http.Request, known bool) (bool, error) {
	q := `SELECT COUNT(*) FROM pending_logins WHERE consumed = 0 AND expires > ?`
	args := []any{time.Now().Unix()}
	if known {
		q += ` AND browser_hash = ?`
		args = append(args, sessionHash(pendingBrowser(r)))
	}
	var n int
	err := db.QueryRow(q, args...).Scan(&n)
	return n > 0, err
}

// countSince counts rows of sends or failures since a cutoff. An unknown
// browser counts every row, and a known one only those from known browsers.
func countSince(db *sql.DB, table string, since time.Time, known bool) (int, error) {
	q := `SELECT COUNT(*) FROM ` + table + ` WHERE ts > ?`
	if known {
		q += ` AND known = 1`
	}
	var n int
	err := db.QueryRow(q, since.Unix()).Scan(&n)
	return n, err
}

// sendsInWindow counts what has actually been published lately.
func sendsInWindow(db *sql.DB, known bool) (int, error) {
	return countSince(db, "sends", time.Now().Add(-sendWindow), known)
}

// startLogin mints and stores a code, returning it and the browser token for the
// cookie. It publishes nothing, and the caller records the send only once ntfy
// accepts it, so a failed publish doesn't spend the ceiling.
// startMu makes the checks and the insert one step, or two requests at once
// both see no outstanding code and both get one.
var startMu sync.Mutex

func startLogin(db *sql.DB, r *http.Request, known bool) (code, browser string, err error) {
	startMu.Lock()
	defer startMu.Unlock()

	// No point pushing a code that cannot be typed.
	if locked, err := lockedOut(db, known); err != nil {
		return "", "", err
	} else if locked {
		return "", "", errLockedOut
	}

	// One outstanding code for the account, not per browser, so a flood of
	// requests collapses into one notification per window.
	if out, err := outstanding(db, r, known); err != nil {
		return "", "", err
	} else if out {
		return "", "", errOutstanding
	}

	n, err := sendsInWindow(db, known)
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
func recordSend(db *sql.DB, known bool) error {
	_, err := db.Exec(`INSERT INTO sends (ts, known) VALUES (?, ?)`, time.Now().Unix(), known)
	return err
}

// finishLogin checks a code against the outstanding row for this browser, so
// somebody who can see the notification can't use it from a browser that didn't ask.
func finishLogin(db *sql.DB, r *http.Request, code string, known bool) error {
	browser := pendingBrowser(r)
	if browser == "" {
		return errNoPending
	}

	if locked, err := lockedOut(db, known); err != nil {
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
		_, _ = db.Exec(`INSERT INTO failures (ts, known) VALUES (?, ?)`, time.Now().Unix(), known)
		return errBadCode
	}

	_, err = db.Exec(`UPDATE pending_logins SET consumed = 1 WHERE id = ?`, id)
	return err
}

// failuresInWindow counts wrong codes in the last day.
func failuresInWindow(db *sql.DB, known bool) (int, error) {
	return countSince(db, "failures", time.Now().Add(-failureWindow), known)
}

func lockedOut(db *sql.DB, known bool) (bool, error) {
	n, err := failuresInWindow(db, known)
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
