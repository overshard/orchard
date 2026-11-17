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
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	d := &Deps{HTTP: srv.Client(), Public: srv.Client(), Now: time.Now,
		Guard: NewGuard(time.Minute), Budgets: NewBudgets()}
	// A tiny pool on this test's host.
	host := hostOf(srv.URL)
	budgets[host] = budget{gap: 0, minute: 2, hour: 2, day: 2}
	defer delete(budgets, host)

	for i := range 2 {
		if _, err := get(context.Background(), d, srv.URL, ""); err != nil {
			t.Fatalf("call %d refused early: %v", i+1, err)
		}
	}
	before := hits.Load()
	for range 5 {
		if _, err := get(context.Background(), d, srv.URL, ""); err == nil {
			t.Fatal("a spent pool still allowed a call")
		}
	}
	if got := hits.Load(); got != before {
		t.Errorf("%d requests went out after the pool was spent", got-before)
	}
}

// A host that refused isn't called again either.
func TestBoxedHostSendsNothing(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := &Deps{HTTP: srv.Client(), Public: srv.Client(), Now: time.Now,
		Guard: NewGuard(time.Minute), Budgets: NewBudgets()}
	if _, err := get(context.Background(), d, srv.URL, ""); err == nil {
		t.Fatal("a 202 was not treated as a refusal")
	}
	after := hits.Load()
	for range 5 {
		_, _ = get(context.Background(), d, srv.URL, "")
	}
	if got := hits.Load(); got != after {
		t.Errorf("%d requests were sent to a host that already refused", got-after)
	}
}
