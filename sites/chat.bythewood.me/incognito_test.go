package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The gateway keeps every prompt it forwards, so an incognito turn that writes
// nothing here would still be readable there without this header.
func TestAnIncognitoTurnAsksTheGatewayNotToLog(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get(incognitoHeader)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	}))
	defer srv.Close()

	l := NewLLM(srv.URL, "local", "key")
	var out chatResp
	if err := l.post(WithIncognito(context.Background()), chatReq{Model: "local"}, &out); err != nil {
		t.Fatalf("post: %v", err)
	}
	if seen != "1" {
		t.Errorf("incognito header = %q, want 1", seen)
	}

	seen = ""
	if err := l.post(context.Background(), chatReq{Model: "local"}, &out); err != nil {
		t.Fatalf("post: %v", err)
	}
	if seen != "" {
		t.Errorf("an ordinary turn sent the incognito header = %q", seen)
	}
}
