package tools

import (
	"context"
	"fmt"
	"strings"
)

// A tool's arguments are a struct, and its params method ties each field to the
// name and description the model is handed. The schema and the decoding both
// come from that one list, so a field misspelt in a tool fails to compile.
type args[A any] interface {
	*A
	params() []param
}

// Define finishes a tool from its arguments struct, filling in the schema and a
// Run that decodes into the struct before calling run.
func Define[A any, P args[A]](t Tool, run func(ctx context.Context, t *Turn, a A) (any, error)) Tool {
	var zero A
	t.Schema = schemaOf(P(&zero).params())
	t.Run = func(ctx context.Context, turn *Turn, raw map[string]any) (any, error) {
		var a A
		for _, p := range P(&a).params() {
			p.set(raw)
		}
		return run(ctx, turn, a)
	}
	return t
}

// noArgs is for a tool that takes nothing.
type noArgs struct{}

func (*noArgs) params() []param { return nil }

// param is one argument. Exactly one of the pointers is set, and it says both
// the JSON type the model is told and how forgiving the decoding is.
type param struct {
	name string
	desc string
	enum []string
	must bool
	def  float64

	str   *string
	num   *float64
	whole *int
	id    *int64
	flag  *bool
}

func strArg(p *string, name, desc string) param  { return param{name: name, desc: desc, str: p} }
func numArg(p *float64, name, desc string) param { return param{name: name, desc: desc, num: p} }
func intArg(p *int, name, desc string) param     { return param{name: name, desc: desc, whole: p} }
func idArg(p *int64, name, desc string) param    { return param{name: name, desc: desc, id: p} }
func boolArg(p *bool, name, desc string) param   { return param{name: name, desc: desc, flag: p} }
func (p param) required() param                  { p.must = true; return p }
func (p param) oneOf(values ...string) param     { p.enum = values; return p }
func (p param) otherwise(def float64) param      { p.def = def; return p }

func (p param) kind() string {
	switch {
	case p.num != nil:
		return "number"
	case p.whole != nil, p.id != nil:
		return "integer"
	case p.flag != nil:
		return "boolean"
	}
	return "string"
}

// set reads the field the way a small model writes it, so a number can arrive
// as a string and a missing field is its zero or its default.
func (p param) set(a map[string]any) {
	switch {
	case p.str != nil:
		*p.str = argStr(a, p.name)
	case p.num != nil:
		*p.num = argNum(a, p.name, p.def)
	case p.whole != nil:
		*p.whole = int(argNum(a, p.name, p.def))
	case p.id != nil:
		*p.id = argInt64(a, p.name)
	case p.flag != nil:
		*p.flag = argBool(a, p.name)
	}
}

func schemaOf(ps []param) map[string]any {
	props := map[string]any{}
	var required []string
	for _, p := range ps {
		prop := map[string]any{"type": p.kind(), "description": p.desc}
		if p.enum != nil {
			prop["enum"] = p.enum
		}
		props[p.name] = prop
		if p.must {
			required = append(required, p.name)
		}
	}
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func argStr(a map[string]any, k string) string {
	if v, ok := a[k]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

// argBool takes a small model's "true" as readily as its true.
func argBool(a map[string]any, k string) bool {
	switch v := a[k].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return false
}

func argNum(a map[string]any, k string, def float64) float64 {
	v, ok := a[k]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%g", &f); err == nil {
			return f
		}
	}
	return def
}

// The model sends an id as a number, a float or a string depending on how the
// template rendered it, and a wrong type here reads as a missing id.
func argInt64(a map[string]any, k string) int64 {
	v, ok := a[k]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		var out int64
		if _, err := fmt.Sscan(strings.TrimSpace(n), &out); err != nil {
			return 0
		}
		return out
	}
	return 0
}
