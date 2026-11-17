package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The request carries the schema as a structured output and no sampling
// fields, which Haiku refuses, and the answer decodes into out.
func TestModelStructuredRequest(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"read\":\"Fine.\"}"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	var out struct {
		Read string `json:"read"`
	}
	if err := NewModel(srv.URL, "key").Structured(context.Background(), "s", "u", map[string]any{"type": "object"}, 10, &out); err != nil {
		t.Fatal(err)
	}
	if out.Read != "Fine." {
		t.Errorf("read %q", out.Read)
	}
	for _, k := range []string{"temperature", "top_p", "chat_template_kwargs"} {
		if _, ok := got[k]; ok {
			t.Errorf("sent %s", k)
		}
	}
	if rf, _ := got["response_format"].(map[string]any); rf["type"] != "json_schema" {
		t.Errorf("response_format %v", got["response_format"])
	}
}

// A refusal and a cut off answer are both errors, never a half object.
func TestModelRefusalAndCutOff(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"refused": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"error":{"message":"the model declined to answer","type":"refusal"}}`)
		},
		"cut": func(w http.ResponseWriter) {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"read\":\"Fi"},"finish_reason":"length"}]}`)
		},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reply(w) }))
		var out map[string]any
		err := NewModel(srv.URL, "key").Structured(context.Background(), "s", "u", nil, 10, &out)
		srv.Close()
		if err == nil || name == "refused" && !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s: err %v", name, err)
		}
	}
}
