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

// stubModel answers every decide call with the same failing calc call, so the
// round loop is driven without a model anywhere near it. The last reply has no
// tool calls, which is what the answer step looks like.
type stubModel struct {
	mu     sync.Mutex
	rounds int
	notes  []string
}

func (s *stubModel) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools  []json.RawMessage `json:"tools"`
			Stream bool              `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		s.mu.Lock()
		defer s.mu.Unlock()
		// The answer step is the call that arrives with no tools offered.
		if len(req.Tools) == 0 {
			if n := len(req.Messages); n > 0 {
				s.notes = append(s.notes, req.Messages[n-1].Content)
			}
			if !req.Stream {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"}}]}`)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		s.rounds++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"",
			"tool_calls":[{"id":"c%d","type":"function","function":
			{"name":"calc","arguments":"{\"expression\":\"r%d = 1 $ 2\"}"}}]}}]}`, s.rounds, s.rounds)
	}))
}

func (s *stubModel) drive(t *testing.T) {
	t.Helper()
	srv := s.server(t)
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "local", "k"), "test")
	e.Render = func(md string) string { return md }
	_, _, _, _, _, err := e.Run(context.Background(), nil, "what is the payment",
		"", "", NewTrace(nil), func(Event) {})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

// A round whose every call failed teaches the model how to call the tool and
// then leaves it no budget to act on that, which is how a turn ends up
// answering from nothing.
func TestAFailedRoundBuysOneRetryAndOnlyOne(t *testing.T) {
	s := &stubModel{}
	s.drive(t)
	if s.rounds <= maxToolRounds-1 {
		t.Errorf("tool rounds = %d, want more than the %d a clean turn gets",
			s.rounds, maxToolRounds-1)
	}
	// One extra and no more, or a tool that always fails spins the turn out.
	if s.rounds > maxToolRounds {
		t.Errorf("tool rounds = %d, the grace round is not capped", s.rounds)
	}
}

// The answer step has to be told what never worked, or it writes an estimate in
// the same voice as a checked figure.
func TestTheAnswerStepIsToldTheCallsFailed(t *testing.T) {
	s := &stubModel{}
	s.drive(t)
	if len(s.notes) == 0 {
		t.Fatal("the answer step was never reached")
	}
	note := s.notes[len(s.notes)-1]
	if !strings.Contains(note, "calc") {
		t.Errorf("the answer step was not told calc failed: %q", note)
	}
	if !strings.Contains(note, "Do not write down a number you could not work out") {
		t.Errorf("the answer step was not told to stop guessing: %q", note)
	}
}
