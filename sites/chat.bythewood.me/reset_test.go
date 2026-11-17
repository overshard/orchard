package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Prose written before a tool call is "let me look", so the page is told to
// take it back, and the round after the results is the answer.
func TestProseBeforeACallIsTakenBack(t *testing.T) {
	var mu sync.Mutex
	rounds := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		rounds++
		if rounds == 1 {
			streamReply(w, Message{Content: "Let me add that up.", ToolCalls: []ToolCall{
				call("c1", "calc", `{"expression":"2+2"}`)}})
			return
		}
		streamReply(w, Message{Content: "It comes to 4."})
	}))
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "m", "k"), "test")
	e.Render = func(md string) string { return md }
	var kinds []string
	reply, used, _, _, _, err := e.Run(context.Background(), nil, "what is 2 plus 2", "", "", NewTrace(nil),
		func(ev Event) { kinds = append(kinds, ev.Kind) })
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "It comes to 4." || len(used) != 1 || rounds != 2 {
		t.Errorf("reply %q after %d rounds and %d tools", reply.Content, rounds, len(used))
	}
	reset, tool := -1, -1
	for i, k := range kinds {
		if k == "reset" && reset < 0 {
			reset = i
		}
		if k == "tool" && tool < 0 {
			tool = i
		}
	}
	if reset < 0 || tool < reset {
		t.Errorf("events %v, want a reset before the tool ran", kinds)
	}
}
