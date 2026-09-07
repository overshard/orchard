package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chat.bythewood.me/tools"
)

// A model server that answers the freshness question with whatever the test
// wants, and records what it was sent.
func fakeJudge(t *testing.T, needsFresh bool, query string) (*LLM, *[]chatReq) {
	t.Helper()
	var got []chatReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req chatReq
		_ = json.Unmarshal(body, &req)
		got = append(got, req)
		payload, _ := json.Marshal(freshness{NeedsFresh: needsFresh, Query: query})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, mustJSONString(string(payload)))
	}))
	t.Cleanup(srv.Close)
	return NewLLM(srv.URL, "local", ""), &got
}

func mustJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// The failure this was written for: a draft that sounds like an answer, on a
// question the weights cannot answer, with nothing fetched.
func TestAFreshQuestionWithNoToolCallIsSentBack(t *testing.T) {
	llm, _ := fakeJudge(t, true, "big news today")
	e := NewEngine(llm, "test")

	nudge, _ := e.gate(context.Background(), "any big news today?",
		"No big news today. It is a quiet Monday.", nil, nil, func(Event) {})

	if nudge == "" {
		t.Fatal("a confident draft on a current question was let through")
	}
	if !strings.Contains(nudge, "big news today") {
		t.Errorf("the nudge carries no query to search for: %q", nudge)
	}
}

// The check is about the question, so a turn that already fetched something is
// not sent back for asking about today.
func TestAFreshQuestionThatFetchedSomethingIsNotSentBack(t *testing.T) {
	llm, sent := fakeJudge(t, true, "big news today")
	e := NewEngine(llm, "test")

	used := []tools.Result{{Name: "web_search", Content: map[string]any{"results": "something"}}}
	_, _ = e.gate(context.Background(), "any big news today?", "Here is what happened.", nil, used, func(Event) {})

	for _, req := range *sent {
		for _, m := range req.Messages {
			if m.Role == RoleSystem && strings.Contains(m.Content, "needs_fresh") {
				t.Fatal("the freshness check ran on a turn that had already fetched")
			}
		}
	}
}

// Turning the model loose again after a nudge is what let the old loop run out
// of attempts, so the round after one has to be a forced call.
func TestTheCallAfterANudgeRequiresATool(t *testing.T) {
	var choices []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req chatReq
		_ = json.Unmarshal(body, &req)
		choices = append(choices, req.ToolChoice)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()
	l := NewLLM(srv.URL, "local", "")

	schemas := []map[string]any{{"type": "function"}}
	if _, _, err := l.CompleteStats(context.Background(), nil, schemas, 10); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.CompleteRequiringTool(context.Background(), nil, schemas, 10); err != nil {
		t.Fatal(err)
	}
	want := []string{"auto", "required"}
	for i, w := range want {
		if choices[i] != w {
			t.Errorf("call %d sent tool_choice %q, want %q", i, choices[i], w)
		}
	}
}

// With no tools on the table there is nothing to require, and asking for one
// anyway is a request llama.cpp cannot satisfy.
func TestNoToolsMeansNoToolChoice(t *testing.T) {
	var seen chatReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seen)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	l := NewLLM(srv.URL, "local", "")
	if _, _, err := l.CompleteRequiringTool(context.Background(), nil, nil, 10); err != nil {
		t.Fatal(err)
	}
	if seen.ToolChoice != "" {
		t.Errorf("tool_choice = %q with no tools offered", seen.ToolChoice)
	}
}

// A ticker or an abbreviation is too short to name on its own, so the answer
// goes to the titler as well. "VXUS" alone came back "Video game streaming
// service".
func TestTheTitlerIsGivenTheAnswerAsWellAsTheQuestion(t *testing.T) {
	var seen chatReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seen)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Vanguard Total International Stock ETF"}}]}`)
	}))
	defer srv.Close()

	c := NewCompactor(NewLLM(srv.URL, "local", ""), 65536)
	got := c.Title(context.Background(), "VXUS", "VXUS is the Vanguard Total International Stock ETF.")
	if got != "Vanguard Total International Stock ETF" {
		t.Errorf("title = %q", got)
	}
	var prompt string
	for _, m := range seen.Messages {
		if m.Role == RoleUser {
			prompt = m.Content
		}
	}
	if !strings.Contains(prompt, "Vanguard") {
		t.Errorf("the answer never reached the titler: %q", prompt)
	}
	if !strings.Contains(prompt, "VXUS") {
		t.Errorf("the question never reached the titler: %q", prompt)
	}
}

// A turn with no answer to hand still has to produce a name rather than an
// empty prompt section.
func TestTheTitlerWorksWithNoAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Sheet pan dinners"}}]}`)
	}))
	defer srv.Close()

	c := NewCompactor(NewLLM(srv.URL, "local", ""), 65536)
	if got := c.Title(context.Background(), "easy weeknight meals", ""); got != "Sheet pan dinners" {
		t.Errorf("title = %q", got)
	}
}
