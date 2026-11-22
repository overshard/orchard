package main

import (
	"context"
	"testing"
	"time"

	"logging.bythewood.me/web"
)

// A backup pings once a day, so the five minute window would page all day and
// the source would age out of Bootstrap's window before it went quiet.
func TestHeartbeatSourceGetsItsOwnWindow(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)
	w, fired := testWatchdog(t, db, &clock)

	w.Observe("backup", []web.Record{{Level: "INFO", Msg: "backup finished"}})
	clock = clock.Add(25 * time.Hour)
	w.checkSilence()
	if len(*fired) != 0 {
		t.Fatalf("backup alerted after 25h: %v", *fired)
	}
	clock = clock.Add(2 * time.Hour)
	w.checkSilence()
	if len(*fired) != 1 || (*fired)[0].kind != "silence" {
		t.Fatalf("alerts = %v, want one silence", *fired)
	}
	msg, _ := renderAlert("silence", (*fired)[0].ctx)
	if msg.Title != "backup has not checked in" {
		t.Errorf("title = %q", msg.Title)
	}
}

func TestBootstrapSeedsHeartbeatFromItsLastRollup(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	w, fired := testWatchdog(t, db, &clock)

	hour := hourFloor(clock.Add(-30 * time.Hour).UnixMilli())
	if _, err := db.Exec(`INSERT INTO rollups (hour, source, level, component, status, count)
		VALUES (?,'backup','INFO','',0,1)`, hour); err != nil {
		t.Fatal(err)
	}
	if err := w.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.checkSilence()
	if len(*fired) != 1 || (*fired)[0].ctx.Source != "backup" {
		t.Fatalf("alerts = %v, want backup silent straight after the restart", *fired)
	}
}

func TestErrorBurstFiresOncePerHour(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 11, 21, 1, 1, 0, 0, time.UTC)
	w, fired := testWatchdog(t, db, &clock)

	errs := func(n int) {
		for range n {
			w.Observe("llm", []web.Record{{Level: "ERROR", Msg: "database or disk is full"}})
			clock = clock.Add(time.Second)
		}
	}

	errs(errorBurst - 1)
	w.checkErrors()
	if len(*fired) != 0 {
		t.Fatalf("fired under the burst size: %v", *fired)
	}
	errs(1)
	w.checkErrors()
	if len(*fired) != 1 || (*fired)[0].kind != "errors" || (*fired)[0].ctx.Source != "llm" {
		t.Fatalf("alerts = %v, want one errors for llm", *fired)
	}

	errs(errorBurst)
	w.checkErrors()
	if len(*fired) != 1 {
		t.Fatalf("re-fired inside the hour: %d alerts", len(*fired))
	}

	clock = clock.Add(errorRepeat)
	errs(errorBurst)
	w.checkErrors()
	if len(*fired) != 2 {
		t.Fatalf("alerts after an hour = %d, want 2", len(*fired))
	}
}

// Errors spread thinner than the window are a rate, which isn't a rule here.
func TestSpreadOutErrorsDoNotFire(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 11, 21, 1, 0, 0, 0, time.UTC)
	w, fired := testWatchdog(t, db, &clock)

	for range errorBurst * 3 {
		w.Observe("gate", []web.Record{{Level: "ERROR", Msg: "flush failed"}})
		clock = clock.Add(time.Minute)
		w.checkErrors()
	}
	if len(*fired) != 0 {
		t.Fatalf("fired on one error a minute: %v", *fired)
	}
}

func TestDiskLowFiresOnceAndRearms(t *testing.T) {
	db := testDB(t)
	clock := time.Date(2026, 11, 21, 1, 0, 0, 0, time.UTC)
	w, fired := testWatchdog(t, db, &clock)

	var free uint64 = 20 << 30
	const total = 80 << 30
	w.diskPath = "/data"
	w.statDisk = func(string) (uint64, uint64, error) { return free, total, nil }

	w.checkDisk()
	if len(*fired) != 0 {
		t.Fatalf("fired at 25%% free: %v", *fired)
	}
	free = 10 << 30
	w.checkDisk()
	w.checkDisk()
	if len(*fired) != 1 || (*fired)[0].kind != "disk" {
		t.Fatalf("alerts = %v, want one disk", *fired)
	}
	free = 20 << 30
	w.checkDisk()
	free = 2 << 30
	w.checkDisk()
	if len(*fired) != 2 {
		t.Fatalf("alerts = %d after recovering and dropping again, want 2", len(*fired))
	}
}

func TestDiskIsLow(t *testing.T) {
	cases := []struct {
		free, total uint64
		want        bool
	}{
		{20 << 30, 80 << 30, false},
		{11 << 30, 80 << 30, true},
		{2 << 30, 10 << 30, true},
		{4 << 30, 10 << 30, false},
	}
	for _, c := range cases {
		if got := diskIsLow(c.free, c.total); got != c.want {
			t.Errorf("diskIsLow(%d, %d) = %v, want %v", c.free, c.total, got, c.want)
		}
	}
}
