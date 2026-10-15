package main

import (
	"context"
	"sync"
	"time"
)

// Queue runs one turn at a time. The model server runs --parallel 1 since Qwen3.5's
// hybrid attention corrupts context checkpoints under multi-slot load, and nothing
// about who is waiting is kept past the request.
type Queue struct {
	mu      sync.Mutex
	waiting []*ticket
	running bool
}

type ticket struct {
	ready chan struct{}
	done  bool
}

// QueueState is what a waiting page is told, since a tab sitting on "thinking"
// with another turn ahead looks broken. Positions only, never identities.
type QueueState struct {
	Position int `json:"position"` // 1 means next, 0 means running now
	Ahead    int `json:"ahead"`
	Total    int `json:"total"`
}

func NewQueue() *Queue { return &Queue{} }

// Enter blocks until this caller's turn and the returned release must be called.
// A cancelled context leaves the queue, and that's a stopped or timed out turn
// and not a closed tab, since a turn outlives its tab.
func (q *Queue) Enter(ctx context.Context, onWait func(QueueState)) (release func(), ok bool) {
	t := &ticket{ready: make(chan struct{}, 1)}

	q.mu.Lock()
	q.waiting = append(q.waiting, t)
	first := !q.running && len(q.waiting) == 1
	if first {
		q.running = true
		q.waiting = q.waiting[1:]
		t.done = true
	}
	q.mu.Unlock()

	if first {
		return q.finish, true
	}

	// Report the starting position now so a waiting page isn't blank until the
	// first tick.
	if onWait != nil {
		onWait(q.stateOf(t))
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-t.ready:
			return q.finish, true
		case <-ctx.Done():
			q.drop(t)
			return func() {}, false
		case <-tick.C:
			if onWait != nil {
				onWait(q.stateOf(t))
			}
		}
	}
}

func (q *Queue) stateOf(t *ticket) QueueState {
	q.mu.Lock()
	defer q.mu.Unlock()
	pos := 0
	for i, w := range q.waiting {
		if w == t {
			pos = i + 1
			break
		}
	}
	return QueueState{Position: pos, Ahead: pos - 1, Total: len(q.waiting)}
}

// finish hands the slot to whoever is next.
func (q *Queue) finish() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.waiting) == 0 {
		q.running = false
		return
	}
	next := q.waiting[0]
	q.waiting = q.waiting[1:]
	next.done = true
	// Buffered, so this never blocks on a receiver that has gone away.
	next.ready <- struct{}{}
}

// drop removes a ticket that gave up before its turn. The context can end just as
// finish promotes it, and then the slot has to be handed on or the person behind
// waits forever.
func (q *Queue) drop(t *ticket) {
	q.mu.Lock()
	for i, w := range q.waiting {
		if w == t {
			q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
			q.mu.Unlock()
			return
		}
	}
	promoted := t.done
	q.mu.Unlock()

	if promoted {
		q.finish()
	}
}

// Depth is how many are waiting, for the page to show before anything is asked.
func (q *Queue) Depth() (waiting int, running bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.waiting), q.running
}
