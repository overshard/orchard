package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// fakeClaude answers every Messages call with the next of replies and keeps
// each request body so a test can read what the gateway sent.
func fakeClaude(t *testing.T, contentType string, replies ...string) (*site, string, *[]map[string]any) {
	t.Helper()
	var sent []map[string]any
	n := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		sent = append(sent, m)
		w.Header().Set("Content-Type", contentType)
		if n >= len(replies) {
			t.Errorf("an unexpected call %d to the api", n+1)
			w.WriteHeader(500)
			return
		}
		if strings.HasPrefix(replies[n], "status:") {
			w.WriteHeader(400)
			replies[n] = strings.TrimPrefix(replies[n], "status:")
		}
		fmt.Fprint(w, replies[n])
		n++
	}))
	t.Cleanup(up.Close)
	store := testStore(t)
	secret, _, _ := store.NewKey("chat")
	s := &site{store: store, claude: newClaude("test", defaultClaudeModel,
		option.WithBaseURL(up.URL), option.WithMaxRetries(0))}
	return s, secret, &sent
}

func call(t *testing.T, s *site, secret, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	s.requireKey(s.completions)(rec, req)
	return rec
}

const usage = `"usage":{"input_tokens":10,"cache_read_input_tokens":90,"cache_creation_input_tokens":0,"output_tokens":5}`

func TestAToolRoundIsTranslatedBothWays(t *testing.T) {
	s, secret, sent := fakeClaude(t, "application/json", `{"id":"msg_1","type":"message","role":"assistant",
		"model":"claude-haiku-5-5","stop_reason":"tool_use","content":[
		{"type":"text","text":"checking"},
		{"type":"tool_use","id":"toolu_2","name":"weather","input":{"city":"Paris"}}],`+usage+`}`)

	rec := call(t, s, secret, `{"model":"local","temperature":0.7,"top_k":20,"max_tokens":300,
		"chat_template_kwargs":{"enable_thinking":false},"tool_choice":"required",
		"messages":[
		{"role":"system","content":"be brief"},
		{"role":"user","content":"weather?"},
		{"role":"assistant","content":"","tool_calls":[{"id":"call.1","type":"function","function":{"name":"clock","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call.1","content":"noon"},
		{"role":"user","content":"and paris?"}],
		"tools":[{"type":"function","function":{"name":"weather","description":"the weather",
		"parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	got := (*sent)[0]
	for _, k := range []string{"temperature", "top_k", "top_p", "chat_template_kwargs"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s was sent on", k)
		}
	}
	if got["max_tokens"].(float64) != 300 || got["model"] != defaultClaudeModel {
		t.Errorf("max_tokens %v model %v", got["max_tokens"], got["model"])
	}
	if th := got["thinking"].(map[string]any); th["type"] != "disabled" {
		t.Errorf("thinking = %v", th)
	}
	if tc := got["tool_choice"].(map[string]any); tc["type"] != "any" {
		t.Errorf("tool_choice = %v", tc)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("sent %d turns, want user, assistant, user: %v", len(msgs), msgs)
	}
	last := msgs[2].(map[string]any)["content"].([]any)
	if r := last[0].(map[string]any); r["type"] != "tool_result" || r["tool_use_id"] != "call_1" {
		t.Errorf("the tool result did not lead the next turn under the mapped id: %v", r)
	}
	if tools := got["tools"].([]any); len(tools) != 2 || tools[1].(map[string]any)["type"] != "web_search_20250305" {
		t.Errorf("tools = %v", tools)
	}

	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string       `json:"content"`
				ToolCalls []oaToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	c := out.Choices[0]
	if c.FinishReason != "tool_calls" || c.Message.Content != "checking" || len(c.Message.ToolCalls) != 1 {
		t.Fatalf("answer = %+v", c)
	}
	if tc := c.Message.ToolCalls[0]; tc.Function.Name != "weather" || tc.Function.Arguments != `{"city":"Paris"}` {
		t.Errorf("tool call = %+v", tc)
	}
	if out.Usage.PromptTokens != 100 {
		t.Errorf("prompt tokens = %d, want the cached ones counted too", out.Usage.PromptTokens)
	}
	calls, _ := s.store.Calls("chat", 5)
	if len(calls) != 1 || calls[0].Model != defaultClaudeModel || calls[0].PromptTok != 100 {
		t.Errorf("logged %+v", calls)
	}
}

func TestASearchThatPausesIsSentBackAndOnlyTheAnswerKept(t *testing.T) {
	s, secret, sent := fakeClaude(t, "application/json",
		`{"id":"m1","type":"message","role":"assistant","model":"x","stop_reason":"pause_turn","content":[
		{"type":"text","text":"Let me look."},
		{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"q"}}],`+usage+`}`,
		`{"id":"m2","type":"message","role":"assistant","model":"x","stop_reason":"end_turn","content":[
		{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[]},
		{"type":"text","text":"It is sunny."}],`+usage+`}`)

	rec := call(t, s, secret, `{"messages":[{"role":"user","content":"weather?"}],
		"tools":[{"type":"function","function":{"name":"clock","parameters":{"type":"object","properties":{}}}}]}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(*sent) != 2 {
		t.Fatalf("made %d calls, want the pause sent back once", len(*sent))
	}
	if !strings.Contains(rec.Body.String(), `"content":"It is sunny."`) {
		t.Errorf("answer = %s", rec.Body)
	}
}

