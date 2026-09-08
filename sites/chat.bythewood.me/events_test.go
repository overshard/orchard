package main

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The meta stream is what makes a turn started on the desktop show up on the
// phone. It has to open immediately, since a proxy that buffers until it sees
// output would otherwise hold the whole stream, and it has to stop when the tab
// goes away rather than leaking a subscriber per reload.
func TestEventsStreamsAndCleansUp(t *testing.T) {
	s := &site{hub: NewHub()}
	srv := httptest.NewServer(http.HandlerFunc(s.events))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("content type = %q", got)
	}

	br := bufio.NewReader(resp.Body)
	// The opening comment, so a buffering proxy lets the stream through.
	line, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, ":") {
		t.Fatalf("first line = %q, %v, want a comment", line, err)
	}

	// A subscriber has to be registered by now, or a publish would go nowhere.
	deadline := time.Now().Add(2 * time.Second)
	for s.hub.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.hub.Len() != 1 {
		t.Fatalf("%d subscribers, want 1", s.hub.Len())
	}

	s.hub.Publish(HubEvent{Kind: "finished", ConvID: "c9", Title: "Named"})
	var got string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			got = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			break
		}
	}
	if !strings.Contains(got, `"conversation_id":"c9"`) || !strings.Contains(got, `"kind":"finished"`) {
		t.Errorf("frame = %s", got)
	}

	// A tab that goes away drops its subscriber rather than leaving one behind
	// for every reload the phone does.
	cancel()
	resp.Body.Close()
	for time.Now().Before(deadline) {
		if s.hub.Len() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("%d subscribers left after the reader went away", s.hub.Len())
}
