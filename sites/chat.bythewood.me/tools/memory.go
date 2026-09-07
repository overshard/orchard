package tools

import (
	"context"
	"fmt"
	"strings"
)

// Asked to remember something, the model had nothing to call. Memory was
// written only by the pass that runs after a turn, which reads the exchange and
// decides for itself what was worth keeping, so "remember that I want to watch
// this" was a sentence it could agree with and not act on.
//
// This is the deliberate half. The pass still runs and still catches what Isaac
// never thought to state, and this is for when he says it outright.

// MemoryFact is one thing known about Isaac, thinned down to what a tool needs.
type MemoryFact struct {
	ID   int64  `json:"id"`
	Text string `json:"fact"`
}

// Memory is chat's own fact store. It is an interface because this package
// cannot see the database the site owns, and it is nil in a test that does not
// care, which the tool has to survive rather than panic on.
type Memory interface {
	Facts() ([]MemoryFact, error)
	Add(text string) (int64, error)
	Replace(id int64, text string) error
	Delete(id int64) error
}

var Remember = Tool{
	Name: "remember",
	Description: "Write to or read what is remembered about Isaac between conversations. " +
		"Call it whenever he says to remember, note, forget or correct something, " +
		"and whenever he states a preference, a plan or a fact about himself that should outlast this chat. " +
		"Saying you will remember without calling this does not remember anything.",
	Schema: obj(map[string]any{
		"action": map[string]any{"type": "string",
			"description": "what to do with memory",
			"enum":        []string{"add", "replace", "forget", "list"}},
		"fact": map[string]any{"type": "string",
			"description": "for add and replace, the thing to keep, written as one plain sentence about Isaac"},
		"id": map[string]any{"type": "integer",
			"description": "for replace and forget, which remembered fact, from a list call"},
	}, "action"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		if d.Memory == nil {
			return nil, fmt.Errorf("memory is not available in this turn")
		}
		// A mode that writes nothing down cannot be the one that teaches it
		// something to write down later, which is the same rule the automatic
		// pass follows. Reading is still fine, since that reveals nothing new.
		action := strings.ToLower(argStr(a, "action"))
		if d.Incognito && action != "list" {
			return nil, fmt.Errorf("this is an incognito turn, so nothing is written down, " +
				"including memory, and Isaac has to ask again outside incognito")
		}

		switch action {
		case "list":
			facts, err := d.Memory.Facts()
			if err != nil {
				return nil, err
			}
			return map[string]any{"facts": facts, "count": len(facts)}, nil

		case "add":
			text := argStr(a, "fact")
			if text == "" {
				return nil, fmt.Errorf("nothing to remember, pass the fact to keep")
			}
			id, err := d.Memory.Add(text)
			if err != nil {
				return nil, err
			}
			return map[string]any{"remembered": text, "id": id,
				"note": "Say plainly that you have remembered it, in a line, and do not " +
					"repeat the answer you gave before."}, nil

		case "replace":
			id := argInt64(a, "id")
			text := argStr(a, "fact")
			if id == 0 || text == "" {
				return nil, fmt.Errorf("replace needs both the id from a list call and the new wording")
			}
			if err := d.Memory.Replace(id, text); err != nil {
				return nil, err
			}
			return map[string]any{"replaced": id, "now": text}, nil

		case "forget":
			id := argInt64(a, "id")
			if id == 0 {
				return nil, fmt.Errorf("forget needs the id from a list call")
			}
			if err := d.Memory.Delete(id); err != nil {
				return nil, err
			}
			return map[string]any{"forgot": id}, nil
		}
		return nil, fmt.Errorf("no action called %q, use add, replace, forget or list", action)
	},
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
