// Package tools is what the model can do besides talk. Every backend is keyless,
// since a tool that needs an account stops working when a key expires and nobody
// notices, and a tool answers with JSON and leaves the prose to the model.
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Result is what a tool hands back. Content is JSON encoded for the model.
type Result struct {
	Name    string          `json:"name"`
	Args    json.RawMessage `json:"args,omitempty"`
	Content any             `json:"content"`
	Err     string          `json:"error,omitempty"`
	Elapsed time.Duration   `json:"-"`
}

// Tool is one capability. Schema is the JSON Schema the model is handed, and
// Run is given the arguments it chose.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Run         func(ctx context.Context, d *Deps, args map[string]any) (any, error)
}

// Deps is what every tool shares. Now is a clock instead of time.Now so a test
// can sit on a fixed day, which matters for anything that decides what "this
// weekend" is.
type Deps struct {
	// HTTP reaches anything, including this machine's own services by container
	// name for the orchard tools.
	HTTP *http.Client
	// Public refuses to connect to this machine or its network, and every tool
	// that takes a url from the model goes through it.
	Public  *http.Client
	Now     func() time.Time
	Guard   *Guard
	Budgets *Budgets

	// The session of whoever is chatting, forwarded by the orchard tools so
	// each site does its own check. It's per turn, so it is set on a copy and
	// never on the shared Deps.
	Session string

	// Where a tool hangs a chart or a forecast for the page to draw. Per turn
	// and on the copy, for the same reason the session is.
	Widgets *Sink

	// Memory is the site's own fact store, so the remember tool writes to the
	// database this process owns instead of to a service over http.
	Memory Memory
	// History is the site's own conversation store, for the same reason.
	History History
	// Property is the house lookup engine, wired the same way since it owns
	// tables in the site's database.
	Property Property
	// A tool that reaches a service with a model behind it has to pass this
	// along. Per turn and on the copy too.
	Incognito bool

	// OnImage is where the turn wants to hear how a picture is getting on, and
	// Picture is the one the turn already showed as expected. Both per turn and
	// set on the copy.
	Images  Images
	OnImage func(ImageProgress)
	Picture *ImageProgress
	// Start is what this turn's picture starts from whatever the model says,
	// and LastPicture is what change_last means. Per turn, on the copy.
	Start       []string
	LastPicture string
}

// WithSession returns a copy carrying one turn's session and its own widget
// sink, since Deps is shared across every turn and writing either onto it would
// hand one person's session and one turn's charts to the next request.
func (d *Deps) WithSession(session string) *Deps {
	c := *d
	c.Session = session
	c.Widgets = NewSink()
	return &c
}

func NewDeps() *Deps {
	return &Deps{
		HTTP:   &http.Client{Timeout: 25 * time.Second},
		Public: publicClient(25 * time.Second),
		Now:    time.Now,
		// A host that starts refusing keeps refusing for a while, and asking it
		// again in the meantime keeps a rate limit alive instead of letting it expire.
		Guard: NewGuard(10 * time.Minute),
		// What we allow ourselves, as opposed to what a host has told us. A gap
		// bounds the rate but only a total stops a long turn spending a day's
		// searches on one question.
		Budgets: NewBudgets(),
	}
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
func integer(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
