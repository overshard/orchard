// With ANTHROPIC_API_KEY set the gateway answers from Claude instead of the card.
// Callers keep speaking the OpenAI shape, so this translates each call to the
// Messages API and the answer back, and nothing past the gateway changes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

const defaultClaudeModel = "claude-haiku-5-5"

// The callers' turn budgets were sized for Ornith, so a request that names none
// still needs one since the Messages API has no default.
const defaultMaxTokens = 4096

// A caller's max_tokens is clamped to this, so a leaked key can't ask for the
// most expensive answer the model will give on every call.
const maxTokensCeiling = 16384

// A web search turn can pause partway and has to be sent back to carry on.
const maxPauses = 4

type claude struct {
	client anthropic.Client
	model  string
}

func newClaude(key, model string, opts ...option.RequestOption) *claude {
	opts = append([]option.RequestOption{option.WithAPIKey(key)}, opts...)
	return &claude{client: anthropic.NewClient(opts...), model: model}
}

// oaReq is the part of an OpenAI chat completion this translates. Sampling
// fields are read and dropped, since Haiku 5.5 refuses any non default value.
type oaReq struct {
	Model               string          `json:"model"`
	Stream              bool            `json:"stream"`
	Messages            []oaMsg         `json:"messages"`
	Tools               []oaTool        `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	MaxTokens           int64           `json:"max_tokens"`
	MaxCompletionTokens int64           `json:"max_completion_tokens"`
	ResponseFormat      *struct {
		Type       string `json:"type"`
		JSONSchema *struct {
			Schema map[string]any `json:"schema"`
		} `json:"json_schema"`
	} `json:"response_format"`
}

type oaMsg struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []oaToolCall    `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
}

type oaToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaTool struct {
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// errBadRequest is a call that can't be translated, which is the caller's to
// fix and not an outage.
var errBadRequest = errors.New("bad request")

// params builds the Messages API call. Web search is offered beside the
// caller's own tools when it isn't streaming, since a search can pause the turn
// and only the plain path sends it back to finish.
func (c *claude) params(req oaReq) (anthropic.MessageNewParams, error) {
	p := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: defaultMaxTokens,
		// Every caller here asks for thinking off, and a thinking block can't
		// survive the trip through the OpenAI shape and back.
		Thinking:     anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}},
		CacheControl: anthropic.NewCacheControlEphemeralParam(),
	}
	if req.MaxTokens > 0 {
		p.MaxTokens = req.MaxTokens
	} else if req.MaxCompletionTokens > 0 {
		p.MaxTokens = req.MaxCompletionTokens
	}
	p.MaxTokens = min(p.MaxTokens, maxTokensCeiling)

	system, msgs := translateMessages(req.Messages)
	if len(msgs) == 0 {
		return p, fmt.Errorf("%w: no messages past the system prompt", errBadRequest)
	}
	if len(system) > 0 {
		system[len(system)-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	p.System, p.Messages = system, msgs

	choice := toolChoice(req.ToolChoice)
	if len(req.Tools) > 0 && choice != "none" {
		ownSearch := false
		for _, t := range req.Tools {
			ownSearch = ownSearch || t.Function.Name == "web_search"
			tp := anthropic.ToolParam{Name: t.Function.Name, InputSchema: inputSchema(t.Function.Parameters)}
			if t.Function.Description != "" {
				tp.Description = anthropic.String(t.Function.Description)
			}
			p.Tools = append(p.Tools, anthropic.ToolUnionParam{OfTool: &tp})
		}
		// The API refuses two tools with one name, so a caller with its own
		// web_search keeps it.
		if !req.Stream && !ownSearch {
			p.Tools = append(p.Tools, anthropic.ToolUnionParam{OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{
				MaxUses: anthropic.Int(3),
			}})
		}
		switch choice {
		case "required":
			p.ToolChoice = anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}
		case "auto", "":
		default:
			p.ToolChoice = anthropic.ToolChoiceParamOfTool(choice)
		}
	}

	if f := req.ResponseFormat; f != nil && f.Type == "json_schema" && f.JSONSchema != nil && f.JSONSchema.Schema != nil {
		p.OutputConfig.Format = anthropic.JSONOutputFormatParam{Schema: strictSchema(f.JSONSchema.Schema)}
	}
	return p, nil
}

