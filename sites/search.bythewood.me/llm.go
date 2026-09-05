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
	Model           string          `json:"model"`
	Messages        []chatMessage   `json:"messages"`
	Temperature     float64         `json:"temperature"`
	TopP            float64         `json:"top_p"`
	TopK            int             `json:"top_k"`
	MinP            float64         `json:"min_p"`
	PresencePenalty float64         `json:"presence_penalty"`
	MaxTokens       int             `json:"max_tokens"`
	Stream          bool            `json:"stream"`
	ResponseFormat  *responseFormat `json:"response_format,omitempty"`
	TemplateKwargs  map[string]any  `json:"chat_template_kwargs,omitempty"`
}

// Qwen publishes sampling numbers per mode on the model card, and this pipeline
// needs two of them rather than one setting for everything.
//
// A step handing over a JSON schema wants the likeliest token inside the
// grammar, since the schema is doing the deciding and creativity there is only
// a way to pick the wrong enum. Synthesis is the one step writing prose, and
// there the model card's own non-thinking numbers apply. The presence penalty
// matters most: Qwen names it as the fix for the model repeating itself, which
// is exactly the failure this site keeps hitting, a closing paragraph that says
// the bullet list again in weaker words.
//
// Before this everything ran at temperature 0.2 with llama.cpp's defaults for
// the rest, so prose was sampled almost greedily with nothing discouraging
// repetition.
var (
	exact = sampling{Temperature: 0.2, TopP: 0.8, TopK: 20}
	prose = sampling{Temperature: 0.7, TopP: 0.8, TopK: 20, PresencePenalty: 1.5}
)

type sampling struct {
	Temperature     float64
	TopP            float64
	TopK            int
	PresencePenalty float64
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
	return l.call(ctx, system, user, maxTokens, nil, prose)
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
	text, err := l.call(ctx, system, user, maxTokens, format, exact)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i > 0 {
		text = text[i:]
	}
	return json.Unmarshal([]byte(text), out)
}

func (l *LLM) call(ctx context.Context, system, user string, maxTokens int, format *responseFormat, s sampling) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:           l.Model,
		Temperature:     s.Temperature,
		TopP:            s.TopP,
		TopK:            s.TopK,
		PresencePenalty: s.PresencePenalty,
		MaxTokens:       maxTokens,
		ResponseFormat:  format,
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
	l.call(ctx, "", "hi", 1, nil, exact)
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
