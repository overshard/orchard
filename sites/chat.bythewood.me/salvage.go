// Recovering a tool call the model wrote as prose.
//
// llama.cpp parses the tool call syntax its template defines and hands back a
// structured tool_calls array. When a model emits a malformed version of that
// syntax, the parser does not match and the whole thing arrives as ordinary
// content, which then renders to the user as markup. Ornith does this
// occasionally, in this shape and with no closing tags:
//
//	<tool_call> <function=web_fetch> <parameter=url> https://example.com </tool_call>
//
// Two things have to happen. The markup must never reach the page, and the
// call it describes should still run, because the turn is otherwise wasted.
package main

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	// The block a leaked call sits in. The closing tag is optional because the
	// model often drops it, in which case everything to the end is the call.
	callBlock = regexp.MustCompile(`(?is)<tool_call>(.*?)(?:</tool_call>|$)`)
	// The two shapes seen in the wild: an attribute style name, and a JSON
	// object with a name field.
	fnName    = regexp.MustCompile(`(?is)<function\s*=\s*([a-z_][a-z0-9_]*)`)
	fnParam   = regexp.MustCompile(`(?is)<parameter\s*=\s*([a-z_][a-z0-9_]*)\s*>([^<]*)`)
	jsonName  = regexp.MustCompile(`(?is)"name"\s*:\s*"([a-z_][a-z0-9_]*)"`)
	strayOpen = regexp.MustCompile(`(?is)</?(tool_call|function|parameter)[^>]*>`)
)

// salvageCalls pulls any tool calls out of prose and returns them alongside the
// content with the markup removed. Both halves matter: an unparsed call is a
// wasted turn, and leaving the markup in is markup on the page.
func salvageCalls(content string, known func(string) bool) (string, []ToolCall) {
	if !strings.Contains(content, "<tool_call") && !strings.Contains(content, "<function=") {
		return content, nil
	}
	var calls []ToolCall
	blocks := callBlock.FindAllStringSubmatchIndex(content, -1)
	if blocks == nil {
		// A bare <function=...> with no wrapper around it.
		if tc, ok := parseCall(content, known); ok {
			calls = append(calls, tc)
		}
		return cleanup(content), calls
	}
	var kept strings.Builder
	last := 0
	for _, m := range blocks {
		kept.WriteString(content[last:m[0]])
		last = m[1]
		if tc, ok := parseCall(content[m[2]:m[3]], known); ok {
			calls = append(calls, tc)
		}
	}
	kept.WriteString(content[last:])
	return cleanup(kept.String()), calls
}

func parseCall(s string, known func(string) bool) (ToolCall, bool) {
	name := ""
	if m := fnName.FindStringSubmatch(s); m != nil {
		name = m[1]
	} else if m := jsonName.FindStringSubmatch(s); m != nil {
		name = m[1]
	}
	if name == "" || !known(name) {
		return ToolCall{}, false
	}
	args := map[string]any{}
	for _, m := range fnParam.FindAllStringSubmatch(s, -1) {
		args[m[1]] = strings.TrimSpace(m[2])
	}
	if len(args) == 0 {
		// The JSON shape keeps its arguments in an object rather than in tags.
		if i := strings.Index(s, "{"); i >= 0 {
			var probe struct {
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal([]byte(s[i:]), &probe) == nil && len(probe.Arguments) > 0 {
				_ = json.Unmarshal(probe.Arguments, &args)
			}
		}
	}
	if len(args) == 0 {
		return ToolCall{}, false
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return ToolCall{}, false
	}
	var tc ToolCall
	tc.Type = "function"
	tc.ID = "salvaged_" + name
	tc.Function.Name = name
	tc.Function.Arguments = string(raw)
	return tc, true
}

// cleanup takes out any tag fragments left behind and tidies the whitespace,
// so a partially leaked call does not show as stray angle brackets.
func cleanup(s string) string {
	s = strayOpen.ReplaceAllString(s, "")
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
