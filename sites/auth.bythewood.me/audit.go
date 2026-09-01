package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"
)

// The event vocabulary, fixed here rather than spelled at each call site. A
// query written against these outlives the code that emits them, so adding a
// kind is cheap and renaming one is not.
const (
	evCodeRequested   = "code_requested"
	evCodeSent        = "code_sent"
	evCodeFailed      = "code_failed"
	evCodeExpired     = "code_expired"
	evLogin           = "login"
	evLogout          = "logout"
	evSessionRevoked  = "session_revoked"
	evRecoveryUsed    = "recovery_used"
	evRecoveryFailed  = "recovery_failed"
	evRecoveryRotated = "recovery_rotated"
	evRateLimited     = "rate_limited"
	evCeilingHit      = "ceiling_hit"
	evUsernameChanged = "username_changed"
)

// Event is one row of the activity page.
type Event struct {
	TS      time.Time
	Kind    string
	IP      string
	Country string
	City    string
	UA      string
	Detail  string
}

// audit writes the local copy and ships the same fact to
// logging.bythewood.me through the slog tee.
//
// No code ever reaches here, expired or otherwise. Retention outlives incident
// response, and a login code in a log store with a year on it is a login code
// somebody can read next spring.
func audit(db *sql.DB, r *http.Request, kind, detail string) {
	c := requestContext(r)

	// Best effort. A login must not fail because the audit write did.
	_, err := db.Exec(`
        INSERT INTO events (ts, kind, ip, country, city, ua, cf_ray, detail)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().Unix(), kind, c.IP, c.Country, c.City, c.UA, c.Ray, detail)
	if err != nil {
		slog.Error("audit write failed", slog.String("component", "auth"), slog.Any("err", err))
	}

	attrs := []any{
		slog.String("component", "auth"),
		slog.String("event", kind),
		slog.String("ip", c.IP),
		slog.String("country", c.Country),
		slog.String("city", c.City),
	}
	if c.Ray != "" {
		attrs = append(attrs, slog.String("cf_ray", c.Ray))
	}
	if detail != "" {
		attrs = append(attrs, slog.String("detail", detail))
	}
	slog.Info("auth "+kind, attrs...)
}

func recentEvents(db *sql.DB, limit int) ([]Event, error) {
	rows, err := db.Query(`
        SELECT ts, kind, ip, country, city, ua, detail FROM events
        ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var (
			e  Event
			ts int64
		)
		if err := rows.Scan(&ts, &e.Kind, &e.IP, &e.Country, &e.City, &e.UA, &e.Detail); err != nil {
			return nil, err
		}
		e.TS = time.Unix(ts, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// sweepEvents keeps the local copy bounded. logging.bythewood.me holds the long
// history, this table is only what the activity page reads when logging is the
// thing that is down.
func sweepEvents(db *sql.DB) error {
	_, err := db.Exec(`
        DELETE FROM events WHERE id NOT IN (
            SELECT id FROM events ORDER BY id DESC LIMIT 2000
        )`)
	return err
}
