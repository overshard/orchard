package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildBox(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	var p aggregatePayload
	raw := `{"box":{"disk_free":11811160064,"disk_total":80000000000,"disk_low":false,
		"heartbeats":{"backup":` + jsonInt(now.Add(-17*time.Hour).UnixMilli()) + `}}}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	b := buildBox(p, now)
	if !b.Known || b.Disk != "11G FREE" || b.DiskState != "ok" || b.Backup != "17H AGO" || b.BackupState != "ok" {
		t.Errorf("box = %+v", b)
	}

	p.Box.DiskLow = true
	p.Box.Heartbeats["backup"] = now.Add(-30 * time.Hour).UnixMilli()
	if b := buildBox(p, now); b.DiskState != "bad" || b.BackupState != "bad" {
		t.Errorf("a low disk and a 30h old backup read %+v", b)
	}

	p.Box.Heartbeats = map[string]int64{}
	if b := buildBox(p, now); b.Backup != "NEVER" || b.BackupState != "bad" {
		t.Errorf("no heartbeat read %+v", b)
	}

	// A logging from before the box sends none of it, so the row stays hidden.
	if b := buildBox(aggregatePayload{}, now); b.Known {
		t.Errorf("an old logging reads as known: %+v", b)
	}
}

func jsonInt(n int64) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
