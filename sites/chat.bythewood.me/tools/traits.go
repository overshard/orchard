package tools

import "slices"

// Trait is something about a tool that the turn acts on, so the turn asks the
// tool instead of knowing it by name.
type Trait uint

const (
	// Untrusted reads text a stranger wrote, which can tell the model to leak
	// whatever it reads next, so nothing Private is offered after one answers.
	Untrusted Trait = 1 << iota
	// Private reads Isaac's own data, and strangers write some of it through
	// request paths and /collect, so nothing Outbound is offered after one answers.
	Private
	// Estate reads one of his own sites, which are the record of themselves,
	// so the gate never sends its answer off to be checked on the web.
	Estate
	// OpenWeb searches or fetches whatever the web turns up.
	OpenWeb
	// Settles answers in full out of the public record, so the open web comes
	// off after it and the gate takes its answer as it stands.
	Settles
	// Once is called one time a turn, since a second call repeats the first.
	Once
	// Rundown is a finished list of many pages, each one worth a link, and the
	// gate never sends it back for more.
	Rundown
	// Notes writes down what he asked to be kept.
	Notes
	// Lookup is a first place to look something up, offered beside Notes when
	// a note asks for one.
	Lookup
	// Draws makes a picture, and a picture ends the turn.
	Draws
	// Tickers takes ticker symbols, so the ones he typed go into the call.
	Tickers
	// Sums does arithmetic, and a total worked out in prose is sent back to it.
	Sums
	// Clock reads the time, and a countdown in a draft is checked against it.
	Clock
	// Outbound puts what the model wrote into a request to somebody else's server.
	Outbound
)

// Cited is one page behind a result, for the numbered sources an answer links.
// Fetched is a page read whole, which goes ahead of a search hit.
type Cited struct {
	URL     string
	Title   string
	Text    string
	Fetched bool
}

// Is reports whether the tool has any of the traits.
func (t Tool) Is(tr Trait) bool { return t.Traits&tr != 0 }

var every = Default()

// Of is the tool registered under a name, and one with no traits for a name
// that isn't registered.
func Of(name string) Tool {
	t, _ := every.Get(name)
	return t
}

// WithoutAny drops every tool with any of the traits from an offer.
func WithoutAny(schemas []map[string]any, tr Trait) []map[string]any {
	return slices.DeleteFunc(slices.Clone(schemas), func(s map[string]any) bool {
		return Of(schemaName(s)).Is(tr)
	})
}

// OnlyAny narrows an offer to the tools with any of the traits.
func OnlyAny(schemas []map[string]any, tr Trait) []map[string]any {
	var out []map[string]any
	for _, s := range schemas {
		if Of(schemaName(s)).Is(tr) {
			out = append(out, s)
		}
	}
	return out
}

func schemaName(s map[string]any) string {
	fn, _ := s["function"].(map[string]any)
	n, _ := fn["name"].(string)
	return n
}
