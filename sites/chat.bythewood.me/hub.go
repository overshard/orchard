package main

// The meta channel every open tab hears. It says which conversation changed and
// nothing about what was said, so a tab knows what to go and read and the answer
// still comes from the run itself.

import (
	"encoding/json"
	"sync"
)

// A slow reader misses events instead of being waited on. A tab on a locked
// phone can stop reading for minutes, and a broadcast that blocks on it would
// stall every other tab and the turn doing the publishing.
const hubBuffer = 16

type HubEvent struct {
	Kind   string `json:"kind"` // started, waiting, working, finished, changed
	ConvID string `json:"conversation_id,omitempty"`
	Title  string `json:"title,omitempty"`
	// Where in line a turn is, 1 being next. Zero is not waiting.
	Position int  `json:"position,omitempty"`
	Failed   bool `json:"failed,omitempty"`
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

// Publish never blocks, since a subscriber whose buffer is full has stopped
// reading and the events are only hints to go and re-read.
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
