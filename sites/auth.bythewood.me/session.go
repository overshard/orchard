package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"time"
)

const (
	sessionCookie = "bw_session"
	sessionTTL    = 30 * 24 * time.Hour

	// How long after proving possession of the phone a session may still change
	// the credentials. Long enough to finish what the login was for, short
	// enough that a cookie stolen later cannot rotate the recovery codes.
	sudoWindow = 10 * time.Minute

	// last_seen is a display field, so writing it on every request would cost a
	// database write per page load to move a number a person reads once.
	lastSeenResolution = 5 * time.Minute
)

// cookieDomain is what makes one login cover every bythewood.me host. Empty
// leaves the cookie host-only, which is what development on localhost needs,
// since a Domain of .bythewood.me is simply not sent to localhost and the login
// would appear to succeed and then not stick.
func cookieDomain() string { return os.Getenv("SESSION_DOMAIN") }

// Session is one row of the list on the sessions page.
type Session struct {
	ID       int64
	Created  time.Time
	LastSeen time.Time
	Expires  time.Time
	IP       string
	Country  string
	City     string
	UA       string
	Current  bool
}

var errNoSession = errors.New("no live session")

// sessionHash is what the database holds. A leaked copy of this file is then a
// list of when somebody logged in and from where, and not a set of cookies
// somebody can paste into a browser.
func sessionHash(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

// newSession mints an opaque identifier, records it, and returns the value for
// the cookie. The value is never stored and cannot be recovered.
func newSession(db *sql.DB, r *http.Request) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(raw)

	now := time.Now()
	c := requestContext(r)
	_, err := db.Exec(`
        INSERT INTO sessions (hash, created, last_seen, expires, sudo_at, ip, country, city, ua, cf_ray)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionHash(value), now.Unix(), now.Unix(), now.Add(sessionTTL).Unix(), now.Unix(),
		c.IP, c.Country, c.City, c.UA, c.Ray)
	if err != nil {
		return "", err
	}
	return value, nil
}

func writeSessionCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookie,
		Value:  value,
		Path:   "/",
		Domain: cookieDomain(),
		// SameSite=Strict rather than a CSRF token: every state-changing route
		// here is a POST, and a subdomain of the same registrable domain is
		// same-site, so moving between the sites keeps the cookie.
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
		Secure:   true,
		MaxAge:   int(sessionTTL / time.Second),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		Domain:   cookieDomain(),
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
		Secure:   true,
		MaxAge:   -1,
	})
}

// live holds what a validated session can answer without a second query.
type live struct {
	ID     int64
	Hash   []byte
	SudoAt time.Time
}

// lookupSession validates the cookie against the table. Expiry is checked in
// SQL so a row that has aged out is never returned, revoked or not.
func lookupSession(db *sql.DB, r *http.Request) (live, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return live{}, errNoSession
	}
	h := sessionHash(c.Value)

	var (
		id       int64
		sudoAt   int64
		lastSeen int64
	)
	err = db.QueryRow(`
        SELECT id, sudo_at, last_seen FROM sessions
        WHERE hash = ? AND revoked = 0 AND expires > ?`,
		h, time.Now().Unix()).Scan(&id, &sudoAt, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return live{}, errNoSession
	}
	if err != nil {
		return live{}, err
	}

	now := time.Now()
	if now.Sub(time.Unix(lastSeen, 0)) > lastSeenResolution {
		// Best effort. A page must not fail because this write did.
		_, _ = db.Exec(`UPDATE sessions SET last_seen = ? WHERE id = ?`, now.Unix(), id)
	}

	return live{ID: id, Hash: h, SudoAt: time.Unix(sudoAt, 0)}, nil
}

// inSudo reports whether this session proved possession recently enough to
// change credentials.
func (l live) inSudo() bool { return time.Since(l.SudoAt) < sudoWindow }

// listSessions returns the live sessions, newest first, marking the caller's own.
func listSessions(db *sql.DB, current []byte) ([]Session, error) {
	rows, err := db.Query(`
        SELECT id, hash, created, last_seen, expires, ip, country, city, ua
        FROM sessions WHERE revoked = 0 AND expires > ?
        ORDER BY last_seen DESC`, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var (
			s                          Session
			hash                       []byte
			created, lastSeen, expires int64
		)
		if err := rows.Scan(&s.ID, &hash, &created, &lastSeen, &expires,
			&s.IP, &s.Country, &s.City, &s.UA); err != nil {
			return nil, err
		}
		s.Created = time.Unix(created, 0).UTC()
		s.LastSeen = time.Unix(lastSeen, 0).UTC()
		s.Expires = time.Unix(expires, 0).UTC()
		s.Current = len(hash) == len(current) && string(hash) == string(current)
		out = append(out, s)
	}
	return out, rows.Err()
}

func revokeSession(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE sessions SET revoked = 1 WHERE id = ?`, id)
	return err
}

// revokeOthers signs out everything but the caller. Sessions the other sites
// are holding die with it, because they validate against this table rather than
// carrying a signature of their own.
func revokeOthers(db *sql.DB, keep int64) (int64, error) {
	res, err := db.Exec(`UPDATE sessions SET revoked = 1 WHERE id != ? AND revoked = 0`, keep)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// sweepSessions drops rows that expired or were revoked long enough ago that
// nobody is going to ask about them. Without it the table only grows.
func sweepSessions(db *sql.DB) error {
	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
	_, err := db.Exec(`DELETE FROM sessions WHERE expires < ? OR (revoked = 1 AND last_seen < ?)`,
		time.Now().Unix(), cutoff)
	return err
}
