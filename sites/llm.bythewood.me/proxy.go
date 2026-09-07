// The gateway itself: an OpenAI shaped endpoint in front of llama-swap that
// checks an API key, forwards the request, and writes down what was asked and
// what came back.
//
// It is a proxy and not a client library on purpose. Every caller here already
// speaks the OpenAI chat completions shape, so putting this in the path costs
// them a base url and a header rather than a rewrite, and anything that shape
// supports keeps working without this file knowing about it.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// The whole request body is held so it can be both forwarded and logged, so
// this is a real ceiling rather than a formality. A 64k window of text is a
// long way under it.
const maxRequestBytes = 32 << 20

// incognitoHeader is a caller saying this turn is not to be written down. Chat
// and search set it when the user is in incognito, and it is taken at its word
// because every caller here holds a key that was handed out by hand.
const incognitoHeader = "X-Incognito"

type upstreamReq struct {
	Model    string            `json:"model"`
	Stream   bool              `json:"stream"`
	Messages []json.RawMessage `json:"messages"`
	Tools    []json.RawMessage `json:"tools,omitempty"`
}

// authenticate resolves the bearer token on a request. Sessions do not work
// here: the callers are other containers with no browser and no cookie.
func (s *site) authenticate(r *http.Request) (Key, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		// llama.cpp's own clients send the key as an api key header, so both
		// spellings are accepted rather than making every caller special.
		if v := r.Header.Get("X-Api-Key"); v != "" {
			return s.store.Authenticate(strings.TrimSpace(v))
		}
		return Key{}, false
	}
	return s.store.Authenticate(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
}

func (s *site) requireKey(next func(http.ResponseWriter, *http.Request, Key)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		k, ok := s.authenticate(r)
		if !ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"a valid api key is required","type":"invalid_request_error"}}`))
			return
		}
		s.store.TouchKey(k.ID)
		next(w, r, k)
	}
}

// completions forwards one chat completion and logs it. Both shapes go through
// here, since a streamed answer has to be reassembled to be written down and a
// caller that streams is exactly the one whose output is worth keeping.
func (s *site) completions(w http.ResponseWriter, r *http.Request, k Key) {
	started := time.Now()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	var req upstreamReq
	_ = json.Unmarshal(body, &req)

	// A row with its text blanked would still say who asked something and when,
	// so incognito writes no row at all and the prompt is never copied out of
	// the body it arrived in.
	keep := r.Header.Get(incognitoHeader) != "1"

	call := Call{KeyID: k.ID, Caller: k.Name, Model: req.Model}
	if keep {
		call.Messages = string(mustJSON(req.Messages))
		if len(req.Tools) > 0 {
			call.Tools = fmt.Sprintf("%d offered", len(req.Tools))
		}
	}
	defer func() {
		if !keep {
			return
		}
		call.MS = time.Since(started).Milliseconds()
		s.store.LogCall(call)
	}()

	up, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.upstream+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		call.Err = err.Error()
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	up.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(up)
	if err != nil {
		call.Err = err.Error()
		call.Status = http.StatusBadGateway
		slog.Error("upstream refused", "err", err, "caller", k.Name)
		http.Error(w, "the model server is not answering", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	call.Status = resp.StatusCode

	for h, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(h, v)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)

	// An upstream refusal carries its reason in the body, and that body is the
	// one thing worth having when a turn fails. Without this the log says 500
	// and nothing else, which is what made a template error look like a size
	// limit for an afternoon.
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_, _ = w.Write(body)
		call.Err = upstreamReason(body)
		slog.Error("upstream refused a call", "status", resp.StatusCode,
			"caller", k.Name, "reason", call.Err)
		return
	}

	if req.Stream {
		call.Completion, call.PromptTok, call.OutputTok, call.DecodeTPS = s.pipeStream(w, resp.Body)
		return
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxRequestBytes))
	if err != nil {
		call.Err = err.Error()
		return
	}
	_, _ = w.Write(out)
	call.Completion, call.PromptTok, call.OutputTok, call.DecodeTPS = summarise(out)
}

// pipeStream copies the event stream straight through while reading the deltas
// out of it. The bytes the caller receives are the bytes upstream sent, since
// re-serialising them would put this service in the position of having to keep
// up with a format it only wants to record.
func (s *site) pipeStream(w http.ResponseWriter, body io.Reader) (string, int, int, float64) {
	rc := http.NewResponseController(w)
	var text strings.Builder
	var promptTok, outTok int
	var tps float64

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		fmt.Fprintln(w, line)
		// A blank line ends an event, and flushing there rather than per line
		// keeps a half written event from reaching the caller.
		if line == "" {
			_ = rc.Flush()
			continue
		}
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
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
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		for _, c := range chunk.Choices {
			text.WriteString(c.Delta.Content)
		}
		if chunk.Usage.PromptTokens > 0 {
			promptTok = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			outTok = chunk.Usage.CompletionTokens
		}
		if chunk.Timings.PredictedPerSec > 0 {
			tps = chunk.Timings.PredictedPerSec
		}
	}
	_ = rc.Flush()
	return text.String(), promptTok, outTok, tps
}

func summarise(out []byte) (string, int, int, float64) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content   string            `json:"content"`
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Timings struct {
			PredictedPerSec float64 `json:"predicted_per_second"`
		} `json:"timings"`
	}
	if json.Unmarshal(out, &resp) != nil || len(resp.Choices) == 0 {
		return "", 0, 0, 0
	}
	text := resp.Choices[0].Message.Content
	// A turn that only called tools has no content, and logging it as empty
	// loses the thing that actually happened.
	if text == "" && len(resp.Choices[0].Message.ToolCalls) > 0 {
		text = "[tool calls: " + string(mustJSON(resp.Choices[0].Message.ToolCalls)) + "]"
	}
	return text, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Timings.PredictedPerSec
}

// passthrough carries the endpoints that are not a completion, like the model
// list. They are not logged, because there is no prompt in them and a health
// check every thirty seconds would bury the log that matters.
func (s *site) passthrough(w http.ResponseWriter, r *http.Request, _ Key) {
	up, err := http.NewRequestWithContext(r.Context(), r.Method, s.upstream+r.URL.Path, r.Body)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	up.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	resp, err := s.client.Do(up)
	if err != nil {
		http.Error(w, "the model server is not answering", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for h, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, maxRequestBytes))
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("[]")
	}
	return b
}

// upstreamReason pulls the message out of llama.cpp's error shape and falls
// back to the raw body, since a body that does not parse is still the evidence.
func upstreamReason(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		if e.Error.Type != "" {
			return e.Error.Type + ": " + e.Error.Message
		}
		return e.Error.Message
	}
	return strings.TrimSpace(string(body))
}