func TestACallerWithItsOwnSearchKeepsIt(t *testing.T) {
	s, secret, sent := fakeClaude(t, "application/json", `{"id":"m","type":"message","role":"assistant",
		"model":"x","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],`+usage+`}`)
	rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}],
		"tools":[{"type":"function","function":{"name":"web_search","parameters":{"type":"object","properties":{}}}}]}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if tools := (*sent)[0]["tools"].([]any); len(tools) != 1 {
		t.Errorf("tools = %v", tools)
	}
}

func TestAStructuredCallGetsASchemaTheApiTakes(t *testing.T) {
	s, secret, sent := fakeClaude(t, "application/json", `{"id":"m","type":"message","role":"assistant",
		"model":"x","stop_reason":"end_turn","content":[{"type":"text","text":"{\"lines\":[\"a\"]}"}],`+usage+`}`)
	rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}],"response_format":{"type":"json_schema",
		"json_schema":{"name":"lines","strict":true,"schema":{"type":"object","properties":{"lines":{"type":"array",
		"items":{"type":"string","maxLength":40},"minItems":5,"maxItems":5}},"required":["lines"]}}}}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	schema := (*sent)[0]["output_config"].(map[string]any)["format"].(map[string]any)["schema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Errorf("the object was left open: %v", schema)
	}
	lines := schema["properties"].(map[string]any)["lines"].(map[string]any)
	if _, ok := lines["maxItems"]; ok || lines["minItems"].(float64) != 1 {
		t.Errorf("array constraints = %v", lines)
	}
	if _, ok := lines["items"].(map[string]any)["maxLength"]; ok {
		t.Errorf("maxLength went through")
	}
	if _, ok := (*sent)[0]["tools"]; ok {
		t.Error("web search was offered to a call with no tools")
	}
}

func TestARefusalIsAnError(t *testing.T) {
	s, secret, _ := fakeClaude(t, "application/json", `{"id":"m","type":"message","role":"assistant","model":"x",
		"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"},"content":[],`+usage+`}`)
	rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "cyber") {
		t.Errorf("status = %d: %s", rec.Code, rec.Body)
	}
}

func TestAnApiErrorKeepsItsStatusAndMessage(t *testing.T) {
	s, secret, _ := fakeClaude(t, "application/json",
		`status:{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long"}}`)
	rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "prompt is too long") {
		t.Errorf("status = %d: %s", rec.Code, rec.Body)
	}
	calls, _ := s.store.Calls("chat", 5)
	if len(calls) != 1 || calls[0].Err != "prompt is too long" {
		t.Errorf("logged %+v", calls)
	}
}

func TestAStreamComesBackInTheOpenAIShape(t *testing.T) {
	ev := func(name, data string) string { return "event: " + name + "\ndata: " + data + "\n\n" }
	stream := ev("message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"x","content":[],"usage":{"input_tokens":12,"output_tokens":1}}}`) +
		ev("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`) +
		ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`) +
		ev("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`) +
		ev("message_stop", `{"type":"message_stop"}`)
	s, secret, _ := fakeClaude(t, "text/event-stream", stream)

	rec := call(t, s, secret, `{"stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{`"content":"Hel"`, `"content":"lo"`, `"finish_reason":"stop"`,
		`"prompt_tokens":12`, `"completion_tokens":4`, "data: [DONE]"} {
		if !strings.Contains(body, want) {
			t.Errorf("stream is missing %s:\n%s", want, body)
		}
	}
	calls, _ := s.store.Calls("chat", 5)
	if len(calls) != 1 || calls[0].Completion != "Hello" {
		t.Errorf("logged %+v", calls)
	}
}

func TestTheCardEndpointsAnswerWithoutACard(t *testing.T) {
	s, secret, _ := fakeClaude(t, "application/json")
	for _, c := range []struct{ method, path, want string }{
		{"GET", "/v1/gpu", `"busy":false`},
		{"GET", "/v1/running", `"loaded":false`},
		{"POST", "/v1/unload", `"loaded":false`},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		h := map[string]func(http.ResponseWriter, *http.Request, Key){"/v1/gpu": s.gpu, "/v1/running": s.running, "/v1/unload": s.unload}[c.path]
		s.requireKey(h)(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s = %d %s", c.path, rec.Code, rec.Body)
		}
	}
}

func TestASearchHandsBackHitsWithTheQuotedPassages(t *testing.T) {
	s, secret, sent := fakeClaude(t, "application/json", `{"id":"m","type":"message","role":"assistant","model":"x",
		"stop_reason":"end_turn","content":[
		{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"q"}},
		{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[
			{"type":"web_search_result","url":"https://a.example","title":"A","encrypted_content":"x","page_age":"2 days ago"},
			{"type":"web_search_result","url":"https://b.example","title":"B","encrypted_content":"x"}]},
		{"type":"text","text":"A says so.","citations":[{"type":"web_search_result_location",
			"url":"https://a.example","title":"A","cited_text":"the passage","encrypted_index":"e"}]}],`+usage+`}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/search", strings.NewReader(`{"query":"q","domains":["x.com"]}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	s.requireKey(s.search)(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var out struct{ Results []searchHit }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Results) != 2 || out.Results[0].Snippet != "the passage" || out.Results[1].Title != "B" {
		t.Errorf("results = %+v", out.Results)
	}
	tool := (*sent)[0]["tools"].([]any)[0].(map[string]any)
	if d := tool["allowed_domains"].([]any); len(d) != 1 || d[0] != "x.com" {
		t.Errorf("domains = %v", d)
	}
}
