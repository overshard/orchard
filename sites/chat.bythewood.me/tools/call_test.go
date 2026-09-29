package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Run on no arguments, remember answers a cut off call with a complaint about
// the action, which sends the model after the wrong fix.
func TestCallRefusesArgumentsThatDoNotParse(t *testing.T) {
	ran := 0
	var r Registry
	r.Add(Tool{Name: "probe", Run: func(context.Context, *Deps, map[string]any) (any, error) {
		ran++
		return "ok", nil
	}})

	res := r.Call(context.Background(), NewDeps(), "probe",
		json.RawMessage(`{"action":"add","fact":"directed by Feige, directed by`))
	if ran != 0 {
		t.Error("the tool ran on arguments that do not parse")
	}
	if !strings.Contains(res.Err, "cut off") {
		t.Errorf("err = %q, want it to say the call was cut off", res.Err)
	}
	if m, ok := res.Content.(map[string]any); !ok || m["error"] != res.Err {
		t.Errorf("content = %#v, want the error for the model", res.Content)
	}

	// No arguments at all is a call that takes none, not a broken one.
	if res := r.Call(context.Background(), NewDeps(), "probe", nil); res.Err != "" || ran != 1 {
		t.Errorf("an empty call gave %q and ran %d times", res.Err, ran)
	}
}

func TestOnlyKeepsEveryNamedTool(t *testing.T) {
	all := Default().Schemas()
	var names []string
	for _, s := range Only(all, Remember.Name, WebSearch.Name, Wikipedia.Name) {
		names = append(names, s["function"].(map[string]any)["name"].(string))
	}
	if got := strings.Join(names, " "); got != "remember web_search wikipedia" {
		t.Errorf("got %q", got)
	}
	if got := Only(all, "no_such_tool"); len(got) != 0 {
		t.Errorf("an unknown name offered %d tools", len(got))
	}
}
