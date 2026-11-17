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
// to a JSON schema, with no tools and no streaming. The gateway hands it to
// Claude Haiku as a structured output, so the answer always parses.
type Model struct {
	url  string
	key  string
	http *http.Client
}

const modelTimeout = 2 * time.Minute

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

// Structured asks for one answer that has to parse as schema. The gateway
// strips length and count keywords like maxLength and maxItems before the API
// sees them, so the prompt says how long and Go holds the counts.
func (m *Model) Structured(ctx context.Context, system, user string, schema map[string]any, maxTok int, out any) error {
	body, err := json.Marshal(map[string]any{
		"model":      "claude-haiku-5-5",
		"messages":   []modelMsg{{"system", system}, {"user", user}},
		"max_tokens": maxTok,
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
		if resp.StatusCode == http.StatusUnprocessableEntity {
			return fmt.Errorf("model: refused: %s", strings.TrimSpace(string(msg)))
		}
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
	return json.Unmarshal([]byte(r.Choices[0].Message.Content), out)
}
