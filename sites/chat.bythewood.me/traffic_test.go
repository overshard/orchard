package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestATrafficQuestionIsReadBeforeTheModelDecides(t *testing.T) {
	first := "what's my most popular blog post according to orchard-analytics"
	history := []Message{{Role: RoleUser, Content: first}, {Role: RoleAssistant, Content: "It had 8 views."}}
	for _, c := range []struct {
		q        string
		history  []Message
		property string
		days     int
		ok       bool
	}{
		{first, nil, "blog", 0, true},
		{"only 8 views in 7 days? that doesn't sound right", history, "blog", 7, true},
		{"how many visitors did my portfolio get this month", nil, "isaacbythewood", 30, true},
		{"where is my site's traffic coming from", nil, "", 0, true},
		{"what are your views on tabs versus spaces", nil, "", 0, false},
		{"how's traffic on I-40 right now", nil, "", 0, false},
		{"what is google analytics", nil, "", 0, false},
		{"what are your views on that", history[:1], "blog", 0, true},
		{"is my site up", nil, "", 0, false},
	} {
		property, days, ok := trafficQuestion(c.q, c.history)
		if ok != c.ok || property != c.property || days != c.days {
			t.Errorf("%q = %q, %d, %v, want %q, %d, %v", c.q, property, days, ok, c.property, c.days, c.ok)
		}
	}
}

func TestAToolThatFailedTwiceIsTakenAway(t *testing.T) {
	var offered [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Tools) == 0 {
			streamReply(w, Message{Content: "done"})
			return
		}
		var names []string
		for _, tl := range req.Tools {
			names = append(names, tl.Function.Name)
		}
		offered = append(offered, names)
		if len(offered) <= 2 {
			streamReply(w, Message{ToolCalls: []ToolCall{call(fmt.Sprint("c", len(offered)), "calc",
				fmt.Sprintf(`{"expression":"%d+"}`, len(offered)))}})
			return
		}
		streamReply(w, Message{Content: "It would not add up."})
	}))
	defer srv.Close()
	e := NewEngine(NewLLM(srv.URL, "local", "k"), "test")
	e.Render = func(md string) string { return md }
	if _, _, _, _, _, err := e.Run(context.Background(), nil, "what is 1 plus", "", "", NewTrace(nil), func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if len(offered) < 3 {
		t.Fatalf("%d decide rounds", len(offered))
	}
	if !slices.Contains(offered[1], "calc") {
		t.Error("calc was taken away after one failure")
	}
	if slices.Contains(offered[2], "calc") {
		t.Error("calc was still offered after failing twice")
	}
}