// translateMessages splits off the leading system prompt and folds the rest
// into alternating turns, since the Messages API wants a tool's results in the
// user turn after the call and consecutive turns from one side merged.
func translateMessages(in []oaMsg) ([]anthropic.TextBlockParam, []anthropic.MessageParam) {
	var system []anthropic.TextBlockParam
	var out []anthropic.MessageParam
	add := func(assistant bool, blocks ...anthropic.ContentBlockParamUnion) {
		if len(blocks) == 0 {
			return
		}
		if n := len(out); n > 0 {
			if last := out[n-1]; assistant && last.OfAssistant != nil {
				last.OfAssistant.Content = append(last.OfAssistant.Content, blocks...)
				return
			} else if !assistant && last.OfUser != nil {
				last.OfUser.Content = append(last.OfUser.Content, blocks...)
				return
			}
		}
		if assistant {
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		} else {
			out = append(out, anthropic.NewUserMessage(blocks...))
		}
	}
	for _, m := range in {
		text := contentText(m.Content)
		switch m.Role {
		case "system", "developer":
			if strings.TrimSpace(text) == "" {
				continue
			}
			if len(out) == 0 {
				system = append(system, anthropic.TextBlockParam{Text: text})
				continue
			}
			out = append(out, anthropic.MessageParam{OfSystem: &anthropic.SystemMessageParam{
				Content: []anthropic.TextBlockParam{{Text: text}}}})
		case "assistant":
			var blocks []anthropic.ContentBlockParamUnion
			if strings.TrimSpace(text) != "" {
				blocks = append(blocks, anthropic.NewTextBlock(text))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(toolID(tc.ID), toolInput(tc.Function.Arguments), tc.Function.Name))
			}
			add(true, blocks...)
		case "tool":
			if text == "" {
				text = "(no output)"
			}
			add(false, anthropic.NewToolResultBlock(toolID(m.ToolCallID), text, false))
		default:
			if strings.TrimSpace(text) != "" {
				add(false, anthropic.NewTextBlock(text))
			}
		}
	}
	return system, out
}

// contentText reads a message's content as a string or as the parts array,
// keeping the text parts.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

var badIDChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// toolID makes llama.cpp's call ids legal for the Messages API, which only takes
// letters, digits, dashes and underscores. A call and its result go through the
// same mapping so they still match.
func toolID(id string) string {
	if id == "" {
		return "call"
	}
	return badIDChars.ReplaceAllString(id, "_")
}

// toolInput passes a call's arguments through when they're a JSON object, since
// the API refuses a tool_use whose input is anything else.
func toolInput(args string) json.RawMessage {
	raw := json.RawMessage(strings.TrimSpace(args))
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// toolChoice reads both spellings, the bare string and the object naming one
// function, and hands back the mode or the function's name.
func toolChoice(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &o) == nil && o.Function.Name != "" {
		return o.Function.Name
	}
	return ""
}

func inputSchema(p map[string]any) anthropic.ToolInputSchemaParam {
	s := anthropic.ToolInputSchemaParam{Properties: p["properties"], ExtraFields: map[string]any{}}
	if req, ok := p["required"].([]any); ok {
		for _, r := range req {
			if name, ok := r.(string); ok {
				s.Required = append(s.Required, name)
			}
		}
	}
	for k, v := range p {
		switch k {
		case "type", "properties", "required":
		default:
			s.ExtraFields[k] = v
		}
	}
	if s.Properties == nil {
		s.Properties = map[string]any{}
	}
	return s
}

// Structured outputs refuse these keywords, where llama.cpp's grammar honoured
// them. The prompts already say how many and how long, so they're dropped.
var unsupportedSchemaKeys = []string{
	"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf",
	"minLength", "maxLength", "pattern", "maxItems", "uniqueItems",
}

// strictSchema copies a schema into the subset structured outputs accept, with
// every object closed since the API requires additionalProperties false.
func strictSchema(v map[string]any) map[string]any {
	out := make(map[string]any, len(v))
	for k, val := range v {
		out[k] = strictValue(val)
	}
	for _, k := range unsupportedSchemaKeys {
		delete(out, k)
	}
	// minItems of 0 or 1 is all the API takes.
	if n, ok := out["minItems"].(float64); ok && n > 1 {
		out["minItems"] = 1
	}
	if out["type"] == "object" {
		out["additionalProperties"] = false
	}
	return out
}

func strictValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return strictSchema(t)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = strictValue(x)
		}
		return out
	}
	return v
}

// finishReason is the OpenAI spelling of why the model stopped.
func finishReason(r anthropic.StopReason) string {
	switch r {
	case anthropic.StopReasonToolUse:
		return "tool_calls"
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		return "length"
	case anthropic.StopReasonRefusal:
		return "content_filter"
	}
	return "stop"
}

// logUsage keeps the cache split per call, since the cost alone can't show
// whether the prompt cache is hitting.
func logUsage(u anthropic.Usage) {
	slog.Info("claude usage", "input", u.InputTokens, "cache_write", u.CacheCreationInputTokens,
		"cache_read", u.CacheReadInputTokens, "output", u.OutputTokens)
}

