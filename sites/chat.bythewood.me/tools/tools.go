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

// Tool is one capability. Schema is the JSON Schema the model is handed and Run
// is given the arguments it chose, and Define fills in both from a struct.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Run         func(ctx context.Context, t *Turn, args map[string]any) (any, error)

	Traits Trait
	// Cites lists the pages a result read, and is nil for a tool whose
	// results are not pages.
	Cites func(result map[string]any) []Cited
}

// Deps is what every turn shares. Now is a clock instead of time.Now so a test
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

	// Memory is the site's own fact store, so the remember tool writes to the
	// database this process owns instead of to a service over http.
	Memory Memory
	// History is the site's own conversation store, for the same reason.
	History History
	// Property is the house lookup engine, wired the same way since it owns
	// tables in the site's database.
	Property Property
	Images   Images
}

// Turn is what a tool is handed, the shared Deps and what belongs to one turn,
// which lives here so it can never be written onto the Deps every turn shares.
type Turn struct {
	*Deps

	// The session of whoever is chatting, forwarded by the orchard tools so
	// each site does its own check.
	Session string
	// Where a tool hangs a chart or a forecast for the page to draw.
	Widgets *Sink
	// A tool that reaches a service with a model behind it has to pass this
	// along.
	Incognito bool

	// OnImage is where the turn wants to hear how a picture is getting on, and
	// Picture is the one the turn already showed as expected.
	OnImage func(ImageProgress)
	Picture *ImageProgress
	// Start is what this turn's picture starts from whatever the model says,
	// and LastPicture is what change_last means.
	Start       []string
	LastPicture string
}

// Turn starts one turn on the shared Deps, carrying its session and a widget
// sink of its own.
func (d *Deps) Turn(session string) *Turn {
	return &Turn{Deps: d, Session: session, Widgets: NewSink()}
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
