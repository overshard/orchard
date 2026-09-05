package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// liveModel is a small stand-in for the site's own client, so this package can
// be evaluated against the real model without importing main. It speaks the
// same OpenAI compatible endpoint and sets the one option that matters, since
// a thinking model with thinking left on returns an empty content beside a full
// reasoning_content and looks like a silent failure.
type liveLLM struct {
	base   string
	client *http.Client
}

func liveModel(t *testing.T) *liveLLM {
	t.Helper()
	base := os.Getenv("LLM_URL")
	if base == "" {
		base = "http://orchard-search-llm:8091"
	}
	return &liveLLM{
		base:   strings.TrimRight(base, "/"),
		client: &http.Client{Timeout: 2 * time.Minute},
	}
}

func (l *liveLLM) Structured(ctx context.Context, system, user string, maxTokens int, schema any, out any) error {
	raw, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"model":       "local",
		"temperature": 0.2,
		"max_tokens":  maxTokens,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "response", "strict": true, "schema": json.RawMessage(raw),
			},
		},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", l.base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var r struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if len(r.Choices) == 0 {
		return fmt.Errorf("no choices")
	}
	text := strings.TrimSpace(r.Choices[0].Message.Content)
	if text == "" {
		return fmt.Errorf("empty content, reasoning was %q", r.Choices[0].Message.Reasoning)
	}
	if i := strings.Index(text, "{"); i > 0 {
		text = text[i:]
	}
	return json.Unmarshal([]byte(text), out)
}
