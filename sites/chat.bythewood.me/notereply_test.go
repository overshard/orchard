package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Handed "Remembered it." to write up, the writer wrote "Understood. I'll write
// the answer now." and that was the whole reply.
func TestANoteKeepsTheLineTheModelWrote(t *testing.T) {
	var mu sync.Mutex
	rounds, streamed := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools  []map[string]any `json:"tools"`
			Stream bool             `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		defer mu.Unlock()
		if req.Stream {
			streamed++
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Understood. I'll write the answer now.\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		rounds++
		if rounds == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
				"finish_reason": "tool_calls",
				"message": Message{Role: RoleAssistant, ToolCalls: []ToolCall{
					call("c1", "remember", `{"action":"add","fact":"Isaac wants quick chat on dash."}`)}},
			}}})
			return
		}
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Remembered it."}}]}`)
	}))
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "local", "k"), "test")
	e.Render = func(md string) string { return md }
	e.deps.Memory = &fakeMemory{}

	reply, used, _, _, _, err := e.Run(context.Background(), nil,
		"remember that i want quick chat on dash", "", "", NewTrace(nil), func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "Remembered it." || streamed != 0 {
		t.Errorf("reply = %q after %d streamed answers, want the model's own line", reply.Content, streamed)
	}
	if len(used) != 1 {
		t.Errorf("a note made %d calls, want one", len(used))
	}
}

// Made to call a tool, a model that writes prose until the cap instead has not
// written a draft, and half an answer handed to the writer came out with a
// stray [1] on its own line at the end.
func TestProseRunToTheCapOnAForcedRoundIsDropped(t *testing.T) {
	var mu sync.Mutex
	rounds := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools  []map[string]any `json:"tools"`
			Stream bool             `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		defer mu.Unlock()
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Tea, noted.\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if len(req.Tools) == 0 {
			fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{}"}}]}`)
			return
		}
		rounds++
		body := map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "Tea it is."}}
		if rounds == 1 {
			body = map[string]any{"finish_reason": "length",
				"message": map[string]any{"role": "assistant", "content": strings.Repeat("Tea is good [1] ", 400)}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{body}})
	}))
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "local", "k"), "test")
	e.Render = func(md string) string { return md }
	e.deps.Memory = &fakeMemory{}
	tr := NewTrace(nil)

	reply, _, _, _, _, err := e.Run(context.Background(), nil, "remember that i like tea", "", "", tr, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reply.Content, "Tea is good") {
		t.Errorf("the runaway prose reached the answer: %q", reply.Content[:80])
	}
	dropped := false
	for _, st := range tr.Steps() {
		if strings.Contains(st.Label, "dropped prose") {
			dropped = true
		}
	}
	if !dropped {
		t.Error("the trace does not say the prose was dropped")
	}
}
