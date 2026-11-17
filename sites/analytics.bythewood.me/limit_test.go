package main

import (
	"fmt"
	"testing"
	"time"
)

func TestClientLimiter(t *testing.T) {
	l := newClientLimiter()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for i := range 60 {
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

func TestPropertyLimiter(t *testing.T) {
	l := newPropertyLimiter()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for i := range 200 {
		if !l.allow("prop", now) {
			t.Fatalf("refused event %d inside the burst", i+1)
		}
	}
	if l.allow("prop", now) {
		t.Error("allowed an event past the burst")
	}
	for i := range 20 {
		if !l.allow("prop", now.Add(time.Second)) {
			t.Fatalf("refused event %d of the twenty a second refills", i+1)
		}
	}
	if l.allow("prop", now.Add(time.Second)) {
		t.Error("allowed more than twenty in a second past the burst")
	}
}

// A flood of fresh keys all mid burst can't grow the map past its cap, and once
// they go quiet a new key gets in again.
func TestLimiterKeyCap(t *testing.T) {
	l := &limiter{burst: 2, perMinute: 1, maxKeys: 3}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for i := range 10 {
		l.allow(fmt.Sprintf("k%d", i), now)
	}
	if len(l.buckets) != 3 {
		t.Errorf("%d buckets, want the cap of 3", len(l.buckets))
	}
	if l.allow("new", now) {
		t.Error("let a new key in while the map was full")
	}
	if !l.allow("new", now.Add(2*time.Minute)) {
		t.Error("refused a new key after the others refilled")
	}
}

func TestClientKey(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"203.0.113.7", "203.0.113.7"},
		{"::ffff:203.0.113.7", "203.0.113.7"},
		{"2001:db8:1:2:aaaa::1", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:ffff:ffff:ffff:ffff", "2001:db8:1:2::/64"},
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"not an ip", "not an ip"},
	} {
		if got := clientKey(tt.in); got != tt.want {
			t.Errorf("clientKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
