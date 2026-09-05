// Package skills holds the handlers that answer a question from a live source
// instead of from the web.
//
// Some questions have a right answer sitting behind an API, and searching for
// them is strictly worse: slower, and it produces a paraphrase of a page that
// was itself reading the same number. A skill claims those and nothing else.
//
// The rule the whole package follows: a skill that guesses wrong is worse than
// a web search that takes ten seconds. Anything ambiguous falls through.
package skills

import (
	"context"
	"net/http"
	"time"
)

// Source is where a skill's numbers came from. It mirrors the pipeline's own
// source type, kept separate so this package does not import the engine.
type Source struct {
	URL   string
	Title string
	Site  string
}

// Result is what a skill returns. Text is markdown, rendered by the caller,
// since rendering belongs to whoever owns the template.
type Result struct {
	Skill   string
	Text    string
	Sources []Source
	// Shape names the answer format for the caller's contract table. A skill
	// writes its own prose, so this only decides how the answer is presented.
	Shape   string
	Elapsed string
}

// Deps is everything a skill is allowed to reach. Nothing here is a global, so
// a test hands over a stub clock and a stub transport and gets a deterministic
// run.
type Deps struct {
	HTTP *http.Client
	// UA has to be a real browser string. Yahoo and ESPN both answer a block
	// page to anything else.
	UA  string
	Now func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Card is the routing metadata for one skill, and it is the only thing the
// router shows the model.
//
// Written for routing rather than for a reader: what the skill does in the
// third person, the phrasings that should fire it, and the near misses that
// should not. The negative half matters as much as the positive half, because
// the questions a router gets wrong are the ones that sit just outside a
// skill rather than far away from every skill.
type Card struct {
	Name   string
	Does   string
	Fires  []string
	NotFor []string

	// Keywords is the offline matcher. It runs only when the model is
	// unreachable or the routing call fails, so it is allowed to be blunt.
	Keywords []string
}

// Skill answers one kind of question from one source.
type Skill interface {
	Card() Card
	Run(ctx context.Context, question string, d Deps) (*Result, error)
}

// Registry is the set of skills the router chooses between. Order is the order
// they are shown to the model and the order the offline matcher tries them, so
// put the narrow ones first.
type Registry struct {
	skills []Skill
	byName map[string]Skill
}

func NewRegistry(list ...Skill) *Registry {
	r := &Registry{byName: make(map[string]Skill, len(list))}
	for _, s := range list {
		r.skills = append(r.skills, s)
		r.byName[s.Card().Name] = s
	}
	return r
}

// All returns the registered skills in registration order.
func (r *Registry) All() []Skill { return r.skills }

// Names are the enum the router is constrained to, plus the caller's "none".
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s.Card().Name)
	}
	return out
}

// Get returns a skill by name, or nil when the name is not one of ours.
func (r *Registry) Get(name string) Skill { return r.byName[name] }

// Default is the set this site runs. Adding a skill is adding it here and
// nowhere else, since the router builds its prompt and its enum from whatever
// this returns.
func Default() *Registry {
	return NewRegistry(
		Calculator{},
		Convert{},
		Time{},
		Weather{},
		Markets{},
		Odds{},
		Sports{},
	)
}
