package main

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// Alert is one surge or link push, kept so the dashboard can mark it on the
// chart and the watcher remembers its cooldowns across a deploy.
type Alert struct {
	Kind      string
	Title     string
	Body      string
	CreatedAt int64
}

func (a Alert) When() string {
	t := time.UnixMilli(a.CreatedAt).In(eastern)
	if now := time.Now().In(eastern); t.Format("2006-01-02") == now.Format("2006-01-02") {
		return t.Format("15:04")
	}
	return formatGraphLabel(t)
}

func recordAlert(ctx context.Context, db *sql.DB, id uuid.UUID, kind, key, title, body string, at time.Time) error {
	_, err := db.ExecContext(ctx, `INSERT INTO alerts (property_id, kind, key, title, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id[:], kind, key, title, body, at.UnixMilli())
	return err
}

// readAlerts is a property's pushes between two times, newest first.
func readAlerts(ctx context.Context, db *sql.DB, id uuid.UUID, since, until int64, limit int) []Alert {
	rows, err := db.QueryContext(ctx, `SELECT kind, title, body, created_at FROM alerts
		WHERE property_id = ? AND created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC LIMIT ?`, id[:], since, until, limit)
	logQuery("alerts", err)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		if rows.Scan(&a.Kind, &a.Title, &a.Body, &a.CreatedAt) == nil {
			out = append(out, a)
		}
	}
	logQuery("alerts", rows.Err())
	return out
}

// recentAlertKeys is when each cooldown key last pushed, for a watcher that
// just started.
func recentAlertKeys(ctx context.Context, db *sql.DB, since time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	rows, err := db.QueryContext(ctx, `SELECT key, MAX(created_at) FROM alerts WHERE created_at >= ? GROUP BY key`, since.UnixMilli())
	logQuery("alert_keys", err)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var at int64
		if rows.Scan(&k, &at) == nil {
			out[k] = time.UnixMilli(at)
		}
	}
	return out
}
