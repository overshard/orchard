package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

type probeArgs struct {
	Query string
	Count int
	Scale float64
	ID    int64
	Loud  bool
	Mode  string
}

func (a *probeArgs) params() []param {
	return []param{
		strArg(&a.Query, "query", "what to look for").required(),
		intArg(&a.Count, "count", "how many").otherwise(6),
		numArg(&a.Scale, "scale", "how much"),
		idArg(&a.ID, "id", "which one"),
		boolArg(&a.Loud, "loud", "say it loudly"),
		strArg(&a.Mode, "mode", "how").oneOf("fast", "slow").required(),
	}
}

func TestDefineWritesTheSchemaFromTheStruct(t *testing.T) {
	tool := Define(Tool{Name: "probe"}, func(context.Context, *Deps, probeArgs) (any, error) { return nil, nil })
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "what to look for"},
			"count": map[string]any{"type": "integer", "description": "how many"},
			"scale": map[string]any{"type": "number", "description": "how much"},
			"id":    map[string]any{"type": "integer", "description": "which one"},
			"loud":  map[string]any{"type": "boolean", "description": "say it loudly"},
			"mode":  map[string]any{"type": "string", "description": "how", "enum": []string{"fast", "slow"}},
		},
		"required": []string{"query", "mode"},
	}
	if !reflect.DeepEqual(tool.Schema, want) {
		t.Errorf("schema = %#v", tool.Schema)
	}

	none := Define(Tool{Name: "none"}, func(context.Context, *Deps, noArgs) (any, error) { return nil, nil })
	if b, _ := json.Marshal(none.Schema); string(b) != `{"properties":{},"type":"object"}` {
		t.Errorf("a tool with no arguments has schema %s", b)
	}
}

// A small model sends numbers as strings, booleans as words and leaves out what
// it has no use for, and none of that should reach a tool as a different value.
func TestDefineReadsSloppyArgumentsLeniently(t *testing.T) {
	var got probeArgs
	tool := Define(Tool{Name: "probe"}, func(_ context.Context, _ *Deps, a probeArgs) (any, error) {
		got = a
		return nil, nil
	})
	cases := []struct {
		raw  map[string]any
		want probeArgs
	}{
		{map[string]any{"query": "  tea  "}, probeArgs{Query: "tea", Count: 6}},
		{map[string]any{"query": 42, "count": "3", "scale": "2.5", "id": "7", "loud": "TRUE"},
			probeArgs{Query: "42", Count: 3, Scale: 2.5, ID: 7, Loud: true}},
		{map[string]any{"count": 2.9, "scale": float64(4), "id": float64(9), "loud": true},
			probeArgs{Count: 2, Scale: 4, ID: 9, Loud: true}},
		{map[string]any{"count": "lots", "id": "x", "loud": "yes", "scale": true},
			probeArgs{Count: 6}},
		{nil, probeArgs{Count: 6}},
	}
	for _, c := range cases {
		got = probeArgs{}
		if _, err := tool.Run(context.Background(), NewDeps(), c.raw); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%v read as %+v, want %+v", c.raw, got, c.want)
		}
	}
}
