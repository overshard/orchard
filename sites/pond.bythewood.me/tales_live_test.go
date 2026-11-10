package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// TestTalesLive writes one night against the real gateway and prints it. Run
// it in a container on orchard-edge with TALES_LIVE=1 and LLM_KEY set.
func TestTalesLive(t *testing.T) {
	if os.Getenv("TALES_LIVE") == "" {
		t.Skip("set TALES_LIVE=1 to call the model")
	}
	m := newModel("http://orchard-llm:8000", os.Getenv("LLM_KEY"))
	if m == nil {
		t.Fatal("no LLM_KEY")
	}
	defer m.call(context.Background(), http.MethodPost, "/v1/unload", nil)
	n := &nightly{m: m, dir: t.TempDir()}
	got, kept, dropped, err := n.write(context.Background(), "2026-11-11")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.MarshalIndent(got, "", " ")
	t.Logf("kept %d, dropped %d\n%s", kept, dropped, raw)
}
