package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A scheduled run's calls are marked background and a forced one's aren't, and
// the gateway turning one away stops the run for as long as it said.
func TestModelMarksScheduledCallsAndStopsWhenTurnedAway(t *testing.T) {
	var marked []string
	busy := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		marked = append(marked, r.Header.Get("X-Background"))
		if busy {
			w.Header().Set("Retry-After", "600")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"message":"the desktop is using the card: card full","type":"card_busy"}}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	m := NewModel(srv.URL, "key")
	var out map[string]any

	if err := m.Structured(context.Background(), "s", "u", nil, 10, &out); err != nil {
		t.Fatal(err)
	}
	ctx, gate := scheduledRun(context.Background())
	if err := m.Structured(ctx, "s", "u", nil, 10, &out); err != nil {
		t.Fatal(err)
	}
	if marked[0] != "" || marked[1] != "1" {
		t.Errorf("background header %q", marked)
	}

	busy = true
	err := m.Structured(ctx, "s", "u", nil, 10, &out)
	var cb *CardBusy
	if !errors.As(err, &cb) || gate.Busy() == nil || ctx.Err() == nil {
		t.Fatalf("err %v, gate %v, ctx %v", err, gate.Busy(), ctx.Err())
	}
	if left := time.Until(cb.Until); left < 9*time.Minute || left > 11*time.Minute {
		t.Errorf("until %s from now", left)
	}
}

// A 503 that isn't the gateway's card_busy is an ordinary failure, retried in a
// quarter hour, and doesn't stop the run.
func TestModelOtherUnavailableIsAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":{"message":"loading model","type":"unavailable_error"}}`)
	}))
	defer srv.Close()
	ctx, gate := scheduledRun(context.Background())
	var out map[string]any
	err := NewModel(srv.URL, "key").Structured(ctx, "s", "u", nil, 10, &out)
	var cb *CardBusy
	if err == nil || errors.As(err, &cb) || gate.Busy() != nil {
		t.Errorf("err %v, gate %v", err, gate.Busy())
	}
}
