package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// llama.cpp puts running totals on every chunk when asked for per token
// timings, so the count is the last one and not the sum of them all.
func TestAStreamCountsItsTokensOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 1; i <= 345; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}],"+
				"\"timings\":{\"prompt_n\":4961,\"predicted_n\":%d,\"predicted_per_second\":60}}\n\n", i)
		}
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4961,\"completion_tokens\":345}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	_, st, err := NewLLM(srv.URL, "local", "k").Stream(context.Background(), nil, 100, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if st.Completion != 345 || st.Prompt != 4961 || st.Decode != 60 {
		t.Errorf("got %+v, want 345 out, 4961 in, 60 tok/s", st)
	}
}