// promptTokens is the whole window the model read, cached or not, which is what
// the callers' meters mean by prompt.
func promptTokens(u anthropic.Usage) int {
	return int(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens)
}

// answer is one finished turn read out of the Messages API's blocks.
type answer struct {
	text    string
	calls   []oaToolCall
	stop    anthropic.StopReason
	refusal string
	usage   Call
}

// complete runs one turn, sending it back while a web search has it paused.
func (c *claude) complete(ctx context.Context, p anthropic.MessageNewParams) (answer, error) {
	var blocks []anthropic.ContentBlockUnion
	var a answer
	for range maxPauses + 1 {
		resp, err := c.client.Messages.New(ctx, p)
		if err != nil {
			return a, err
		}
		blocks = append(blocks, resp.Content...)
		a.usage.addUsage(resp.Usage)
		a.stop = resp.StopReason
		if resp.StopReason == anthropic.StopReasonRefusal {
			a.refusal = string(resp.StopDetails.Category)
		}
		if resp.StopReason != anthropic.StopReasonPauseTurn {
			break
		}
		p.Messages = append(p.Messages, resp.ToParam())
	}

	// The text before a search is the model saying it's going to look, so the
	// answer is what it wrote after the last results came back.
	start := 0
	for i, b := range blocks {
		if b.Type == "web_search_tool_result" {
			start = i + 1
		}
	}
	var text strings.Builder
	for i, b := range blocks {
		switch v := b.AsAny().(type) {
		case anthropic.TextBlock:
			if i >= start {
				text.WriteString(v.Text)
			}
		case anthropic.ToolUseBlock:
			tc := oaToolCall{ID: v.ID, Type: "function"}
			tc.Function.Name = v.Name
			tc.Function.Arguments = v.JSON.Input.Raw()
			a.calls = append(a.calls, tc)
		}
	}
	a.text = text.String()
	return a, nil
}

// viaClaude answers a chat completion from Claude. It fills in the call row the
// way the llama.cpp path does, so the log reads the same either way.
func (s *site) viaClaude(w http.ResponseWriter, r *http.Request, body []byte, call *Call) {
	call.Model = s.claude.model
	if r.URL.Path != "/v1/chat/completions" {
		call.Status = http.StatusNotFound
		writeOAError(w, http.StatusNotFound, "invalid_request_error", "only chat completions are served while the gateway runs on Claude")
		return
	}
	var req oaReq
	if err := json.Unmarshal(body, &req); err != nil {
		call.Status, call.Err = http.StatusBadRequest, err.Error()
		writeOAError(w, http.StatusBadRequest, "invalid_request_error", "the body is not a chat completion: "+err.Error())
		return
	}
	p, err := s.claude.params(req)
	if err != nil {
		call.Status, call.Err = http.StatusBadRequest, err.Error()
		writeOAError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if req.Stream {
		s.claudeStream(w, r, p, call)
		return
	}

	a, err := s.claude.complete(r.Context(), p)
	if err != nil {
		s.claudeFailed(w, err, call)
		return
	}
	call.PromptTok, call.OutputTok, call.Cost, call.tokenSplit = a.usage.PromptTok, a.usage.OutputTok, a.usage.Cost, a.usage.tokenSplit
	call.Completion = a.text
	if call.Completion == "" && len(a.calls) > 0 {
		call.Completion = "[tool calls: " + string(mustJSON(a.calls)) + "]"
	}
	if a.usage.Searches > 0 {
		call.Tools = strings.TrimSpace(call.Tools + fmt.Sprintf(", %d web searches", a.usage.Searches))
		call.Tools = strings.TrimPrefix(call.Tools, ", ")
	}
	if a.stop == anthropic.StopReasonRefusal {
		refused(w, a.refusal, call)
		return
	}
	call.Status = http.StatusOK

	msg := map[string]any{"role": "assistant", "content": a.text}
	if len(a.calls) > 0 {
		msg["tool_calls"] = a.calls
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-claude", "object": "chat.completion", "created": time.Now().Unix(), "model": s.claude.model,
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finishReason(a.stop)}},
		"usage": map[string]int{"prompt_tokens": call.PromptTok, "completion_tokens": call.OutputTok,
			"total_tokens": call.PromptTok + call.OutputTok},
	})
}

