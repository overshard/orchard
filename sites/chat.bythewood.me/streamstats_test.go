package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The usage arrives on a chunk of its own after the prose, and the calls come
// in pieces that have to be put back together by index.
func TestAStreamedRoundCarriesItsCallsAndItsCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		streamReply(w, Message{Content: "Let me look.", ToolCalls: []ToolCall{
			call("a", "web_search", `{"query":"one"}`),
			call("b", "calc", `{"expression":"1+1"}`),
		}})
	}))
	defer srv.Close()
	var said string
	reply, st, err := NewLLM(srv.URL, "m", "k").Turn(context.Background(), nil, nil, 100, func(d string) { said += d })
	if err != nil {
		t.Fatal(err)
	}
	if said != "Let me look." || reply.Content != said {
		t.Errorf("prose = %q, reply = %q", said, reply.Content)
	}
	if len(reply.ToolCalls) != 2 || reply.ToolCalls[1].ID != "b" || reply.ToolCalls[1].Function.Arguments != `{"expression":"1+1"}` {
		t.Errorf("calls = %+v", reply.ToolCalls)
	}
	if st.Prompt != 10 || st.Completion != 5 {
		t.Errorf("stats = %+v", st)
	}
}

// A streamed refusal finishes with content_filter and no text, which has to be
// an error and not an empty answer.
func TestAStreamedRefusalIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"content_filter\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	if _, _, err := NewLLM(srv.URL, "m", "k").Turn(context.Background(), nil, nil, 100, nil); err != errDeclined {
		t.Errorf("err = %v, want the decline", err)
	}
}
