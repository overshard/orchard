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
)

// A ticker or an abbreviation is too short to name on its own, so the answer
// goes to the titler as well. "VXUS" alone came back "Video game streaming
// service".
func TestTheTitlerIsGivenTheAnswerAsWellAsTheQuestion(t *testing.T) {
	var seen chatReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seen)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"Vanguard Total International Stock ETF\"}"}}]}`)
	}))
	defer srv.Close()

	c := NewCompactor(NewLLM(srv.URL, "m", ""))
	got := c.Title(context.Background(), "VXUS", "VXUS is the Vanguard Total International Stock ETF.")
	if got != "Vanguard Total International Stock ETF" {
		t.Errorf("title = %q", got)
	}
	if seen.ResponseFormat == nil || seen.ResponseFormat.Type != "json_schema" {
		t.Error("the title was not asked for as structured output")
	}
	var prompt string
	for _, m := range seen.Messages {
		if m.Role == RoleUser {
			prompt = m.Content
		}
	}
	if !strings.Contains(prompt, "Vanguard") || !strings.Contains(prompt, "VXUS") {
		t.Errorf("the titler was not handed both the question and the answer: %q", prompt)
	}
}

func TestTheTitlerWorksWithNoAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"sheet pan dinners.\"}"}}]}`)
	}))
	defer srv.Close()

	c := NewCompactor(NewLLM(srv.URL, "m", ""))
	if got := c.Title(context.Background(), "easy weeknight meals", ""); got != "Sheet pan dinners" {
		t.Errorf("title = %q", got)
	}
}

// The gateway answers a refusal with a 422, which reads as a plain decline and
// not as the gateway's raw error.
func TestARefusalReadsAsADecline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"error":{"message":"the model declined to answer (cyber)","type":"refusal"}}`)
	}))
	defer srv.Close()

	_, _, err := NewLLM(srv.URL, "m", "").Turn(context.Background(), nil, nil, 10, nil)
	if err != errDeclined {
		t.Errorf("err = %v, want the decline", err)
	}
}
