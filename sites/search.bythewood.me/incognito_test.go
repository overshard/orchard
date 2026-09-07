package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The gateway keeps every prompt it forwards, so an incognito question that
// leaves no history row here would still be readable there without this header.
func TestAnIncognitoQuestionAsksTheGatewayNotToLog(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get(incognitoHeader)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	}))
	defer srv.Close()

	l := NewLLM(srv.URL, "key")
	if _, err := l.Complete(WithIncognito(context.Background()), "sys", "user", 8); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if seen != "1" {
		t.Errorf("incognito header = %q, want 1", seen)
	}

	seen = ""
	if _, err := l.Complete(context.Background(), "sys", "user", 8); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if seen != "" {
		t.Errorf("an ordinary question sent the incognito header = %q", seen)
	}
}
