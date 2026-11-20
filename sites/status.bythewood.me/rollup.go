package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

const dayLayout = "2006-01-02"

// dayAgg is one property's UTC day as it's being added up.
type dayAgg struct {
	checks, failed int64
	ok             []int64
	dns, tcp, tls  []int64
	ttfb           []int64
	hours          [24]struct {
		checks, failed int64
		ok             []int64
	}
}

// rollupDays writes a days row for every property and day still in checks. A
// row never shrinks, so a day whose oldest checks have been deleted keeps the
// fuller count it was written with.
func rollupDays(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx,
		`SELECT property_id, status_code, response_ms, dns_ms, tcp_ms, tls_ms, ttfb_ms, created_at
		 FROM checks`)
	if err != nil {
		return err
	}
	type key struct {
		id  string
		day string
	}
	aggs := map[key]*dayAgg{}
	for rows.Next() {
		var (
			id                  []byte
			code, ms, at        int64
			dns, tcp, tls, ttfb *int64
		)
		if err := rows.Scan(&id, &code, &ms, &dns, &tcp, &tls, &ttfb, &at); err != nil {
			rows.Close()
			return err
		}
		t := time.UnixMilli(at).UTC()
		k := key{string(id), t.Format(dayLayout)}
		a := aggs[k]
		if a == nil {
			a = &dayAgg{}
			aggs[k] = a
		}
		h := &a.hours[t.Hour()]
		a.checks++
		h.checks++
		if code != 200 {
			a.failed++
			h.failed++
			continue
		}
		a.ok = append(a.ok, ms)
		h.ok = append(h.ok, ms)
		for _, ph := range []struct {
			v   *int64
			dst *[]int64
		}{{dns, &a.dns}, {tcp, &a.tcp}, {tls, &a.tls}, {ttfb, &a.ttfb}} {
			if ph.v != nil {
				*ph.dst = append(*ph.dst, *ph.v)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for k, a := range aggs {
		hours := make([][3]any, 24)
		for i, h := range a.hours {
			hours[i] = [3]any{h.checks, h.failed, quantile(h.ok, 0.5)}
		}
		hoursJSON, _ := json.Marshal(hours)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO days (property_id, day, checks, failed, p50_ms, p95_ms, dns_ms, tcp_ms, tls_ms, ttfb_ms, hours)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (property_id, day) DO UPDATE SET
			   checks = excluded.checks, failed = excluded.failed,
			   p50_ms = excluded.p50_ms, p95_ms = excluded.p95_ms,
			   dns_ms = excluded.dns_ms, tcp_ms = excluded.tcp_ms,
			   tls_ms = excluded.tls_ms, ttfb_ms = excluded.ttfb_ms, hours = excluded.hours
			 WHERE excluded.checks >= days.checks`,
			[]byte(k.id), k.day, a.checks, a.failed,
			quantile(a.ok, 0.5), quantile(a.ok, 0.95),
			quantile(a.dns, 0.5), quantile(a.tcp, 0.5), quantile(a.tls, 0.5), quantile(a.ttfb, 0.5),
			string(hoursJSON)); err != nil {
			return fmt.Errorf("upsert day %s: %w", k.day, err)
		}
	}
	return tx.Commit()
}

// quantile is nearest rank, nil for no values so an all-failed hour stores
// NULL and not a made up zero.
func quantile(vals []int64, q float64) *int64 {
	if len(vals) == 0 {
		return nil
	}
	s := slices.Clone(vals)
	slices.Sort(s)
	i := int(float64(len(s))*q+0.5) - 1
	i = max(0, min(len(s)-1, i))
	return &s[i]
}

// backfillIncidents reads the outages the alert machine would have opened out of
// the checks still kept, once, the first time a database has an incidents table.
// A run of two or more failures is an incident and the first 200 after it ends it.
func backfillIncidents(ctx context.Context, db *sql.DB) error {
	var done string
	err := db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'incidents_backfilled'").Scan(&done)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}

	rows, err := db.QueryContext(ctx,
		"SELECT property_id, status_code, created_at FROM checks ORDER BY property_id, created_at")
	if err != nil {
		return err
	}
	type run struct {
		id                []byte
		start, down, code int64
		end               *int64
		n                 int64
	}
	var found []run
	var cur *run
	var lastID []byte
	for rows.Next() {
		var id []byte
		var code, at int64
		if err := rows.Scan(&id, &code, &at); err != nil {
			rows.Close()
			return err
		}
		if string(id) != string(lastID) {
			if cur != nil && cur.n >= 2 {
				found = append(found, *cur)
			}
			cur, lastID = nil, id
		}
		switch {
		case code != 200 && cur == nil:
			cur = &run{id: id, start: at, code: code, n: 1}
		case code != 200:
			if cur.n == 1 {
				cur.down = at
			}
			cur.n++
		case cur != nil:
			if cur.n >= 2 {
				cur.end = &at
				found = append(found, *cur)
			}
			cur = nil
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if cur != nil && cur.n >= 2 {
		found = append(found, *cur)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, r := range found {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO incidents (property_id, started_at, down_at, ended_at, first_code, failed_checks, backfilled)
			 VALUES (?, ?, ?, ?, ?, ?, 1)`, r.id, r.start, r.down, r.end, r.code, r.n); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES ('incidents_backfilled', ?)",
		time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}
