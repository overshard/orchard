package main

// What every open tab is told, whichever one asked for it.
//
// A turn already outlives the tab that started it, but nothing told the other
// tabs about it. So a question asked on the desktop never reached the phone
// without a reload, and switching to another conversation and back was the only
// way to see an answer that had finished while you were elsewhere. Isaac asked
// for a notification, a queue and cross device history, and the queue was the
// only one of the three that already existed.
//
// This is the meta channel and deliberately not a second copy of the answer
// stream. It carries which conversation changed and nothing about what was
// said, so a tab knows what to go and read. The answer itself still comes from
// the run, which is the one place it is assembled.

import (
	"encoding/json"
	"sync"
)

// A slow reader is dropped rather than waited on. A tab on a locked phone can
// stop reading for minutes, and a broadcast that blocks on it would stall every
// other tab and the turn doing the publishing.
const hubBuffer = 16

type HubEvent struct {
	Kind   string `json:"kind"` // started, finished, changed
	ConvID string `json:"conversation_id,omitempty"`
	Title  string `json:"title,omitempty"`
}

type Hub struct {
	mu   sync.Mutex
	subs map[chan HubEvent]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan HubEvent]struct{}{}} }

// Subscribe returns a channel of events and the function that closes it. The
// caller must call cancel, and may call it more than once.
func (h *Hub) Subscribe() (<-chan HubEvent, func()) {
	ch := make(chan HubEvent, hubBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// Publish tells every open tab. It never blocks: a subscriber whose buffer is
// full has stopped reading, and the events are hints to go and re-read rather
// than a record that has to arrive.
func (h *Hub) Publish(ev HubEvent) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Len is how many tabs are listening. It exists so nothing has to reach into
// the map without the lock to find out.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

func (ev HubEvent) frame() []byte {
	b, err := json.Marshal(ev)
	if err != nil {
		return nil
	}
	return b
}
