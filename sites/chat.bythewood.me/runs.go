// Turns that outlive the tab that asked for them.
//
// A turn used to hang off the request context, so closing the tab, locking the
// phone or switching app cancelled it mid generation and lost everything it had
// already fetched. A turn takes up to twelve minutes and Isaac has one GPU, so
// the tab is the least reliable part of the arrangement and the wrong thing to
// tie the work to.
//
// So a turn runs detached and writes its events into a run, and a browser is
// only ever a reader. Closing the tab drops a reader. Opening the conversation
// again picks up a new one, which is handed everything the run has produced so
// far and then follows it live.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// How long a finished run is kept so a tab that comes back can still read it.
// The messages are in the database by then, so this only has to cover the gap
// between finishing and the reader noticing.
const runKept = 30 * time.Minute

// The ceiling on what one run holds. A turn's events are small and bounded by
// the tool rounds, and this only exists so a runaway cannot grow without limit.
const runMaxEvents = 4096

type turnRun struct {
	mu sync.Mutex
	// Raw json rather than Event, because the last frame of a turn is the done
	// payload, which carries the stats, the sources and the rendered html and
	// is not an Event at all.
	events []json.RawMessage
	subs   map[chan json.RawMessage]struct{}
	done   bool
	// Set when the run ends, so a reader arriving after the fact is told the
	// turn is over rather than waiting on a stream that will never speak.
	endedAt time.Time
	cancel  context.CancelFunc
}

// Runs holds every turn in flight, keyed by the conversation it belongs to. A
// new conversation has no id until its first turn is stored, so it is keyed by
// the id the browser was given up front instead.
type Runs struct {
	mu sync.Mutex
	m  map[string]*turnRun
}

func NewRuns() *Runs { return &Runs{m: map[string]*turnRun{}} }

// Start opens a run for a key, replacing and cancelling any run already there.
// Sending a second turn into the same conversation while one is still going
// means the first is no longer wanted, and leaving it running would have two
// generations writing into one conversation.
func (rs *Runs) Start(key string) *turnRun {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if old, ok := rs.m[key]; ok && old.cancel != nil {
		old.cancel()
	}
	r := &turnRun{subs: map[chan json.RawMessage]struct{}{}}
	rs.m[key] = r
	return r
}

func (rs *Runs) Get(key string) (*turnRun, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	r, ok := rs.m[key]
	return r, ok
}

// Rekey moves a run to the conversation id the store gave it, so a tab that
// reopens the conversation by its real id finds the turn that is still running
// under the temporary one.
func (rs *Runs) Rekey(from, to string) {
	if from == to || to == "" {
		return
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if r, ok := rs.m[from]; ok {
		rs.m[to] = r
		delete(rs.m, from)
	}
}

// Sweep drops runs that finished long enough ago that nobody is coming back for
// them. Called on a timer rather than on read, so a conversation nobody opens
// again does not sit in memory until the process restarts.
func (rs *Runs) Sweep(now time.Time) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for k, r := range rs.m {
		r.mu.Lock()
		stale := r.done && now.Sub(r.endedAt) > runKept
		r.mu.Unlock()
		if stale {
			delete(rs.m, k)
		}
	}
}

// Cancel stops a run, which is what a browser asking to stop a turn does. The
// events already produced stay readable.
func (rs *Runs) Cancel(key string) bool {
	rs.mu.Lock()
	r, ok := rs.m[key]
	rs.mu.Unlock()
	if !ok {
		return false
	}
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func (r *turnRun) setCancel(cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancel = cancel
}

// Emit records an event and hands it to whoever is reading. A reader that is
// not keeping up is skipped rather than waited on, since one slow tab must not
// hold up the turn or the readers behind it.
func (r *turnRun) Emit(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("an event would not marshal", "err", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	if len(r.events) < runMaxEvents {
		r.events = append(r.events, b)
	}
	for ch := range r.subs {
		select {
		case ch <- b:
		default:
		}
	}
}

// Finish closes the run to new events and wakes every reader so they can see it
// ended rather than sitting on an open stream.
func (r *turnRun) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	r.endedAt = time.Now()
	for ch := range r.subs {
		close(ch)
		delete(r.subs, ch)
	}
}

// Follow hands back everything the run has already produced and a channel of
// what comes next. The backlog is taken under the same lock that registers the
// channel, so an event cannot land in the gap between the two and be lost.
//
// A closed channel means the turn is over. A run that had already finished
// gives back its backlog and a closed channel, which is exactly what a tab
// returning after the fact needs.
func (r *turnRun) Follow() (backlog []json.RawMessage, ch <-chan json.RawMessage, live bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	backlog = append([]json.RawMessage(nil), r.events...)
	if r.done {
		return backlog, nil, false
	}
	c := make(chan json.RawMessage, 256)
	r.subs[c] = struct{}{}
	return backlog, c, true
}

// Unfollow drops a reader, which happens when its tab goes away. The run does
// not care how many readers it has and keeps going with none.
func (r *turnRun) Unfollow(ch <-chan json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for c := range r.subs {
		if (<-chan json.RawMessage)(c) == ch {
			delete(r.subs, c)
			close(c)
			return
		}
	}
}

// Running reports whether a turn is still going, which is what the conversation
// endpoint tells a browser so it knows to attach rather than render and stop.
func (r *turnRun) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.done
}
