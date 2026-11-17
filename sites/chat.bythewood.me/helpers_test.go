package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"chat.bythewood.me/internal/history"
	"chat.bythewood.me/tools"
)

func call(id, name, args string) ToolCall {
	var tc ToolCall
	tc.ID, tc.Type = id, "function"
	tc.Function.Name, tc.Function.Arguments = name, args
	return tc
}

// streamReply answers a round the way the gateway streams one, the prose a
// piece at a time, then each call, then the usage on a chunk of its own.
func streamReply(w http.ResponseWriter, msg Message) {
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	if msg.Content != "" {
		send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": msg.Content}}}})
	}
	for i, tc := range msg.ToolCalls {
		send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
			map[string]any{"index": i, "id": tc.ID, "type": "function",
				"function": map[string]any{"name": tc.Function.Name, "arguments": ""}}}}}}})
		send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
			map[string]any{"index": i, "function": map[string]any{"arguments": tc.Function.Arguments}}}}}}})
	}
	finish := "stop"
	if len(msg.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": finish}}})
	send(map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

type fakeMemory struct {
	mu    sync.Mutex
	facts []tools.MemoryFact
}

func (m *fakeMemory) Facts() ([]tools.MemoryFact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]tools.MemoryFact(nil), m.facts...), nil
}

func (m *fakeMemory) Add(text string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := int64(len(m.facts) + 1)
	m.facts = append(m.facts, tools.MemoryFact{ID: id, Text: text})
	return id, nil
}

func (m *fakeMemory) Replace(int64, string) error { return nil }
func (m *fakeMemory) Delete(int64) error          { return nil }

func testStore(t *testing.T) *history.Store {
	t.Helper()
	st, err := history.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
