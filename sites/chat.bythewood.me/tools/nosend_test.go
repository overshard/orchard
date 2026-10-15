package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Once a pool is spent no request leaves, since a limit that still sends the call
// and throws the answer away isn't a limit.
func TestSpentPoolSendsNothing(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	d := &Deps{HTTP: srv.Client(), Public: srv.Client(), Now: time.Now,
		Guard: NewGuard(time.Minute), Budgets: NewBudgets()}
	// A tiny pool on this test's host.
	host := hostOf(srv.URL)
	budgets[host] = budget{gap: 0, minute: 2, hour: 2, day: 2}
	defer delete(budgets, host)

	for i := 0; i < 2; i++ {
		if _, err := get(context.Background(), d, srv.URL, ""); err != nil {
			t.Fatalf("call %d refused early: %v", i+1, err)
		}
	}
	before := atomic.LoadInt32(&hits)
	for i := 0; i < 5; i++ {
		if _, err := get(context.Background(), d, srv.URL, ""); err == nil {
			t.Fatal("a spent pool still allowed a call")
		}
	}
	if got := atomic.LoadInt32(&hits); got != before {
		t.Errorf("%d requests went out after the pool was spent", got-before)
	}
}

// A host that refused isn't called again either.
func TestBoxedHostSendsNothing(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := &Deps{HTTP: srv.Client(), Public: srv.Client(), Now: time.Now,
		Guard: NewGuard(time.Minute), Budgets: NewBudgets()}
	if _, err := get(context.Background(), d, srv.URL, ""); err == nil {
		t.Fatal("a 202 was not treated as a refusal")
	}
	after := atomic.LoadInt32(&hits)
	for i := 0; i < 5; i++ {
		_, _ = get(context.Background(), d, srv.URL, "")
	}
	if got := atomic.LoadInt32(&hits); got != after {
		t.Errorf("%d requests were sent to a host that already refused", got-after)
	}
}
