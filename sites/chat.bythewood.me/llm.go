// The model client. The gateway speaks the OpenAI chat completions shape and
// turns it into a Claude call, so this is that shape and nothing more.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"chat.bythewood.me/internal/incognito"
	"chat.bythewood.me/tools"
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
	return &LLM{
		BaseURL: strings.TrimRight(base, "/"),
		Model:   model,
		Key:     key,
		// The context deadline on the request is the real limit, this only
		// catches a connection that never answers at all.
		http: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Sampling fields are left out since Haiku 5.5 refuses anything but its
// defaults, and the gateway turns thinking off by itself.
type chatReq struct {
	Model          string           `json:"model"`
	Messages       []Message        `json:"messages"`
	Tools          []map[string]any `json:"tools,omitempty"`
	MaxTokens      int              `json:"max_tokens"`
	Stream         bool             `json:"stream,omitempty"`
	ResponseFormat *responseFormat  `json:"response_format,omitempty"`
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

// callDelta is a piece of a tool call on a stream. The first piece for an index
// carries the id and the name and the rest carry more of the arguments.
type callDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatResp struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Content   string      `json:"content"`
			ToolCalls []callDelta `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Timings struct {
		PredictedPerSec float64 `json:"predicted_per_second"`
	} `json:"timings"`
}

// Stats is what one turn cost, for the meter in the bar. Prompt is the whole
// window the model was handed, so it says how full the context is and not how
// long the last message was.
type Stats struct {
	Prompt     int     `json:"prompt_tokens"`
	Completion int     `json:"completion_tokens"`
	Decode     float64 `json:"decode_tps"`
	// The reply stopped at max_tokens. It belongs to one call and is never merged.
	Truncated bool `json:"-"`
}

func (s *Stats) merge(o Stats) {
	if o.Prompt > s.Prompt {
		s.Prompt = o.Prompt
	}
	s.Completion += o.Completion
	if o.Decode > 0 {
		s.Decode = o.Decode
	}
}

func statsOf(r chatResp) Stats {
	return Stats{Prompt: r.Usage.PromptTokens, Completion: r.Usage.CompletionTokens, Decode: r.Timings.PredictedPerSec}
}

// errDeclined is the model turning a question down, which the gateway sends as
// a 422 on a plain call and as a content_filter finish on a stream.
var errDeclined = errors.New("the model declined to answer that")

// Complete asks one thing with no tools and no stream, for the small jobs
// around a turn.
func (l *LLM) Complete(ctx context.Context, msgs []Message, maxTok int) (string, error) {
	var out chatResp
	if err := l.post(ctx, chatReq{Model: l.Model, Messages: msgs, MaxTokens: maxTok}, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("the model returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// Structured constrains an answer to a JSON schema, which the gateway hands to
// Claude as structured outputs, so the reply parses or the call fails.
func (l *LLM) Structured(ctx context.Context, msgs []Message, maxTok int, schema, out any) (Stats, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return Stats{}, err
	}
	req := chatReq{Model: l.Model, Messages: msgs, MaxTokens: maxTok,
		ResponseFormat: &responseFormat{Type: "json_schema",
			JSONSchema: &schemaWrapper{Name: "response", Strict: true, Schema: raw}}}
	var resp chatResp
	if err := l.post(ctx, req, &resp); err != nil {
		return Stats{}, err
	}
	if len(resp.Choices) == 0 {
		return Stats{}, fmt.Errorf("the model returned no choices")
	}
	return statsOf(resp), json.Unmarshal([]byte(resp.Choices[0].Message.Content), out)
}

// Turn streams one round of a conversation with the tools on offer, calling
// onText as prose arrives, and hands back whatever calls the round made.
func (l *LLM) Turn(ctx context.Context, msgs []Message, schemas []map[string]any, maxTok int, onText func(string)) (Message, Stats, error) {
	reply := Message{Role: RoleAssistant}
	body, err := json.Marshal(chatReq{Model: l.Model, Messages: msgs, Tools: schemas, MaxTokens: maxTok, Stream: true})
	if err != nil {
		return reply, Stats{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, "POST", l.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return reply, Stats{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	l.sign(hr)
	resp, err := l.http.Do(hr)
	if err != nil {
		return reply, Stats{}, fmt.Errorf("the model is not answering: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return reply, Stats{}, modelError(resp.StatusCode, resp.Body)
	}

	var st Stats
	var text strings.Builder
	var calls []ToolCall
	finish := ""
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
		// The usage comes on a chunk of its own with no choices.
		if got := statsOf(ev); got.Prompt > 0 || got.Completion > 0 {
			st = got
		}
		if len(ev.Choices) == 0 {
			continue
		}
		c := ev.Choices[0]
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
		if d := c.Delta.Content; d != "" {
			text.WriteString(d)
			if onText != nil {
				onText(d)
			}
		}
		for _, d := range c.Delta.ToolCalls {
			for len(calls) <= d.Index {
				calls = append(calls, ToolCall{Type: "function"})
			}
			tc := &calls[d.Index]
			if d.ID != "" {
				tc.ID = d.ID
			}
			if d.Function.Name != "" {
				tc.Function.Name = d.Function.Name
			}
			tc.Function.Arguments += d.Function.Arguments
		}
	}
	reply.Content, reply.ToolCalls = text.String(), calls
	st.Truncated = finish == "length"
	if err := sc.Err(); err != nil {
		return reply, st, err
	}
	if finish == "content_filter" {
		return reply, st, errDeclined
	}
	return reply, st, nil
}

// sign puts the gateway key on a request, and the incognito header when the
// turn is one.
func (l *LLM) sign(r *http.Request) {
	if l.Key != "" {
		r.Header.Set("Authorization", "Bearer "+l.Key)
	}
	if incognito.On(r.Context()) {
		r.Header.Set(incognitoHeader, "1")
	}
}

// The gateway writes down every prompt and completion it forwards, so a mode
// that writes nothing down here has to say so there as well.
const incognitoHeader = "X-Incognito"

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

// modelError carries the reason back instead of the bare status, since the page
// shows it and "400" tells nobody anything.
func modelError(status int, body io.Reader) error {
	if status == http.StatusUnprocessableEntity {
		return errDeclined
	}
	raw, _ := io.ReadAll(io.LimitReader(body, 32<<10))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && strings.TrimSpace(e.Error.Message) != "" {
		return fmt.Errorf("the model refused this turn: %s", trimLine(e.Error.Message, 300))
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

func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n-1]) + "…"
}

// Search asks the gateway for web results, which it gets from Anthropic's
// search. An error here falls back to DuckDuckGo in the tool.
func (l *LLM) Search(ctx context.Context, query string, domains []string, n int) ([]tools.SearchHit, error) {
	body, err := json.Marshal(map[string]any{"query": query, "domains": domains, "n": n})
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, "POST", l.BaseURL+"/v1/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	l.sign(hr)
	resp, err := l.http.Do(hr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, modelError(resp.StatusCode, resp.Body)
	}
	var out struct {
		Results []tools.SearchHit `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out.Results, nil
}
