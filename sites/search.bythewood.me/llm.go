package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// LLM talks to llama-server's OpenAI compatible endpoint.
//
// The model runs in a separate container with the GPU attached, so this is
// always a network call even in development.
type LLM struct {
	BaseURL string
	Model   string
	client  *http.Client
}

func NewLLM(baseURL string) *LLM {
	return &LLM{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   "local",
		client:  &http.Client{Timeout: 4 * time.Minute},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens"`
	Stream         bool            `json:"stream"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	TemplateKwargs map[string]any  `json:"chat_template_kwargs,omitempty"`
}

type responseFormat struct {
	Type       string         `json:"type"`
	JSONSchema *schemaWrapper `json:"json_schema,omitempty"`
}

type schemaWrapper struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete runs a free-form completion. Used only for the synthesis step.
func (l *LLM) Complete(ctx context.Context, system, user string, maxTokens int) (string, error) {
	return l.call(ctx, system, user, maxTokens, nil)
}

// Structured constrains the model to a JSON schema. llama.cpp turns the schema
// into a GBNF grammar and constrains sampling to it, so the model cannot emit a
// citation ID outside the enum it was given, and cannot emit a refusal either.
func (l *LLM) Structured(ctx context.Context, system, user string, maxTokens int, schema any, out any) error {
	raw, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	format := &responseFormat{
		Type:       "json_schema",
		JSONSchema: &schemaWrapper{Name: "response", Strict: true, Schema: raw},
	}
	text, err := l.call(ctx, system, user, maxTokens, format)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i > 0 {
		text = text[i:]
	}
	return json.Unmarshal([]byte(text), out)
}

func (l *LLM) call(ctx context.Context, system, user string, maxTokens int, format *responseFormat) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:          l.Model,
		Temperature:    0.2,
		MaxTokens:      maxTokens,
		ResponseFormat: format,
		// Qwen3.5 is a thinking model and llama.cpp puts the chain of thought in
		// reasoning_content, leaving content empty until the budget runs out.
		// Every step here is either schema constrained or wants prose directly,
		// so thinking only burns tokens.
		TemplateKwargs: map[string]any{"enable_thinking": false},
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", l.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("llm: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm: no choices")
	}
	msg := out.Choices[0].Message
	if msg.Content == "" && msg.Reasoning != "" {
		return "", fmt.Errorf("llm: answered with reasoning only, thinking is not disabled")
	}
	return msg.Content, nil
}

// Warm fires a one token completion so llama-swap loads the model while the
// search and the fetches are still in flight. The cold start then happens
// inside time that was already being spent.
func (l *LLM) Warm(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	l.call(ctx, "", "hi", 1, nil)
}

// Healthy asks whether the model server is up, without waking the model.
//
// /v1/models answers from llama-swap's config and loads nothing. Asking /health
// would risk pulling the weights back onto the card every time somebody opens
// the page, which would quietly defeat the idle unload.
func (l *LLM) Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", l.BaseURL+"/v1/models", nil)
	if err != nil {
		return false
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
