// The model client. llama.cpp behind llama-swap speaks the OpenAI chat
// completions shape, so this is that shape and nothing more.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is one request from the model to run something.
type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Message is one entry in the conversation as the model sees it.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type LLM struct {
	BaseURL string
	Model   string
	Key     string
	http    *http.Client
}

func NewLLM(base, model, key string) *LLM {
	if model == "" {
		model = "local"
	}
	return &LLM{
		BaseURL: strings.TrimRight(base, "/"),
		Model:   model,
		Key:     key,
		// Generation is slow and a long answer is normal, so this is generous.
		// The context deadline on the request is the real limit.
		http: &http.Client{Timeout: 10 * time.Minute},
	}
}

type chatReq struct {
	Model           string           `json:"model"`
	Messages        []Message        `json:"messages"`
	Tools           []map[string]any `json:"tools,omitempty"`
	ToolChoice      string           `json:"tool_choice,omitempty"`
	Temperature     float64          `json:"temperature"`
	TopP            float64          `json:"top_p"`
	TopK            int              `json:"top_k"`
	MaxTokens       int              `json:"max_tokens"`
	Stream          bool             `json:"stream,omitempty"`
	StreamOptions   map[string]any   `json:"stream_options,omitempty"`
	TimingsPerToken bool             `json:"timings_per_token,omitempty"`
	Kwargs          map[string]any   `json:"chat_template_kwargs,omitempty"`
	ResponseFormat  *responseFormat  `json:"response_format,omitempty"`
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

type chatResp struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string     `json:"content"`
			Reasoning string     `json:"reasoning_content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		Delta struct {
			Content   string     `json:"content"`
			Reasoning string     `json:"reasoning_content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	// llama.cpp reports its own timings, which are the honest numbers: they
	// exclude this client, the tools and the network.
	Timings struct {
		PromptN         int     `json:"prompt_n"`
		PromptPerSecond float64 `json:"prompt_per_second"`
		PredictedN      int     `json:"predicted_n"`
		PredictedPerSec float64 `json:"predicted_per_second"`
	} `json:"timings"`
	Error any `json:"error"`
}

// Stats is what one turn cost, for the meter in the bar. Prompt is the whole
// window the model was handed, so it is the number that says how full the
// context is, not how long the last message was.
type Stats struct {
	Prompt     int     `json:"prompt_tokens"`
	Completion int     `json:"completion_tokens"`
	Decode     float64 `json:"decode_tps"`
	Prefill    float64 `json:"prefill_tps"`
}

func (s *Stats) merge(o Stats) {
	if o.Prompt > s.Prompt {
		s.Prompt = o.Prompt
	}
	s.Completion += o.Completion
	if o.Decode > 0 {
		s.Decode = o.Decode
	}
	if o.Prefill > 0 {
		s.Prefill = o.Prefill
	}
}

// sampling is Qwen's published non-thinking recipe, which the other models here
// are close enough to. Thinking is off because llama.cpp puts a chain of
// thought in reasoning_content and leaves content empty, which does not look
// like an error: a 200, well formed JSON, and nothing to show the user.
func (l *LLM) base(msgs []Message, maxTok int) chatReq {
	return chatReq{
		Model: l.Model, Messages: msgs, Temperature: 0.7, TopP: 0.8, TopK: 20,
		MaxTokens: maxTok, Kwargs: map[string]any{"enable_thinking": false},
	}
}

// Complete asks for one turn, with tools offered. It does not stream, because a
// turn that is going to call a tool has nothing to show yet.
func (l *LLM) Complete(ctx context.Context, msgs []Message, schemas []map[string]any, maxTok int) (Message, error) {
	m, _, err := l.CompleteStats(ctx, msgs, schemas, maxTok)
	return m, err
}

// CompleteStats is Complete plus what the call cost.
func (l *LLM) CompleteStats(ctx context.Context, msgs []Message, schemas []map[string]any, maxTok int) (Message, Stats, error) {
	req := l.base(msgs, maxTok)
	if len(schemas) > 0 {
		req.Tools, req.ToolChoice = schemas, "auto"
	}
	var out chatResp
	if err := l.post(ctx, req, &out); err != nil {
		return Message{}, Stats{}, err
	}
	if len(out.Choices) == 0 {
		return Message{}, Stats{}, fmt.Errorf("the model returned no choices")
	}
	st := statsOf(out)
	c := out.Choices[0].Message
	// Empty content beside a full chain of thought means enable_thinking did
	// not take. Loud is better than a blank bubble.
	if strings.TrimSpace(c.Content) == "" && len(c.ToolCalls) == 0 && strings.TrimSpace(c.Reasoning) != "" {
		return Message{}, st, fmt.Errorf("the model returned only reasoning and no answer")
	}
	return Message{Role: RoleAssistant, Content: c.Content, ToolCalls: c.ToolCalls}, st, nil
}

