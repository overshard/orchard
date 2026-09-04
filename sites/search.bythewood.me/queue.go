package main

import (
	"context"
	"sync"
	"time"
)

// One question runs at a time.
//
// The model server is started with --parallel 1, because Qwen3.5's hybrid
// attention corrupts context checkpoints under multi-slot load, and there is one
// GPU behind it. Two people asking at once would otherwise interleave into one
// slot and both wait longer than if they had taken turns.
//
// So they take turns, and the waiting is shown rather than hidden: a page that
// sits on "searching" for ninety seconds because someone else is ahead looks
// broken, while one that says it is second in line looks like a queue.
//
// Nothing about who is waiting is recorded. A ticket is a position and a
// channel, and it exists only while its request does.
type Queue struct {
	mu      sync.Mutex
	waiting []*ticket
	running bool
}

type ticket struct {
	ready chan struct{}
	done  bool
}

// QueueState is what a waiting page is told: how far back it is and how many
// are behind it. Positions only, never identities.
type QueueState struct {
	Position int `json:"position"` // 1 means next, 0 means running now
	Ahead    int `json:"ahead"`
	Total    int `json:"total"`
}

func NewQueue() *Queue { return &Queue{} }

// Enter joins the queue and blocks until this caller's turn, reporting position
// changes to onWait while it waits. The returned release must be called.
//
// A caller whose context is cancelled leaves the queue, which is the common
// case: a closed tab should not hold the GPU for the person behind it.
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

	// Report the starting position immediately, so a waiting page says so
	// rather than showing nothing until the first tick.
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

// drop removes a ticket that gave up before its turn.
//
// There is a race worth naming: the context can end in the same moment finish
// promotes this ticket, in which case the slot is already held by a caller that
// will never use it and has to be handed on, or the person behind waits
// forever.
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
