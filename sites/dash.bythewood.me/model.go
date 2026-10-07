package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Model is the slice of llm.bythewood.me the briefs need: one call, constrained
// to a JSON schema, with no tools and no streaming.
type Model struct {
	url  string
	key  string
	http *http.Client
}

// The first call of a quiet spell loads the weights onto the card before it
// answers, which is most of a minute on its own.
const modelTimeout = 5 * time.Minute

func NewModel(url, key string) *Model {
	if key == "" {
		return nil
	}
	return &Model{url: strings.TrimRight(url, "/"), key: key, http: &http.Client{Timeout: modelTimeout}}
}

type modelMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Structured asks for one answer that has to parse as schema. llama.cpp
// compiles the schema to a grammar and samples against it, so a malformed
// object is not something this has to recover from.
func (m *Model) Structured(ctx context.Context, system, user string, schema map[string]any, maxTok int, out any) error {
	body, err := json.Marshal(map[string]any{
		"model":       "local",
		"messages":    []modelMsg{{"system", system}, {"user", user}},
		"temperature": 0.3,
		"top_p":       0.8,
		"max_tokens":  maxTok,
		// Thinking spends the token budget on a chain nobody reads and can
		// leave the object itself cut off.
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "brief", "strict": true, "schema": schema},
		},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.key)

	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("model: http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&r); err != nil {
		return fmt.Errorf("model: %w", err)
	}
	if len(r.Choices) == 0 {
		return fmt.Errorf("model: no choices")
	}
	if r.Choices[0].FinishReason == "length" {
		return fmt.Errorf("model: answer cut off at %d tokens", maxTok)
	}

	text := strings.TrimSpace(r.Choices[0].Message.Content)
	if i := strings.Index(text, "{"); i > 0 {
		text = text[i:]
	}
	return json.Unmarshal([]byte(text), out)
}
