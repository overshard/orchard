package main

import (
	"context"
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
