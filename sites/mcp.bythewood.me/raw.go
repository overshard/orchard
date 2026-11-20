package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"regexp"
	"slices"
	"strings"
)

// argPairs splits a call's stored arguments into key and value, in key order.
// Arguments cut short at the storage cap don't parse and give nothing.
func argPairs(args string) [][2]string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		v, ok := m[k].(string)
		if !ok {
			b, _ := json.Marshal(m[k])
			v = string(b)
		}
		out = append(out, [2]string{k, v})
	}
	return out
}

// rpcMessage keeps a JSON-RPC message's fields in the order they go over the wire.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   any             `json:"error,omitempty"`
}

// exchange is a call's request and reply rebuilt from what's kept: the id,
// the method, the arguments and the start of the answer.
type exchange struct {
	Request, Response string
	// Note says what the rebuild can't show, like an answer cut short.
	Note string
}

func (c Call) Exchange() exchange {
	id := json.RawMessage("null")
	if c.RPCID != "" && json.Valid([]byte(c.RPCID)) {
		id = json.RawMessage(c.RPCID)
	}
	req := rpcMessage{JSONRPC: "2.0", ID: id, Method: c.Method}
	var notes []string
	if c.Method == "tools/call" {
		args := any(map[string]any{})
		if c.Args != "" {
			if json.Valid([]byte(c.Args)) {
				args = json.RawMessage(c.Args)
			} else {
				args = c.Args
				notes = append(notes, "the arguments were longer than the 2,000 characters kept")
			}
		}
		req.Params = map[string]any{"name": c.Tool, "arguments": args}
	}
	ex := exchange{Request: indent(req)}
	if c.RPCID == "" {
		notes = append(notes, "this call's id wasn't kept")
	}

	res := rpcMessage{JSONRPC: "2.0", ID: id}
	switch {
	case c.Code > 0:
		ex.Response = fmt.Sprintf("HTTP %d with a WWW-Authenticate challenge to sign in again, and no JSON-RPC body", c.Code)
		ex.Note = strings.Join(notes, ", ")
		return ex
	case c.Code == -32602:
		res.Error = map[string]any{"code": c.Code, "message": "no tool named " + c.Tool}
	case c.Code == -32601:
		res.Error = map[string]any{"code": c.Code, "message": "no method " + c.Method}
	case c.Method == "tools/call":
		text := c.Err
		if c.OK {
			text = c.Preview
		}
		res.Result = map[string]any{"resultType": "complete", "content": []map[string]string{{"type": "text", "text": text}}, "isError": !c.OK}
		if c.OK && c.Cut() {
			notes = append(notes, fmt.Sprintf("the text is the first 600 of %s characters", comma(c.Chars)))
		}
	case c.Method == "ping":
		res.Result = map[string]any{}
	default:
		res.Result = "not kept, every " + c.Method + " gets the same answer"
	}
	ex.Response = indent(res)
	ex.Note = strings.Join(notes, ", ")
	return ex
}

func indent(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

var jsonToken = regexp.MustCompile(`"(?:[^"\\]|\\.)*"(\s*:)?|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null`)

// jsonHTML colours keys, strings and literals in indented JSON.
func jsonHTML(s string) template.HTML {
	var b strings.Builder
	last := 0
	for _, m := range jsonToken.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(template.HTMLEscapeString(s[last:m[0]]))
		tok := s[m[0]:m[1]]
		switch {
		case m[2] >= 0:
			key := s[m[0]:m[2]]
			b.WriteString(`<span class="k">` + template.HTMLEscapeString(key) + `</span>` + template.HTMLEscapeString(s[m[2]:m[3]]))
		case tok[0] == '"':
			b.WriteString(`<span class="s">` + template.HTMLEscapeString(tok) + `</span>`)
		default:
			b.WriteString(`<span class="n">` + tok + `</span>`)
		}
		last = m[1]
	}
	b.WriteString(template.HTMLEscapeString(s[last:]))
	return template.HTML(b.String())
}

// Cut is whether the kept preview is shorter than what was sent.
func (c Call) Cut() bool { return strings.HasSuffix(c.Preview, "…") }