// claudeStream relays a streamed answer as OpenAI chunks. Headers wait for the
// first event, so a call the API turns away still gets a real status.
func (s *site) claudeStream(w http.ResponseWriter, r *http.Request, p anthropic.MessageNewParams, call *Call) {
	stream := s.claude.client.Messages.NewStreaming(r.Context(), p)
	defer stream.Close()
	rc := http.NewResponseController(w)
	created := time.Now().Unix()
	started := false
	send := func(delta map[string]any, finish any, extra map[string]any) {
		if !started {
			started = true
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
		}
		chunk := map[string]any{"id": "chatcmpl-claude", "object": "chat.completion.chunk",
			"created": created, "model": s.claude.model, "choices": []map[string]any{}}
		if delta != nil {
			chunk["choices"] = []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}
		}
		maps.Copy(chunk, extra)
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(chunk))
		_ = rc.Flush()
	}

	var msg anthropic.Message
	var text strings.Builder
	var calls []*oaToolCall
	toolAt := map[int64]int{}
	var first time.Time
	for stream.Next() {
		ev := stream.Current()
		_ = msg.Accumulate(ev)
		switch v := ev.AsAny().(type) {
		case anthropic.MessageStartEvent:
			send(map[string]any{"role": "assistant", "content": ""}, nil, nil)
		case anthropic.ContentBlockStartEvent:
			if v.ContentBlock.Type == "tool_use" {
				i := len(calls)
				toolAt[v.Index] = i
				tc := &oaToolCall{Index: &i, ID: v.ContentBlock.ID, Type: "function"}
				tc.Function.Name = v.ContentBlock.Name
				calls = append(calls, tc)
				send(map[string]any{"tool_calls": []*oaToolCall{tc}}, nil, nil)
			}
		case anthropic.ContentBlockDeltaEvent:
			if first.IsZero() {
				first = time.Now()
			}
			switch d := v.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				text.WriteString(d.Text)
				send(map[string]any{"content": d.Text}, nil, nil)
			case anthropic.InputJSONDelta:
				i, ok := toolAt[v.Index]
				if !ok {
					continue
				}
				calls[i].Function.Arguments += d.PartialJSON
				frag := oaToolCall{Index: &i}
				frag.Function.Arguments = d.PartialJSON
				send(map[string]any{"tool_calls": []oaToolCall{frag}}, nil, nil)
			}
		}
	}
	if err := stream.Err(); err != nil {
		if !started {
			s.claudeFailed(w, err, call)
			return
		}
		call.Status, call.Err = http.StatusBadGateway, err.Error()
		slog.Error("the claude stream broke", "err", err, "caller", call.Caller)
	} else {
		call.Status = http.StatusOK
	}

	call.addUsage(msg.Usage)
	if !first.IsZero() && !call.started.IsZero() {
		call.FirstMS = first.Sub(call.started).Milliseconds()
	}
	if !first.IsZero() && call.OutputTok > 0 {
		if secs := time.Since(first).Seconds(); secs > 0 {
			call.DecodeTPS = float64(call.OutputTok) / secs
		}
	}
	call.Completion = text.String()
	if call.Completion == "" && len(calls) > 0 {
		call.Completion = "[tool calls: " + string(mustJSON(calls)) + "]"
	}
	if msg.StopReason == anthropic.StopReasonRefusal {
		call.Err = "refusal: " + string(msg.StopDetails.Category)
	}
	send(map[string]any{}, finishReason(msg.StopReason), nil)
	send(nil, nil, map[string]any{
		"usage": map[string]int{"prompt_tokens": call.PromptTok, "completion_tokens": call.OutputTok,
			"total_tokens": call.PromptTok + call.OutputTok},
		"timings": map[string]any{"prompt_n": call.PromptTok, "predicted_n": call.OutputTok,
			"predicted_per_second": call.DecodeTPS},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	_ = rc.Flush()
}

// claudeFailed passes the API's status and message on, in the error shape the
// callers already read.
func (s *site) claudeFailed(w http.ResponseWriter, err error, call *Call) {
	status, msg := http.StatusBadGateway, err.Error()
	if apiErr, ok := errors.AsType[*anthropic.Error](err); ok {
		status = apiErr.StatusCode
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(apiErr.RawJSON()), &body) == nil && body.Error.Message != "" {
			msg = body.Error.Message
		}
	}
	call.Status, call.Err = status, msg
	slog.Error("claude refused a call", "status", status, "caller", call.Caller, "reason", msg)
	writeOAError(w, status, "upstream_error", msg)
}

// refused is a safety decline, sent as an error so a caller says something
// went wrong instead of showing an empty answer.
func refused(w http.ResponseWriter, category string, call *Call) {
	call.Status = http.StatusUnprocessableEntity
	call.Err = "refusal: " + category
	writeOAError(w, http.StatusUnprocessableEntity, "refusal", "the model declined to answer ("+category+")")
}

func writeOAError(w http.ResponseWriter, status int, kind, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": msg, "type": kind}})
}