// Structured constrains an answer to a JSON schema, which llama.cpp compiles to
// a GBNF grammar and samples against, so a field declared as an enum cannot come
// back as anything else. Temperature is low because these steps are decisions
// rather than prose.
func (l *LLM) Structured(ctx context.Context, msgs []Message, maxTok int, schema, out any) (Stats, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return Stats{}, err
	}
	req := l.base(msgs, maxTok)
	req.Temperature = 0.2
	req.ResponseFormat = &responseFormat{Type: "json_schema",
		JSONSchema: &schemaWrapper{Name: "response", Strict: true, Schema: raw}}

	var resp chatResp
	if err := l.post(ctx, req, &resp); err != nil {
		return Stats{}, err
	}
	if len(resp.Choices) == 0 {
		return Stats{}, fmt.Errorf("the model returned no choices")
	}
	st := statsOf(resp)
	text := strings.TrimSpace(resp.Choices[0].Message.Content)
	// A model that opens with a word before the object is still constrained to
	// emit one, so find it rather than failing the whole step.
	if i := strings.Index(text, "{"); i > 0 {
		text = text[i:]
	}
	return st, json.Unmarshal([]byte(text), out)
}

func statsOf(r chatResp) Stats {
	s := Stats{Prompt: r.Usage.PromptTokens, Completion: r.Usage.CompletionTokens,
		Decode: r.Timings.PredictedPerSec, Prefill: r.Timings.PromptPerSecond}
	if s.Prompt == 0 {
		s.Prompt = r.Timings.PromptN
	}
	if s.Completion == 0 {
		s.Completion = r.Timings.PredictedN
	}
	return s
}

// Stream asks for the final answer and calls onDelta as text arrives. Tools are
// deliberately not offered here: by this point the turn is answering.
func (l *LLM) Stream(ctx context.Context, msgs []Message, maxTok int, onDelta func(string)) (string, Stats, error) {
	req := l.base(msgs, maxTok)
	req.Stream = true
	// llama.cpp only reports its timings on a stream when asked.
	req.StreamOptions = map[string]any{"include_usage": true}
	req.TimingsPerToken = true
	body, err := json.Marshal(req)
	if err != nil {
		return "", Stats{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, "POST", l.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", Stats{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	l.sign(hr)
	resp, err := l.http.Do(hr)
	if err != nil {
		return "", Stats{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", Stats{}, modelError(resp.StatusCode, resp.Body)
	}
	var st Stats
	var sb strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ev chatResp
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		// The last chunk carries the usage and timings and no choices, which
		// is why this is read before the choices check rather than after.
		if got := statsOf(ev); got.Prompt > 0 || got.Decode > 0 || got.Completion > 0 {
			st.merge(got)
		}
		if len(ev.Choices) == 0 {
			continue
		}
		if d := ev.Choices[0].Delta.Content; d != "" {
			sb.WriteString(d)
			onDelta(d)
		}
	}
	if err := sc.Err(); err != nil {
		return sb.String(), st, err
	}
	return sb.String(), st, nil
}

// sign puts the gateway key on a request, and the incognito header when the
// turn is one. An empty key means talking straight to a llama-swap with no
// gateway in front, which is what a bare development run is.
func (l *LLM) sign(r *http.Request) {
	if l.Key != "" {
		r.Header.Set("Authorization", "Bearer "+l.Key)
	}
	if IsIncognito(r.Context()) {
		r.Header.Set(incognitoHeader, "1")
	}
}

// The gateway writes down every prompt and completion it forwards, so a mode
// that writes nothing down here has to say so there as well.
const incognitoHeader = "X-Incognito"

type incognitoKey struct{}

// WithIncognito marks a context as belonging to an incognito turn. It rides the
// context rather than the client because the client is shared by every turn,
// and this way the gate, the compaction pass and the tools all inherit it.
func WithIncognito(ctx context.Context) context.Context {
	return context.WithValue(ctx, incognitoKey{}, true)
}

func IsIncognito(ctx context.Context) bool {
	on, _ := ctx.Value(incognitoKey{}).(bool)
	return on
}

func (l *LLM) post(ctx context.Context, req chatReq, out *chatResp) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	hr, err := http.NewRequestWithContext(ctx, "POST", l.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	hr.Header.Set("Content-Type", "application/json")
	l.sign(hr)
	resp, err := l.http.Do(hr)
	if err != nil {
		return fmt.Errorf("the model is not answering: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return modelError(resp.StatusCode, resp.Body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Warm fires a one token completion so llama-swap loads the weights while the
// user is still typing. Asking /v1/models would not do it, since that answers
// from config and loads nothing.
func (l *LLM) Warm(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var out chatResp
	_ = l.post(ctx, chatReq{Model: l.Model, MaxTokens: 1,
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Kwargs:   map[string]any{"enable_thinking": false}}, &out)
}

// Healthy asks whether the server is up without waking the model, because
// /v1/models answers from llama-swap's config and loads no weights. Never point
// this at /health, which would defeat the idle unload.
func (l *LLM) Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", l.BaseURL+"/v1/models", nil)
	if err != nil {
		return false
	}
	l.sign(req)
	resp, err := l.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// modelError carries the reason back rather than the number. A bare "the model
// answered 500" is unactionable, and the body always says whether it was the
// context, the template or the request.
func modelError(status int, body io.Reader) error {
	raw, _ := io.ReadAll(io.LimitReader(body, 32<<10))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && strings.TrimSpace(e.Error.Message) != "" {
		return fmt.Errorf("the model refused this turn: %s", strings.TrimSpace(e.Error.Message))
	}
	if txt := strings.TrimSpace(string(raw)); txt != "" {
		return fmt.Errorf("the model answered %d: %s", status, trimLine(txt, 300))
	}
	return fmt.Errorf("the model answered %d with no reason", status)
}

func trimLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
