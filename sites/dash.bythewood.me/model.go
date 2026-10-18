package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	gate := gateOf(ctx)
	if gate != nil {
		req.Header.Set("X-Background", "1")
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if busy := cardBusy(resp, msg); busy != nil && gate != nil {
			gate.refused(*busy)
			return busy
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

	text := strings.TrimSpace(r.Choices[0].Message.Content)
	if i := strings.Index(text, "{"); i > 0 {
		text = text[i:]
	}
	return json.Unmarshal([]byte(text), out)
}

// GPUState is the gateway's read of the card. RetryAt is set when it knows how
// long the card is spoken for, like the wait after the gateway starts.
type GPUState struct {
	Busy    bool   `json:"busy"`
	Reason  string `json:"reason"`
	Loaded  bool   `json:"loaded"`
	RetryAt int64  `json:"retry_at"`
	UtilAvg int    `json:"util_avg"`
	UsedMiB int    `json:"used_mib"`
}

// CardBusy is the gateway turning a scheduled call away because the desktop
// took the card partway through a run.
type CardBusy struct {
	Until  time.Time
	Reason string
}

func (c *CardBusy) Error() string { return "model: card in use: " + c.Reason }

func cardBusy(resp *http.Response, body []byte) *CardBusy {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if resp.StatusCode != http.StatusServiceUnavailable || json.Unmarshal(body, &e) != nil || e.Error.Type != "card_busy" {
		return nil
	}
	wait := briefDefer
	if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 {
		wait = time.Duration(n) * time.Second
	}
	return &CardBusy{Until: time.Now().Add(wait), Reason: e.Error.Message}
}

// runGate rides a scheduled run's context, so its calls are marked background
// and the first one turned away cancels the rest.
type runGate struct {
	stop context.CancelFunc
	mu   sync.Mutex
	busy *CardBusy
}

type runGateKey struct{}

func scheduledRun(ctx context.Context) (context.Context, *runGate) {
	ctx, cancel := context.WithCancel(ctx)
	g := &runGate{stop: cancel}
	return context.WithValue(ctx, runGateKey{}, g), g
}

func gateOf(ctx context.Context) *runGate {
	g, _ := ctx.Value(runGateKey{}).(*runGate)
	return g
}

func (g *runGate) refused(b CardBusy) {
	g.mu.Lock()
	if g.busy == nil {
		g.busy = &b
	}
	g.mu.Unlock()
	g.stop()
}

func (g *runGate) Busy() *CardBusy {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.busy
}

func (m *Model) GPU(ctx context.Context) (GPUState, error) {
	var g GPUState
	err := m.call(ctx, http.MethodGet, "/v1/gpu", &g)
	return g, err
}

// Unload takes the weights off now instead of at the end of the gateway's idle
// timer, so a game started straight after a brief has the card.
func (m *Model) Unload(ctx context.Context) error {
	return m.call(ctx, http.MethodPost, "/v1/unload", nil)
}

func (m *Model) call(ctx context.Context, method, path string, into any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, m.url+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.key)
	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("model: %s %s: http %d", method, path, resp.StatusCode)
	}
	if into == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(into)
}
