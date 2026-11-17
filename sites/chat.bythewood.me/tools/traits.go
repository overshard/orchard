package tools

import "slices"

// Trait is something about a tool that the turn acts on, so the turn asks the
// tool instead of knowing it by name.
type Trait uint

const (
	// Untrusted reads text a stranger wrote, which can tell the model to leak
	// whatever it reads next, so nothing Private is offered after one answers
	// and the turn is not mined for memories.
	Untrusted Trait = 1 << iota
	// Private reads Isaac's own data, and strangers write some of it through
	// request paths and /collect, so nothing Outbound is offered after one answers.
	Private
	// Estate reads one of his own sites, so strangers' text out of one of them
	// still leaves the others open.
	Estate
	// OpenWeb searches or fetches whatever the web turns up.
	OpenWeb
	// Settles answers in full out of the public record, so the open web comes
	// off after it.
	Settles
	// Once is called one time a turn, since a second call repeats the first.
	Once
	// Rundown is a finished list of many pages, each one worth a link.
	Rundown
	// Tickers takes ticker symbols, so the ones he typed go into the call.
	Tickers
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
	return WithoutFunc(schemas, func(t Tool) bool { return t.Is(tr) })
}

// WithoutFunc drops every tool drop is true for from an offer.
func WithoutFunc(schemas []map[string]any, drop func(Tool) bool) []map[string]any {
	return slices.DeleteFunc(slices.Clone(schemas), func(s map[string]any) bool {
		return drop(Of(schemaName(s)))
	})
}

func schemaName(s map[string]any) string {
	fn, _ := s["function"].(map[string]any)
	n, _ := fn["name"].(string)
	return n
}
