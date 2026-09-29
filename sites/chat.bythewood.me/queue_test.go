package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestQueueSerialises(t *testing.T) {
	q := NewQueue()
	var (
		mu      sync.Mutex
		running int
		peak    int
		order   []int
	)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			release, ok := q.Enter(context.Background(), nil)
			if !ok {
				t.Errorf("worker %d was refused", i)
				return
			}
			defer release()

			mu.Lock()
			running++
			if running > peak {
				peak = running
			}
			order = append(order, i)
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			running--
			mu.Unlock()
		}(i)
		// Stagger so the order is deterministic enough to assert on.
		time.Sleep(5 * time.Millisecond)
	}
	wg.Wait()

	if peak != 1 {
		t.Errorf("peak concurrency was %d, want 1", peak)
	}
	if len(order) != 5 {
		t.Errorf("only %d of 5 ran", len(order))
	}
}

func TestQueueGivesUpOnCancel(t *testing.T) {
	q := NewQueue()

	held, ok := q.Enter(context.Background(), nil)
	if !ok {
		t.Fatal("the first caller should run at once")
	}

	// Second joins and then leaves before its turn.
	ctx, cancel := context.WithCancel(context.Background())
	left := make(chan bool, 1)
	go func() {
		_, ok := q.Enter(ctx, nil)
		left <- ok
	}()
	time.Sleep(30 * time.Millisecond)

	if n, _ := q.Depth(); n != 1 {
		t.Fatalf("depth = %d, want 1 waiting", n)
	}
	cancel()
	if ok := <-left; ok {
		t.Error("a cancelled caller should not be given the slot")
	}

	// A third must still get through once the first releases.
	held()
	done := make(chan bool, 1)
	go func() {
		r, ok := q.Enter(context.Background(), nil)
		if ok {
			r()
		}
		done <- ok
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Error("the third caller was refused")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the queue stalled after a cancellation")
	}
}

func TestQueueReportsPosition(t *testing.T) {
	q := NewQueue()
	release, _ := q.Enter(context.Background(), nil)

	seen := make(chan QueueState, 4)
	go func() {
		r, ok := q.Enter(context.Background(), func(s QueueState) { seen <- s })
		if ok {
			r()
		}
	}()

	select {
	case s := <-seen:
		if s.Position != 1 || s.Ahead != 0 {
			t.Errorf("first waiter reported %+v, want position 1 ahead 0", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no position was reported")
	}
	release()
}

// A turn behind another is announced with its place in line, so every tab's
// list says it is waiting rather than working, and one stopped before it reached
// the card is still announced as over, or those lists say waiting forever.
func TestAQueuedTurnIsAnnouncedAndSoIsItsEnd(t *testing.T) {
	q := NewQueue()
	release, _ := q.Enter(context.Background(), nil)
	defer release()

	s := &site{runs: NewRuns(), queue: q, hub: NewHub()}
	events, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	sent := make(chan struct{})
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader(`{"message":"hello","run_id":"r1"}`))
		s.send(httptest.NewRecorder(), req)
		close(sent)
	}()

	next := func(kind string) HubEvent {
		t.Helper()
		for {
			select {
			case ev := <-events:
				if ev.Kind == kind {
					return ev
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("no %s event", kind)
			}
		}
	}

	if ev := next("started"); ev.ConvID != "r1" || ev.Position != 1 {
		t.Errorf("started = %+v, want r1 first in line", ev)
	}
	if got := s.runs.WaitingFor("r1"); got != 1 {
		t.Errorf("the list would show position %d, want 1", got)
	}

	s.runs.Cancel("r1")
	if ev := next("finished"); ev.ConvID != "r1" || !ev.Failed {
		t.Errorf("finished = %+v, want r1 marked failed", ev)
	}
	select {
	case <-sent:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream stayed open after the turn ended")
	}
	if got := s.runs.WaitingFor("r1"); got != 0 {
		t.Errorf("a finished turn still reads as position %d", got)
	}
}
