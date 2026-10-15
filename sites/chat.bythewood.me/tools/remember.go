package tools

import (
	"context"
	"fmt"
	"strings"
)

// The tool for when Isaac says to remember something outright, since the pass
// after a turn decides for itself what to keep and "remember that I want to
// watch this" could otherwise be agreed with and never acted on.

// MemoryFact is one thing known about Isaac, thinned down to what a tool needs.
type MemoryFact struct {
	ID   int64  `json:"id"`
	Text string `json:"fact"`
}

// Memory is chat's own fact store. It's an interface because this package can't
// see the database the site owns, and it's nil in a test that doesn't care, which
// the tool has to survive instead of panicking.
type Memory interface {
	Facts() ([]MemoryFact, error)
	Add(text string) (int64, error)
	Replace(id int64, text string) error
	Delete(id int64) error
}

type rememberArgs struct {
	Action string
	Fact   string
	ID     int64
	Items  string
}

func (a *rememberArgs) params() []param {
	return []param{
		strArg(&a.Action, "action", "what to do with memory").oneOf("add", "append", "replace", "forget", "list").required(),
		strArg(&a.Fact, "fact", "for add and replace, the thing to keep, written as one plain sentence about Isaac"),
		idArg(&a.ID, "id", "for append, replace and forget, which remembered fact, from a list call or the id beside a recalled fact"),
		strArg(&a.Items, "items", "for append, the new items only, like \"The Long Walk\" or \"Cold Storage, Clayface\""),
	}
}

var Remember = Define(Tool{
	Name: "remember",
	Description: "Write to or read what is remembered about Isaac between conversations. " +
		"Call it whenever he says to remember, note, forget or correct something, " +
		"and whenever he states a preference, a plan or a fact about himself that should outlast this chat. " +
		"Saying you will remember without calling this does not remember anything. " +
		"When he adds to a list already remembered, like another film for a watch list, use append with that " +
		"fact's id and only the new items, rather than adding a second fact that holds half the list. " +
		"One call is the whole job.",
}, func(ctx context.Context, d *Deps, a rememberArgs) (any, error) {
	if d.Memory == nil {
		return nil, fmt.Errorf("memory is not available in this turn")
	}
	// A mode that writes nothing down cannot be the one that teaches it
	// something to write down later, which is the same rule the automatic
	// pass follows. Reading is still fine, since that reveals nothing new.
	action := strings.ToLower(a.Action)
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
		text := a.Fact
		if text == "" {
			return nil, fmt.Errorf("nothing to remember, pass the fact to keep")
		}
		before, _ := d.Memory.Facts()
		id, err := d.Memory.Add(text)
		if err != nil {
			return nil, err
		}
		// A fact that says nothing new is merged into the one already held,
		// and a reply saying "the fix is in" over that is a false claim.
		after, _ := d.Memory.Facts()
		for _, f := range before {
			if f.ID == id && factText(after, id) == f.Text {
				return map[string]any{"already_known": f.Text, "id": id,
					"note": "Nothing new was written, since this was already remembered. If he asked you to " +
						"remember it, say it was already known. Never say you changed or fixed something here."}, nil
			}
		}
		return map[string]any{"remembered": text, "id": id,
			"note": "Say plainly that you have remembered it, in a line, and do not " +
				"repeat the answer you gave before."}, nil

	case "replace":
		id := a.ID
		text := a.Fact
		if id == 0 || text == "" {
			return nil, fmt.Errorf("replace needs both the id from a list call and the new wording")
		}
		if err := d.Memory.Replace(id, text); err != nil {
			return nil, err
		}
		return map[string]any{"replaced": id, "now": text,
			"note": "Say plainly that you have updated it, in a line."}, nil

	case "append":
		id := a.ID
		items := strings.Trim(a.Items, ".,")
		if id == 0 || items == "" {
			return nil, fmt.Errorf("append needs the id of the list and the new items")
		}
		facts, err := d.Memory.Facts()
		if err != nil {
			return nil, err
		}
		held := factText(facts, id)
		if held == "" {
			return nil, fmt.Errorf("there is no remembered fact %d, call list to find the right one", id)
		}
		now := appendItems(held, items)
		if err := d.Memory.Replace(id, now); err != nil {
			return nil, err
		}
		return map[string]any{"appended": items, "now": now,
			"note": "Say plainly that it is on the list now, in a line."}, nil

	case "forget":
		id := a.ID
		if id == 0 {
			return nil, fmt.Errorf("forget needs the id from a list call")
		}
		if err := d.Memory.Delete(id); err != nil {
			return nil, err
		}
		return map[string]any{"forgot": id}, nil
	}
	return nil, fmt.Errorf("no action called %q, use add, append, replace, forget or list", action)
})

func factText(facts []MemoryFact, id int64) string {
	for _, f := range facts {
		if f.ID == id {
			return f.Text
		}
	}
	return ""
}

// appendItems extends a list written as a sentence in Go, since a model writing
// the whole list out again dropped a word from a title. The Oxford comma keeps
// the last title whole, or "Cold Storage and Clayface" reads back as one film.
func appendItems(fact, items string) string {
	base := strings.TrimRight(strings.TrimSpace(fact), ". ")
	if i := strings.LastIndex(base, ", and "); i >= 0 {
		base = base[:i] + ", " + base[i+6:]
	} else if i := strings.LastIndex(base, " and "); i >= 0 && !strings.Contains(base[i+5:], ",") {
		base = base[:i] + ", " + base[i+5:]
	}
	var list []string
	for _, part := range strings.Split(strings.ReplaceAll(items, " and ", ", "), ",") {
		if p := strings.TrimSpace(part); p != "" {
			list = append(list, p)
		}
	}
	if len(list) == 0 {
		return base + "."
	}
	if len(list) > 1 {
		base += ", " + strings.Join(list[:len(list)-1], ", ")
	}
	return base + ", and " + list[len(list)-1] + "."
}
