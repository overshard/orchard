package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"status.bythewood.me/ntfy"
)

func insertCheck(t *testing.T, db *sql.DB, id uuid.UUID, code int64, at time.Time) {
	t.Helper()
	ttfb := int64(90)
	if _, err := db.Exec(
		`INSERT INTO checks (property_id, status_code, response_ms, headers, created_at, dns_ms, tcp_ms, tls_ms, ttfb_ms)
		 VALUES (?, ?, 120, '{}', ?, 3, 10, 20, ?)`, id[:], code, at.UnixMilli(), ttfb); err != nil {
		t.Fatal(err)
	}
}

// Two failures open an incident from the first of them, each further failure
// counts, the first 200 closes it, and the push result lands on the row.
func TestIncidentFollowsTheAlertState(t *testing.T) {
	db, id := freshDB(t)
	ctx := context.Background()
	notifier := &Notifier{client: &ntfy.Client{HTTP: http.DefaultClient, Base: "http://127.0.0.1:1", Topic: "test"}}
	p, _ := getProperty(ctx, db, id)

	base := time.Now().Add(-time.Hour)
	for i, code := range []int64{502, 502, 502, 200} {
		at := base.Add(time.Duration(i) * 3 * time.Minute)
		insertCheck(t, db, id, code, at)
		if err := advanceAlertState(ctx, db, notifier, p, code); err != nil {
			t.Fatal(err)
		}
	}
	notifier.Wait()

	var start, down int64
	var end sql.NullInt64
	var code, fails int64
	var pushedDown, pushedUp sql.NullInt64
	if err := db.QueryRow(`SELECT started_at, down_at, ended_at, first_code, failed_checks, pushed_down, pushed_up
		FROM incidents WHERE property_id = ?`, id[:]).Scan(&start, &down, &end, &code, &fails, &pushedDown, &pushedUp); err != nil {
		t.Fatal(err)
	}
	if start != base.UnixMilli() || down != base.Add(3*time.Minute).UnixMilli() {
		t.Errorf("incident started %d and went down %d, want the first and second failed checks", start, down)
	}
	if code != 502 || fails != 3 || !end.Valid {
		t.Errorf("got code %d, %d fails, ended %v, want 502, 3 and closed", code, fails, end.Valid)
	}
	// ntfy is unreachable here, so both pushes are recorded as failed and not unknown.
	if !pushedDown.Valid || pushedDown.Int64 != 0 || !pushedUp.Valid || pushedUp.Int64 != 0 {
		t.Errorf("push results were %v and %v, want both recorded as failed", pushedDown, pushedUp)
	}
}

func TestBackfillIncidentsOnce(t *testing.T) {
	db, id := freshDB(t)
	ctx := context.Background()
	base := time.Now().Add(-2 * time.Hour)
	for i, code := range []int64{200, 503, 503, 200, 503, 200, 502, 502} {
		insertCheck(t, db, id, code, base.Add(time.Duration(i)*3*time.Minute))
	}
	for range 2 {
		if err := backfillIncidents(ctx, db); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.Query("SELECT first_code, failed_checks, ended_at IS NULL, backfilled FROM incidents ORDER BY started_at")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type got struct {
		code, fails int64
		open, back  bool
	}
	var all []got
	for rows.Next() {
		var g got
		if err := rows.Scan(&g.code, &g.fails, &g.open, &g.back); err != nil {
			t.Fatal(err)
		}
		all = append(all, g)
	}
	want := []got{{503, 2, false, true}, {502, 2, true, true}}
	if len(all) != len(want) {
		t.Fatalf("got %d incidents %v, want %v (a lone failure is not one, and a second run adds nothing)", len(all), all, want)
	}
	for i := range want {
		if all[i] != want[i] {
			t.Errorf("incident %d is %+v, want %+v", i, all[i], want[i])
		}
	}
}

// Retention deletes a day's oldest checks while it's still being rolled up, and
// the row has to keep the fuller count it already had.
func TestRollupNeverShrinksADay(t *testing.T) {
	db, id := freshDB(t)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-48 * time.Hour)
	for i := range 10 {
		code := int64(200)
		if i == 4 {
			code = 502
		}
		insertCheck(t, db, id, code, day.Add(time.Duration(i)*time.Hour))
	}
	if err := rollupDays(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM checks WHERE created_at < ?", day.Add(5*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := rollupDays(ctx, db); err != nil {
		t.Fatal(err)
	}

	var checks, failed int64
	var p50 sql.NullInt64
	var hours string
	if err := db.QueryRow("SELECT checks, failed, p50_ms, hours FROM days WHERE property_id = ? AND day = ?",
		id[:], day.Format(dayLayout)).Scan(&checks, &failed, &p50, &hours); err != nil {
		t.Fatal(err)
	}
	if checks != 10 || failed != 1 {
		t.Errorf("day has %d checks and %d failed, want 10 and 1 kept after the delete", checks, failed)
	}
	if !p50.Valid || p50.Int64 != 120 {
		t.Errorf("p50 is %v, want 120", p50)
	}
	if !strings.HasPrefix(hours, "[[1,0,120],[1,0,120],[1,0,120],[1,0,120],[1,1,null]") {
		t.Errorf("hours start %q, want one check an hour with hour four failed", hours[:min(len(hours), 60)])
	}
}

