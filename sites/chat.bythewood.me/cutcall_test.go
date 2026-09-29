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

	"chat.bythewood.me/tools"
)

// The refusal llama.cpp sends when any tool call in a request has arguments it
// cannot parse, which it does for every call in the history, not just the last.
const brokenArgsRefusal = `{"error":{"code":500,"type":"server_error","message":"Failed to parse tool call ` +
	`arguments as JSON: [json.exception.parse_error.101] parse error at line 1, column 5419: syntax error ` +
	`while parsing value - invalid string: missing closing quote; last read: 'directed by Feige, directed by Feige'"}}`

// cutStub is a model that refuses any request carrying a call with broken
// arguments, the way llama.cpp does. Its first tool round answers with first,
// every round after that answers in prose, and a request with no tools is the
// answer step.
type cutStub struct {
	mu      sync.Mutex
	first   []ToolCall
	rounds  int
	refused int
	// The last message of each request that offered tools, which is where a
	// note between rounds lands, and what each of those offered and demanded.
	asked   []string
	offered []string
	choice  []string
}

func (s *cutStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages   []Message        `json:"messages"`
			Tools      []map[string]any `json:"tools"`
			ToolChoice string           `json:"tool_choice"`
			Stream     bool             `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		s.mu.Lock()
		defer s.mu.Unlock()
		for _, m := range req.Messages {
			for _, tc := range m.ToolCalls {
				if !json.Valid([]byte(tc.Function.Arguments)) {
					s.refused++
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, brokenArgsRefusal)
					return
				}
			}
		}
		if len(req.Tools) == 0 {
			if req.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
				fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
			return
		}
		s.rounds++
		if n := len(req.Messages); n > 0 {
			s.asked = append(s.asked, req.Messages[n-1].Content)
		}
		var names []string
		for _, tool := range req.Tools {
			names = append(names, tool["function"].(map[string]any)["name"].(string))
		}
		s.offered = append(s.offered, strings.Join(names, " "))
		s.choice = append(s.choice, req.ToolChoice)
		type choice struct {
			FinishReason string  `json:"finish_reason"`
			Message      Message `json:"message"`
		}
		c := choice{FinishReason: "stop", Message: Message{Role: RoleAssistant, Content: "Noted."}}
		if s.rounds == 1 && len(s.first) > 0 {
			c = choice{FinishReason: "length", Message: Message{Role: RoleAssistant, ToolCalls: s.first}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []choice{c}})
	}))
}

func call(id, name, args string) ToolCall {
	var tc ToolCall
	tc.ID, tc.Type = id, "function"
	tc.Function.Name, tc.Function.Arguments = name, args
	return tc
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

// The real turn made three remember calls, two parsed and the third ran on into
// the token cap mid string, and carrying that one into the next request got the
// whole turn refused.
func TestACallCutOffAtTheTokenLimitIsDropped(t *testing.T) {
	s := &cutStub{first: []ToolCall{
		call("c1", "remember", `{"action":"add","fact":"Isaac and his friends want to watch Heretic this weekend."}`),
		call("c2", "remember", `{"action":"add","fact":"Terrifier, directed by Feige's Feige, directed by Feige, directed by`),
	}}
	srv := s.server(t)
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "local", "k"), "test")
	e.Render = func(md string) string { return md }
	mem := &fakeMemory{}
	e.deps.Memory = mem
	tr := NewTrace(nil)

	_, used, _, _, _, err := e.Run(context.Background(), nil,
		"remember that my friends and I want to watch heretic, alien romulus, and terrifier this weekend, "+
			"look up basic details and name of each and add them to memory",
		"", "", tr, func(Event) {})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if s.refused > 0 {
		t.Errorf("the model was sent the broken call %d times", s.refused)
	}
	if s.offered[0] != "remember web_search wikipedia" || s.choice[0] != "required" {
		t.Errorf("round one offered %q with choice %q, want remember and the lookups, forced",
			s.offered[0], s.choice[0])
	}
	facts, _ := mem.Facts()
	if len(facts) != 1 || !strings.Contains(facts[0].Text, "Heretic") {
		t.Errorf("remembered %+v, want only the call that parsed", facts)
	}
	if len(used) != 1 || used[0].Name != "remember" || used[0].Err != "" {
		t.Errorf("used = %+v, want the one remember call", used)
	}
	if len(s.asked) < 2 || !strings.Contains(s.asked[1], "cut off") {
		t.Errorf("the next round was not told a call was cut off: %q", s.asked)
	}
	var dropped, marked bool
	for _, st := range tr.Steps() {
		if st.Bad && strings.Contains(st.Label, "dropped") {
			dropped = true
		}
		if st.Kind == "model" && strings.Contains(st.Meta, "cut off at the token limit") {
			marked = true
		}
	}
	if !dropped {
		t.Error("the trace does not say a call was dropped")
	}
	if !marked {
		t.Error("the round's step does not say the reply was cut off")
	}
}

