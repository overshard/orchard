package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A small client for the Messages API, which is all llm calls. The Go SDK
// needs more than 2GB to compile, more than the dev container or a build on
// the VPS can spare.

const apiVersion = "2026-10-21"

type claude struct {
	key, model, baseURL string
	retries             int
	http                *http.Client
}

func newClaude(key, model string) *claude {
	return &claude{key: key, model: model, baseURL: "https://api.anthropic.com", retries: 2,
		http: &http.Client{Timeout: 10 * time.Minute}}
}

type apiUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	ServerToolUse            struct {
		WebSearchRequests int64 `json:"web_search_requests"`
	} `json:"server_tool_use"`
}

type cacheControl struct {
	Type string `json:"type"`
}

var ephemeral = &cacheControl{Type: "ephemeral"}

type msgParams struct {
	Model        string            `json:"model"`
	MaxTokens    int64             `json:"max_tokens"`
	Thinking     map[string]string `json:"thinking,omitempty"`
	CacheControl *cacheControl     `json:"cache_control,omitempty"`
	System       []textBlock       `json:"system,omitempty"`
	Messages     []message         `json:"messages"`
	Tools        []any             `json:"tools,omitempty"`
	ToolChoice   map[string]string `json:"tool_choice,omitempty"`
	OutputConfig map[string]any    `json:"output_config,omitempty"`
	Stream       bool              `json:"stream,omitempty"`
}

// Content holds the blocks below, or blocks from a response sent back as is.
type message struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type textBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type toolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

func newText(s string) textBlock { return textBlock{Type: "text", Text: s} }

type webSearchTool struct {
	Type           string   `json:"type"`
	Name           string   `json:"name"`
	MaxUses        int      `json:"max_uses"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
}

func newWebSearch(maxUses int, domains []string) webSearchTool {
	return webSearchTool{Type: "web_search_20250305", Name: "web_search", MaxUses: maxUses, AllowedDomains: domains}
}

type response struct {
	Content     []json.RawMessage `json:"content"`
	StopReason  string            `json:"stop_reason"`
	StopDetails struct {
		Category string `json:"category"`
	} `json:"stop_details"`
	Usage apiUsage `json:"usage"`
}

// toParam is the response as the assistant turn that carries a paused one on.
func (r response) toParam() message {
	m := message{Role: "assistant"}
	for _, b := range r.Content {
		m.Content = append(m.Content, b)
	}
	return m
}

// respBlock is the part of any content block this reads.
type respBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	Citations []struct {
		URL       string `json:"url"`
		CitedText string `json:"cited_text"`
	} `json:"citations"`
}

func readBlocks(raw []json.RawMessage) []respBlock {
	out := make([]respBlock, len(raw))
	for i, b := range raw {
		_ = json.Unmarshal(b, &out[i])
	}
	return out
}

type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return fmt.Sprintf("%d %s", e.status, e.msg) }

func (c *claude) newMessage(ctx context.Context, p msgParams) (response, error) {
	var r response
	res, err := c.post(ctx, p)
	if err != nil {
		return r, err
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return r, fmt.Errorf("reading the answer: %w", err)
	}
	return r, nil
}

// post sends a Messages call, retrying what the API says is worth retrying the
// way the SDK did, and turns any other status into an apiError.
func (c *claude) post(ctx context.Context, p msgParams) (*http.Response, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.baseURL, "/")+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", c.key)
		req.Header.Set("anthropic-version", apiVersion)
		req.Header.Set("content-type", "application/json")
		res, err := c.http.Do(req)
		if err == nil && res.StatusCode < 300 {
			return res, nil
		}
		if attempt < c.retries && ctx.Err() == nil && worthRetrying(res) {
			wait := time.Duration(1<<attempt) * 500 * time.Millisecond
			if res != nil {
				if s, err := strconv.Atoi(res.Header.Get("retry-after")); err == nil && s > 0 && s <= 60 {
					wait = time.Duration(s) * time.Second
				}
				res.Body.Close()
			}
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := strings.TrimSpace(string(raw))
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return nil, &apiError{status: res.StatusCode, msg: msg}
	}
}

func worthRetrying(res *http.Response) bool {
	if res == nil {
		return true
	}
	switch res.Header.Get("x-should-retry") {
	case "true":
		return true
	case "false":
		return false
	}
	s := res.StatusCode
	return s == http.StatusRequestTimeout || s == http.StatusConflict || s == http.StatusTooManyRequests || s >= 500
}

// streamEvent is the part of any stream event this reads.
type streamEvent struct {
	Type    string `json:"type"`
	Index   int64  `json:"index"`
	Message struct {
		Usage apiUsage `json:"usage"`
	} `json:"message"`
	ContentBlock respBlock `json:"content_block"`
	Delta        struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
		StopDetails struct {
			Category string `json:"category"`
		} `json:"stop_details"`
	} `json:"delta"`
	// message_delta leaves out the counts it isn't changing, so these are
	// pointers to tell a missing one from a zero.
	Usage struct {
		InputTokens              *int64 `json:"input_tokens"`
		CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
		OutputTokens             int64  `json:"output_tokens"`
		ServerToolUse            *struct {
			WebSearchRequests int64 `json:"web_search_requests"`
		} `json:"server_tool_use"`
	} `json:"usage"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// streamed is what a finished stream adds up to.
type streamed struct {
	usage      apiUsage
	stopReason string
	category   string
}

func (s *streamed) add(ev streamEvent) {
	switch ev.Type {
	case "message_start":
		s.usage = ev.Message.Usage
	case "message_delta":
		s.stopReason, s.category = ev.Delta.StopReason, ev.Delta.StopDetails.Category
		u := ev.Usage
		s.usage.OutputTokens = u.OutputTokens
		if u.InputTokens != nil {
			s.usage.InputTokens = *u.InputTokens
		}
		if u.CacheCreationInputTokens != nil {
			s.usage.CacheCreationInputTokens = *u.CacheCreationInputTokens
		}
		if u.CacheReadInputTokens != nil {
			s.usage.CacheReadInputTokens = *u.CacheReadInputTokens
		}
		if u.ServerToolUse != nil {
			s.usage.ServerToolUse.WebSearchRequests = u.ServerToolUse.WebSearchRequests
		}
	}
}

// stream sends a streaming call and hands each event to fn as it arrives.
func (c *claude) stream(ctx context.Context, p msgParams, fn func(streamEvent)) error {
	p.Stream = true
	res, err := c.post(ctx, p)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if d, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimPrefix(d, " "))
			continue
		}
		if line != "" || data.Len() == 0 {
			continue
		}
		var ev streamEvent
		err := json.Unmarshal([]byte(data.String()), &ev)
		data.Reset()
		if err != nil {
			return fmt.Errorf("reading the stream: %w", err)
		}
		if ev.Type == "error" {
			return fmt.Errorf("the stream failed: %s", ev.Error.Message)
		}
		fn(ev)
		if ev.Type == "message_stop" {
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}