func TestQuantileIsNearestRank(t *testing.T) {
	vals := []int64{5, 1, 4, 2, 3, 6, 7, 8, 9, 10}
	if q := quantile(vals, 0.5); *q != 5 {
		t.Errorf("p50 is %d, want 5", *q)
	}
	if q := quantile(vals, 0.95); *q != 10 {
		t.Errorf("p95 is %d, want 10", *q)
	}
	if quantile(nil, 0.5) != nil {
		t.Error("no values should be nil and not zero")
	}
}

// A day an outage touched is red, a day with a lone failure is amber, and a day
// with no rollup row says why instead of reading as fine.
func TestHistoryDayKinds(t *testing.T) {
	db, id := freshDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	yday := today.Add(-24 * time.Hour)
	for i := range 20 {
		code := int64(200)
		if i == 3 {
			code = 502
		}
		insertCheck(t, db, id, code, yday.Add(time.Duration(i)*time.Hour))
	}
	insertCheck(t, db, id, 200, today.Add(time.Minute))
	if err := rollupDays(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO incidents (property_id, started_at, down_at, ended_at, first_code, failed_checks)
		VALUES (?, ?, ?, ?, 530, 4)`, id[:], today.Add(time.Minute).UnixMilli(), today.Add(4*time.Minute).UnixMilli(),
		today.Add(13*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE properties SET created_at = ?", today.AddDate(0, 0, -5).UnixMilli()); err != nil {
		t.Fatal(err)
	}

	p, _ := getProperty(ctx, db, id)
	hs, err := loadHistories(ctx, db, []*Property{p}, now)
	if err != nil {
		t.Fatal(err)
	}
	h := hs[id]
	if len(h.Days) != historyDays {
		t.Fatalf("got %d days, want %d", len(h.Days), historyDays)
	}
	last, prev := h.Days[len(h.Days)-1], h.Days[len(h.Days)-2]
	if last.Kind != "bad" || !strings.Contains(last.Tip, "530 from") {
		t.Errorf("today is %q with tip %q, want red naming the 530", last.Kind, last.Tip)
	}
	if prev.Kind != "warn" {
		t.Errorf("yesterday is %q, want amber for one failure in twenty", prev.Kind)
	}
	gap := h.Days[len(h.Days)-3]
	if gap.Kind != "none" || !strings.Contains(gap.Tip, "rollup started") {
		t.Errorf("a day with no row is %q %q, want none saying when the rollup started", gap.Kind, gap.Tip)
	}
	if old := h.Days[0]; !strings.Contains(old.Tip, "not watched yet") {
		t.Errorf("a day before the property existed says %q", old.Tip)
	}
	if h.Up30 == nil || *h.Up30 > 99.9 || h.Inc30 != 1 || h.BudgetLeft == nil {
		t.Errorf("30d uptime %v, %d incidents, budget %v", h.Up30, h.Inc30, h.BudgetLeft)
	}
}
