package main

import (
	"testing"
	"time"
)

func TestHubReachesEveryOpenTab(t *testing.T) {
	h := NewHub()
	a, closeA := h.Subscribe()
	b, closeB := h.Subscribe()
	defer closeA()
	defer closeB()

	h.Publish(HubEvent{Kind: "finished", ConvID: "c1", Title: "A title"})
	for _, ch := range []<-chan HubEvent{a, b} {
		select {
		case ev := <-ch:
			if ev.ConvID != "c1" || ev.Title != "A title" {
				t.Errorf("got %#v", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("a subscriber was not told")
		}
	}
}

// A tab on a locked phone stops reading. Waiting on it would stall the turn
// doing the publishing and every other tab with it, and these events are a hint
// to go and re-read rather than a record that has to arrive.
func TestHubDoesNotBlockOnATabThatStoppedReading(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < hubBuffer*4; i++ {
			h.Publish(HubEvent{Kind: "finished", ConvID: "c1"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publishing blocked on a subscriber that was not reading")
	}
}

// Unsubscribing has to be safe to call twice, since the handler defers it and
// an error path calls it as well.
func TestHubCancelIsIdempotent(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe()
	cancel()
	cancel()
	h.Publish(HubEvent{Kind: "changed"})
	if n := h.Len(); n != 0 {
		t.Errorf("%d subscribers left after cancel", n)
	}
}