// Every call cut off and nothing else said is a round that did nothing, so it
// gets the retry a failed round gets and the next round still has to call.
func TestARoundOfOnlyCutCallsIsRetried(t *testing.T) {
	s := &cutStub{first: []ToolCall{
		call("c1", "remember", `{"action":"add","fact":"Heretic, directed by Feige, directed by`),
	}}
	srv := s.server(t)
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "local", "k"), "test")
	e.Render = func(md string) string { return md }
	e.deps.Memory = &fakeMemory{}

	_, _, _, _, _, err := e.Run(context.Background(), nil,
		"remember that i want to watch heretic this weekend", "", "", NewTrace(nil), func(Event) {})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if s.refused > 0 {
		t.Errorf("the model was sent the broken call %d times", s.refused)
	}
	if s.rounds < 2 || !strings.Contains(s.asked[1], "cut off") {
		t.Fatalf("rounds = %d, asked = %q, want a second round told about the cut", s.rounds, s.asked)
	}
	if s.choice[1] != "required" {
		t.Errorf("the second round was not made to call, choice %q", s.choice[1])
	}
}

// The retry with the tools off meets the same refusal unless the broken call and
// its result come out of the messages first.
func TestTheRetryDropsTheBrokenCall(t *testing.T) {
	s := &cutStub{}
	srv := s.server(t)
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "local", "k"), "test")
	e.Render = func(md string) string { return md }
	history := []Message{
		{Role: RoleUser, Content: "note the films"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{call("c9", "remember", `{"action":"add","fact":"Heretic, dir`)}},
		{Role: RoleTool, ToolCallID: "c9", Name: "remember", Content: `{"error":"nothing to remember"}`},
		{Role: RoleAssistant, Content: "Noted."},
	}
	_, _, _, _, _, err := e.Run(context.Background(), history, "what did we settle on", "", "",
		NewTrace(nil), func(Event) {})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if s.refused != 1 {
		t.Errorf("refused %d times, want the first request only", s.refused)
	}
}

func TestWholeCalls(t *testing.T) {
	kept, cut := wholeCalls([]ToolCall{
		call("a", "remember", `{"action":"list"}`),
		call("b", "remember", `{"action":"add","fact":"directed by Feige, directed by`),
		call("c", "calc", ""),
		call("d", "remember", `{"action":"add"`),
	})
	if cut != 2 {
		t.Errorf("cut = %d, want 2", cut)
	}
	if len(kept) != 2 || kept[0].ID != "a" || kept[1].ID != "c" {
		t.Fatalf("kept = %+v", kept)
	}
	if kept[1].Function.Arguments != "{}" {
		t.Errorf("empty arguments went back as %q, want {}", kept[1].Function.Arguments)
	}
	if kept, cut := wholeCalls(nil); kept != nil || cut != 0 {
		t.Errorf("nil calls gave %v, %d", kept, cut)
	}
}

func TestWithoutBrokenCalls(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "q"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			call("ok", "remember", `{"action":"list"}`),
			call("bad", "remember", `{"action":"add","fact":"cut`),
		}},
		{Role: RoleTool, ToolCallID: "ok", Name: "remember", Content: "{}"},
		{Role: RoleTool, ToolCallID: "bad", Name: "remember", Content: "{}"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{call("", "calc", `{"expression":"1 +`)}},
		{Role: RoleTool, ToolCallID: "calc", Name: "calc", Content: "{}"},
		{Role: RoleUser, Content: "go on"},
	}
	got := withoutBrokenCalls(msgs)
	var roles []string
	for _, m := range got {
		roles = append(roles, string(m.Role))
		for _, tc := range m.ToolCalls {
			if !json.Valid([]byte(tc.Function.Arguments)) {
				t.Errorf("a broken call survived: %+v", tc)
			}
		}
		if m.Role == RoleTool && m.ToolCallID != "ok" {
			t.Errorf("the result of a dropped call survived: %+v", m)
		}
	}
	if want := "system user assistant tool user"; strings.Join(roles, " ") != want {
		t.Errorf("roles = %q, want %q", strings.Join(roles, " "), want)
	}
	if len(msgs[2].ToolCalls) != 2 {
		t.Error("the caller's messages were changed in place")
	}
}

func TestAModelRefusalIsTrimmed(t *testing.T) {
	long := strings.Repeat("directed by Feige, ", 300)
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"code": 500,
		"message": "Failed to parse tool call arguments as JSON: last read: '" + long + "'"}})
	err := modelError(500, strings.NewReader(string(body)))
	if len(err.Error()) > 400 {
		t.Errorf("the refusal is %d characters, want it trimmed", len(err.Error()))
	}
	if !isTruncatedToolCall(err) {
		t.Error("trimming lost what the retry matches on")
	}
}
