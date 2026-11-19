package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Model asks llm.bythewood.me to do a prompt job. The gateway adds web search to
// any call that offers a tool, so offering report is also what lets a check look
// something up.
type Model struct {
	url  string
	key  string
	http *http.Client
}

const modelTimeout = 3 * time.Minute

func NewModel(url, key string) *Model {
	if key == "" {
		return nil
	}
	return &Model{url: strings.TrimRight(url, "/"), key: key, http: &http.Client{Timeout: modelTimeout}}
}

// Answer is what a prompt job came back with. Notify false means there was
// nothing worth a push.
type Answer struct {
	Notify  bool   `json:"notify"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

const system = `You run scheduled checks for Isaac, who lives in North Carolina. It is %s Eastern.

Do the task you are given, searching the web whenever it needs anything current. Then call report exactly once.

Set notify to false only when the task says to stay quiet unless something is true and it isn't, or there's honestly nothing to tell him. Otherwise notify is true.

The message is read on a phone lock screen. Lead with the answer, keep it under 400 characters, plain text with no markdown, and include a link only if it's the one thing he'd tap. The title is a few words.`

var reportTool = map[string]any{
	"type": "function",
	"function": map[string]any{
		"name":        "report",
		"description": "Hand back the result of the check. Call it once, at the end.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"notify":  map[string]any{"type": "boolean", "description": "Whether this is worth a push to his phone"},
				"title":   map[string]any{"type": "string", "description": "A few words"},
				"message": map[string]any{"type": "string", "description": "The answer, under 400 characters, plain text"},
			},
			"required": []string{"notify", "title", "message"},
		},
	},
}

func (m *Model) Ask(ctx context.Context, task string, now time.Time) (Answer, error) {
	body, err := json.Marshal(map[string]any{
		"model": "claude-haiku-5-5",
		"messages": []map[string]string{
			{"role": "system", "content": fmt.Sprintf(system, now.Format("Monday January 2, 2006 3:04pm"))},
			{"role": "user", "content": task},
		},
		"tools":       []any{reportTool},
		"tool_choice": "auto",
		"max_tokens":  2000,
	})
	if err != nil {
		return Answer{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.key)

	resp, err := m.http.Do(req)
	if err != nil {
		return Answer{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Answer{}, fmt.Errorf("llm: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	return parseAnswer(raw)
}

func parseAnswer(raw []byte) (Answer, error) {
	var r struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Answer{}, fmt.Errorf("llm: %w", err)
	}
	if len(r.Choices) == 0 {
		return Answer{}, errors.New("llm: no answer came back")
	}
	msg := r.Choices[0].Message
	for _, c := range msg.ToolCalls {
		if c.Function.Name != "report" {
			continue
		}
		var a Answer
		if err := json.Unmarshal([]byte(c.Function.Arguments), &a); err != nil {
			return Answer{}, fmt.Errorf("llm: report didn't parse: %w", err)
		}
		a.Message = strings.TrimSpace(a.Message)
		if a.Notify && a.Message == "" {
			return Answer{}, errors.New("llm: asked to notify with an empty message")
		}
		return a, nil
	}
	// The model answered in prose instead of calling report, which is still
	// an answer worth having.
	if text := strings.TrimSpace(msg.Content); text != "" {
		return Answer{Notify: true, Message: text}, nil
	}
	return Answer{}, errors.New("llm: the answer was empty")
}
