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

// scriptModel plays a fixed list of decide replies in order, answers every gate
// check with "answered", and records what the answer step was handed.
type scriptModel struct {
	mu      sync.Mutex
	replies []string
	decides int
	final   []map[string]string
}

func (s *scriptModel) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages       []map[string]string `json:"messages"`
			Tools          []json.RawMessage   `json:"tools"`
			Stream         bool                `json:"stream"`
			ResponseFormat json.RawMessage     `json:"response_format"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case len(req.ResponseFormat) > 0:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"verdict\":\"answered\",\"query\":\"\",\"needs_fresh\":false,\"needs_check\":false}"}}]}`)
		case len(req.Tools) == 0:
			s.final = req.Messages
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			s.decides++
			reply := `calc:1+1`
			if s.decides <= len(s.replies) {
				reply = s.replies[s.decides-1]
			}
			w.Header().Set("Content-Type", "application/json")
			if expr, ok := strings.CutPrefix(reply, "calc:"); ok {
				fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"",
					"tool_calls":[{"id":"c%d","type":"function","function":
					{"name":"calc","arguments":"{\"expression\":\"%s\"}"}}]}}]}`, s.decides, expr)
				return
			}
			body, _ := json.Marshal(reply)
			fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%s}}]}`, body)
		}
	}))
}

func (s *scriptModel) drive(t *testing.T) {
	t.Helper()
	srv := s.server(t)
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "local", "k"), "test")
	e.Render = func(md string) string { return md }
	if _, _, _, _, _, err := e.Run(context.Background(), nil, "how fast is it",
		"", "", NewTrace(nil), func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// A send back on the fifth round used to be followed by the budget cut, so the
// nudge to go and search was read and then answered with the tools off.
func TestASendBackOnTheLastRoundGetsARoundToActOn(t *testing.T) {
	s := &scriptModel{replies: []string{"calc:1+1", "calc:1+2", "calc:1+3", "calc:1+4",
		"Let me check that for you.", "calc:1+5"}}
	s.drive(t)
	if s.decides != maxToolRounds {
		t.Errorf("%d decide rounds, want %d so the nudge after round 5 is acted on", s.decides, maxToolRounds)
	}
}

// A draft the gate let through goes to the writer, so the answer keeps its
// figures rather than working them out a second time.
func TestTheWriterIsHandedTheCheckedDraft(t *testing.T) {
	draft := "Used ones sell for about $295 and ask about $400."
	s := &scriptModel{replies: []string{"calc:295+105", draft}}
	s.drive(t)
	n := len(s.final)
	if n < 2 {
		t.Fatalf("the answer step got %d messages", n)
	}
	if s.final[n-2]["role"] != "assistant" || s.final[n-2]["content"] != draft {
		t.Errorf("the message before the instruction is %v, want the draft", s.final[n-2])
	}
	if !strings.Contains(s.final[n-1]["content"], "Your reply above has been checked") {
		t.Errorf("the instruction does not mention the draft: %q", s.final[n-1]["content"])
	}
}

// With no draft, as when the budget ran out mid search, nothing is claimed.
func TestNoDraftNoteWithoutADraft(t *testing.T) {
	if draftNote("  ") != "" {
		t.Error("an empty draft produced a note")
	}
}

// With nothing looked up the writer has nothing to add, and asked to write out
// a draft about Christianity it wrote the answer before it about Wednesday.
func TestADraftWithNothingLookedUpIsTheAnswer(t *testing.T) {
	draft := "Christianity spread through the Roman roads and then had the state behind it."
	s := &scriptModel{replies: []string{draft}}
	srv := s.server(t)
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "local", "k"), "test")
	e.Render = func(md string) string { return md }
	msg, _, _, _, _, err := e.Run(context.Background(), nil, "how fast is it", "", "", NewTrace(nil), func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != draft {
		t.Errorf("answer = %q, want the draft", msg.Content)
	}
	if s.final != nil {
		t.Error("the writer was called for a draft with nothing to cite")
	}
}

func TestAWrittenAnswerCopyingAnEarlierOneIsCaught(t *testing.T) {
	earlier := "The name comes from the Anglo-Saxons. In Old English it was Wodnesdaeg, meaning day of Woden, their equivalent of the Norse god Odin. So the English name honours a Germanic god. In the Romance languages it comes from a different root, the Latin day of Mercury."
	draft := "Christianity took over gradually over centuries, through the Roman roads, a common language, bishops and a written Bible, and then the state itself once Constantine converted in 312."
	if !copiedEarlier(earlier, draft, []string{earlier}) {
		t.Error("a copy of the earlier answer was not caught")
	}
	if copiedEarlier(draft+" It also spread through families.", draft, []string{earlier}) {
		t.Error("the draft written out was read as a copy")
	}
	if copiedEarlier(earlier, earlier, []string{earlier}) {
		t.Error("a draft that was itself the earlier answer should be left to the gate")
	}
}
