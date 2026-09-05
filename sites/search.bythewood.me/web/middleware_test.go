package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The wrappers here embed http.ResponseWriter, so a handler asserting
// w.(http.Flusher) sees the wrapper and fails. Every SSE endpoint has to reach
// the real writer through ResponseController instead, and this is the check
// that the chain still lets it.
func TestChainKeepsTheWriterFlushable(t *testing.T) {
	handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: status\ndata: {}\n\n"))
		if err := rc.Flush(); err != nil {
			t.Errorf("flush through the chain: %v", err)
		}
		<-r.Context().Done()
	}), Recovered, Logged)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200: the writer lost its flusher", resp.StatusCode)
	}

	// Reading a frame before the handler returns is the whole point, since a
	// buffered response would arrive only at close.
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the first frame: %v", err)
	}
	if !strings.HasPrefix(line, "event: status") {
		t.Fatalf("first frame %q, want an event", line)
	}
}
