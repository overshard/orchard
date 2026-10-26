package main

import (
	"testing"
	"time"
)

func TestIPLimiter(t *testing.T) {
	var l ipLimiter
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for i := range collectBurst {
		if !l.allow("203.0.113.1", now) {
			t.Fatalf("refused event %d inside the burst", i+1)
		}
	}
	if l.allow("203.0.113.1", now) {
		t.Error("allowed an event past the burst")
	}
	if !l.allow("203.0.113.2", now) {
		t.Error("one address used up another's bucket")
	}
	if !l.allow("203.0.113.1", now.Add(2*time.Second)) {
		t.Error("no token back after two seconds at thirty a minute")
	}
	l.allow("203.0.113.3", now.Add(5*time.Minute))
	if len(l.buckets) != 1 {
		t.Errorf("%d buckets after a quiet five minutes, want only the new one", len(l.buckets))
	}
}
