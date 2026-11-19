package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"
)

// Queries written against these stored kinds outlive the code that emits them,
// so adding a kind is cheap and renaming one isn't.
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
	evConnectorAdded  = "connector_added"
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

// audit writes the local row and ships the same fact to logging.bythewood.me
// through the slog tee. No code ever reaches here, expired or otherwise, since
// log retention outlives incident response by a long way.
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
// history, and this table is only for the activity page when logging is down.
func sweepEvents(db *sql.DB) error {
	_, err := db.Exec(`
        DELETE FROM events WHERE id NOT IN (
            SELECT id FROM events ORDER BY id DESC LIMIT 2000
        )`)
	return err
}

// eventStats is the last 30 days of the activity log as counts and a series a
// day each, oldest first, for the tiles over the table.
type eventStats struct {
	SignIns, Codes, Refused, Places   int
	SignInDays, CodeDays, RefusedDays []float64
	// Last is the newest events as tick classes, oldest first.
	Last []string
}

func activityStats(db *sql.DB, now time.Time, events []Event) eventStats {
	st := eventStats{SignInDays: make([]float64, 30), CodeDays: make([]float64, 30), RefusedDays: make([]float64, 30)}
	rows, err := db.Query(`SELECT ts, kind, country, city FROM events WHERE ts >= ?`, now.AddDate(0, 0, -30).Unix())
	if err != nil {
		return st
	}
	defer rows.Close()
	places := map[string]bool{}
	for rows.Next() {
		var ts int64
		var kind, country, city string
		if rows.Scan(&ts, &kind, &country, &city) != nil {
			continue
		}
		i := 29 - int(now.Sub(time.Unix(ts, 0)).Hours()/24)
		if i < 0 || i > 29 {
			continue
		}
		switch kind {
		case evLogin, evRecoveryUsed:
			st.SignIns++
			st.SignInDays[i]++
			if country != "" {
				places[city+"|"+country] = true
			}
		case evCodeRequested:
			st.Codes++
			st.CodeDays[i]++
		case evCodeFailed, evRecoveryFailed, evRateLimited, evCeilingHit:
			st.Refused++
			st.RefusedDays[i]++
		}
	}
	st.Places = len(places)
	for i := min(len(events), 40) - 1; i >= 0; i-- {
		switch eventClass(events[i].Kind) {
		case "is-warn":
			st.Last = append(st.Last, "warn")
		case "is-note":
			st.Last = append(st.Last, "")
		default:
			st.Last = append(st.Last, "none")
		}
	}
	return st
}
